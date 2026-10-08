// SPDX-License-Identifier: MIT
package detect

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/basdotio/AgentGuard/internal/collect"
	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/basdotio/AgentGuard/internal/safeio"
)

// The content hash of the three config kinds — hooks, MCP servers, permission lists (with the
// settings env block). Skills, plugins and single files get a tree or file hash at collection
// (collect/hash.go, frozen); these three got "", and every consumer reads "" as "never seen":
// no approval can be stored for them, no reputation entry can match them.
//
// It lives here and not in collect because two things it needs live here: Redact, which collect
// cannot import (detect imports collect), and the script resolution hooks and grants already use.
//
// Definition — see .claude/rules/hash.md before changing any of it, because a change re-keys every
// approval and reputation entry for these kinds:
//
//	hex(sha256(<domain> 0x00 <canonical JSON>))
//
//   - The domain is per kind, so byte-equal inputs of two kinds never share a digest.
//   - Canonical JSON: decoded with UseNumber (numbers as written), keys sorted, no HTML escaping,
//     arrays in order. Not RFC 8785: numbers are not normalised and keys sort by UTF-8 bytes.
//   - No path is part of the input — not the config file's, not OwnerRoot, not a resolved script's —
//     so one configuration on two machines is one identity. The artifact Name is not either: it is a
//     label (a "#n" index, a plugin suffix, a server name), as a skill's directory name is not part
//     of its tree hash.
//   - Secrets are replaced before hashing (hashView). The hash is published in the JSON report and
//     stored in the approvals file, and a digest over a low-entropy secret is a commitment anyone can
//     brute-force. Consequence, intended: changing ONLY a replaced secret does not re-key.
//   - A followed script is folded in by its sha256, or by a marker saying why it could not be.

// Domains. The version suffix is the definition's: bump it with any change to what goes in.
const (
	domainHook        = "aguard:hook:v1"
	domainMCP         = "aguard:mcp:v1"
	domainPermission  = "aguard:permission:v1"
	domainSettingsEnv = "aguard:settings-env:v1"
)

// Markers that stand in for a followed script the hash could not read. Three, not one: a single
// marker would make "there is no X" and "X is there but unreadable" the same key — the collision
// collect.TreeHash's unreadable-entry marker exists to prevent — and an approval recorded against
// the first would then cover the second.
const (
	scriptUnresolved  = "unresolved"   // the path holds a variable or a glob, or no file is there
	scriptOutsideHome = "outside-home" // it resolves outside HOME and is never read (invariant #2)
	scriptUnreadable  = "unreadable"   // it is there and could not be opened
)

// shellMeta are the characters that make a span of a shell command line code rather than a literal.
const shellMeta = "$`();|&<>\\"

const redacted = "<REDACTED>"

// ContentHashes returns a copy of arts in which every hook, MCP server and permission artifact
// whose Hash is empty has its content hash. Nothing else is touched: a hash collect computed is
// never overwritten, other kinds keep what they have, and an artifact standing for a config file
// that did not parse keeps "" — it was never read, and "" is the key nothing can match.
//
// cmd/aguard.analyze calls this immediately after Run, before anything reads Hash.
func ContentHashes(root string, arts []model.ArtifactReport) []model.ArtifactReport {
	// Cleaned as CollectAll cleans it: `--root ~/.claude/` (shell completion adds the slash) would
	// otherwise make home == root, resolve `~/…` scripts against the wrong directory, and give one
	// configuration two identities depending on how its path was typed.
	root = filepath.Clean(root)
	out := make([]model.ArtifactReport, len(arts))
	docs := map[string]configDoc{}
	for i, a := range arts {
		out[i] = a
		if a.Hash != "" || hasParseError(a) {
			continue
		}
		if domain, canon, ok := contentHashInput(root, a, docs); ok {
			out[i].Hash = contentDigest(domain, canon)
		}
	}
	return out
}

// contentDigest is the one place the domain separator is applied.
func contentDigest(domain string, canon []byte) string {
	h := sha256.New()
	h.Write([]byte(domain))
	h.Write([]byte{0})
	h.Write(canon)
	return hex.EncodeToString(h.Sum(nil))
}

// contentHashInput returns the domain and canonical bytes for one config artifact, or ok=false when
// the artifact is not one of the three kinds or its content cannot be read back.
func contentHashInput(root string, a model.ArtifactReport, docs map[string]configDoc) (string, []byte, bool) {
	switch a.Kind {
	case model.KindHook:
		return hookHashInput(root, a.Hook)
	case model.KindMCP:
		return mcpHashInput(a, docs)
	case model.KindPermission:
		// The same predicate unitsFor uses to tell the env block from the permissions list.
		if strings.HasPrefix(a.Name, collect.SettingsEnvName) {
			return sectionHashInput(a.Path, "env", domainSettingsEnv, docs)
		}
		return permissionHashInput(root, a, docs)
	}
	return "", nil, false
}

// hookHashInput: when the hook fires (event, matcher), what kind it is, what it runs, and what the
// scripts it names contain. The command is shell, so it gets the shell view.
func hookHashInput(root string, h model.Hook) (string, []byte, bool) {
	in := map[string]any{"event": h.Event, "matcher": h.Matcher}
	if strings.EqualFold(strings.TrimSpace(h.Type), "http") {
		u := strings.TrimSpace(h.URL)
		if u == "" {
			return "", nil, false
		}
		in["type"] = "http"
		in["url"] = hashView(u)
	} else {
		cmd := strings.TrimSpace(h.Command)
		if cmd == "" {
			return "", nil, false
		}
		in["type"] = "command"
		in["command"] = shellHashView(cmd)
		in["scripts"] = scriptDigests(root, h.OwnerRoot, scriptRefs(cmd))
	}
	canon, ok := canonicalJSON(in)
	return domainHook, canon, ok
}

// mcpHashInput: the server's whole entry under mcpServers. `command` and `args` are what gets
// executed — through a shell when the command is one — so they get the shell view.
func mcpHashInput(a model.ArtifactReport, docs map[string]configDoc) (string, []byte, bool) {
	key := a.MCPServer
	if key == "" {
		key = a.Name
	}
	top, ok := readConfigDoc(a.Path, docs)
	if !ok {
		return "", nil, false
	}
	var servers map[string]json.RawMessage
	if json.Unmarshal(top["mcpServers"], &servers) != nil {
		return "", nil, false
	}
	raw, ok := servers[key]
	if !ok {
		return "", nil, false
	}
	v, ok := decodeJSON(raw)
	if !ok {
		return "", nil, false
	}
	entry, isObject := v.(map[string]any)
	if !isObject {
		canon, ok := canonicalJSON(redactTree(v, "", false))
		return domainMCP, canon, ok
	}
	view := make(map[string]any, len(entry))
	for k, e := range entry {
		view[k] = redactTree(e, k, k == "command" || k == "args")
	}
	canon, ok := canonicalJSON(view)
	return domainMCP, canon, ok
}

// permissionHashInput: the whole permissions object — allow, deny, ask, defaultMode, anything else
// in it — plus the scripts its allow entries name. Findings on this artifact come partly from those
// scripts (permissionUnits), so an approval that did not bind them would survive an edit to them.
func permissionHashInput(root string, a model.ArtifactReport, docs map[string]configDoc) (string, []byte, bool) {
	top, ok := readConfigDoc(a.Path, docs)
	if !ok {
		return "", nil, false
	}
	raw, ok := top["permissions"]
	if !ok {
		return "", nil, false
	}
	v, ok := decodeJSON(raw)
	if !ok {
		return "", nil, false
	}
	in := map[string]any{
		"permissions": redactTree(v, "", false),
		"scripts":     scriptDigests(root, "", allowScriptRefs(v)),
	}
	canon, ok := canonicalJSON(in)
	return domainPermission, canon, ok
}

// sectionHashInput hashes one top-level section of a config file as it stands (the settings env block).
func sectionHashInput(path, section, domain string, docs map[string]configDoc) (string, []byte, bool) {
	top, ok := readConfigDoc(path, docs)
	if !ok {
		return "", nil, false
	}
	raw, ok := top[section]
	if !ok {
		return "", nil, false
	}
	v, ok := decodeJSON(raw)
	if !ok {
		return "", nil, false
	}
	canon, ok := canonicalJSON(redactTree(v, "", false))
	return domain, canon, ok
}

// allowScriptRefs lists, once each and in order, the scripts the allow entries name — the same
// tokenisation permissionUnits follows them with.
func allowScriptRefs(perms any) []string {
	m, _ := perms.(map[string]any)
	allow, _ := m["allow"].([]any)
	var refs []string
	seen := map[string]bool{}
	for _, e := range allow {
		entry, ok := e.(string)
		if !ok {
			continue
		}
		for _, ref := range scriptRefs(entry) {
			if !seen[ref] {
				seen[ref] = true
				refs = append(refs, ref)
			}
		}
	}
	return refs
}

// scriptDigests folds in each referenced script — resolved exactly as hookUnits and permissionUnits
// resolve it — by content, or by the marker that says why it could not be read. Never by path.
func scriptDigests(root, ownerRoot string, refs []string) []string {
	home := filepath.Dir(root) // the anchor hookUnits uses: the scan's own home, never the ambient one
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, scriptDigest(root, home, ownerRoot, ref))
	}
	return out
}

func scriptDigest(root, home, ownerRoot, ref string) string {
	path, why := resolveHookScript(root, home, ref)
	switch {
	case why != "":
		p, ok := resolveInOwnerRoot(ownerRoot, ref)
		if !ok {
			return scriptUnresolved
		}
		path = p
	case !inBoundary(home, path):
		return scriptOutsideHome
	}
	// FileHash streams and refuses non-regular files, so a planted FIFO or a multi-GB script costs a
	// marker, not a hang or an OOM — and a script over the 1 MiB scan cap is still bound by content.
	if sum := collect.FileHash(path); sum != "" {
		return "sha256:" + sum
	}
	return scriptUnreadable
}

// hashView is what of a value goes into a content hash: Redact's credential half. Not the entropy
// catch-all — see Redact for why a hash must not replace an opaque token it cannot leak.
func hashView(s string) string { return redactCredentials(s) }

// shellHashView is hashView for a value a shell will interpret. A replacement that would remove a
// shell metacharacter is refused and the value goes in as written: in an unquoted, replaceable span
// of a command line, `$(` is code and not a literal password — `-u admin:$(curl …|sh)` would
// otherwise hash exactly like `-u admin:hunter2`, and the payload could be swapped under an approval.
func shellHashView(s string) string {
	v := redactCredentials(s)
	if metaSkeleton(s) != metaSkeleton(v) {
		return s
	}
	return v
}

// metaSkeleton is the sequence of shell metacharacters in s, ignoring the replacement marker's own.
func metaSkeleton(s string) string {
	s = strings.ReplaceAll(s, redacted, "")
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(shellMeta, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// redactTree returns a copy of a decoded JSON value with every string replaced by its view. A string
// under an object key is viewed as `KEY=VALUE` — for an env block the key is the signal, and
// `hunter2` alone announces nothing while `DB_PASSWORD=hunter2` does. A string right after an array
// element that starts with "-" is viewed as `flag value` (`["--api-key", "…"]`). shell selects the
// shell view for values a shell will interpret.
func redactTree(v any, key string, shell bool) any {
	switch t := v.(type) {
	case string:
		return viewIn(key, "=", t, shell)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = redactTree(e, k, shell)
		}
		return out
	case []any:
		out := make([]any, len(t))
		prev := ""
		for i, e := range t {
			s, ok := e.(string)
			if !ok {
				out[i] = redactTree(e, "", shell)
				prev = ""
				continue
			}
			flag := ""
			if strings.HasPrefix(prev, "-") {
				flag = prev
			}
			out[i] = viewIn(flag, " ", s, shell)
			prev = s
		}
		return out
	default:
		return v // json.Number, bool, nil
	}
}

// viewIn views s in the context that announces it, if any: the joined `ctx+sep+s` is redacted and
// the part after the unchanged prefix is kept. When the context changes nothing, or is itself
// rewritten, s is viewed alone.
func viewIn(ctx, sep, s string, shell bool) string {
	view := hashView
	if shell {
		view = shellHashView
	}
	if ctx != "" {
		prefix := ctx + sep
		if jv := view(prefix + s); strings.HasPrefix(jv, prefix) && jv[len(prefix):] != s {
			return jv[len(prefix):]
		}
	}
	return view(s)
}

// configDoc is one config file's top level, read once per ContentHashes call: ~/.claude.json holds
// every user-level server and is read by each of them.
type configDoc struct {
	top map[string]json.RawMessage
	ok  bool
}

func readConfigDoc(path string, docs map[string]configDoc) (map[string]json.RawMessage, bool) {
	if d, seen := docs[path]; seen {
		return d.top, d.ok
	}
	var d configDoc
	if b, err := safeio.ReadFile(path, safeio.MaxConfigBytes); err == nil {
		d.ok = json.Unmarshal(b, &d.top) == nil
	}
	docs[path] = d
	return d.top, d.ok
}

func decodeJSON(raw []byte) (any, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return nil, false
	}
	return v, true
}

// canonicalJSON: encoding/json sorts map keys; SetEscapeHTML(false) keeps `<REDACTED>` and `&`
// as written; the encoder's trailing newline is not part of the input.
func canonicalJSON(v any) ([]byte, bool) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if enc.Encode(v) != nil {
		return nil, false
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte{'\n'}), true
}

// hasParseError reports an artifact collect raised for a config file that did not parse.
func hasParseError(a model.ArtifactReport) bool {
	for _, f := range a.Findings {
		if f.Source == model.SrcParseError {
			return true
		}
	}
	return false
}
