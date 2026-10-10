// SPDX-License-Identifier: MIT
package detect

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"regexp"
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
//   - Whole entries: a hook's own JSON object and an MCP server's, every field — a changed type,
//     timeout or header is a different thing to approve.
//   - No path is part of the input — not the config file's, not OwnerRoot, not a resolved script's —
//     so one configuration on two machines is one identity. The artifact Name is not either: it is a
//     label (a "#n" index, a plugin suffix, a server name), as a skill's directory name is not part
//     of its tree hash.
//   - Secrets are replaced before hashing (see guardedView). The hash is published in the JSON
//     report and stored in the approvals file, and a digest over a low-entropy secret is a commitment
//     anyone can brute-force — a fragment of one too: an argument a flag announces is forgotten whole,
//     not up to its first space (viewElement). Consequence, intended: changing ONLY a replaced secret
//     does not re-key.
//     The rule that keeps that from becoming "changing the code does not re-key": a replacement may
//     forget a secret, never structure — decided per replacement, so refusing one span keeps the
//     value's other secrets out (P-043).
//   - A followed script is folded in by its sha256, or by a marker saying why it could not be.

// Domains. The version suffix is the definition's shape — which fields go in, how they are encoded, the
// markers: bump it when that changes, since every entry of the kind then re-keys. A change to what the
// replacement of secrets forgets re-keys only the entries holding such a secret, keeps the suffix and is
// named in its proposal with what it re-keys (P-039: an argument a flag announces, forgotten whole).
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

// How a value is read by whatever consumes it, which decides what in it is STRUCTURE — characters a
// replacement must not take away, because a span holding them is not a literal secret.
type viewMode int

const (
	// viewLiteral is a value read as data (an env value, a header, a URL). Only URL delimiters are
	// structure: `https://other.example:443#@good.example/` connects to other.example, and with the
	// `443#` span replaced it would hash like a URL with a password that connects to good.example.
	viewLiteral viewMode = iota
	// viewShell is a value a shell interprets (a hook command; an MCP command and its args). In an
	// unquoted, replaceable span `$(`, a glob, a `;` are code: `-u admin:$(curl …|sh)` must not hash
	// like `-u admin:hunter2`.
	viewShell
	// viewGrant is a permission entry. `Tool(pattern)` is unwrapped and the pattern — matched against
	// the commands the agent runs — gets the shell view, so `admin:*)` cannot hash like `admin:pw)`:
	// widening an exact grant into a wildcard is the change an approval exists to catch.
	viewGrant
)

const (
	literalStructure = "#?\\"
	shellStructure   = "$`();|&<>\\*?#[]{}"
)

// grantRE splits a permission entry into its tool and its pattern: `Bash(curl -u a:b *)`.
var grantRE = regexp.MustCompile(`(?s)^([A-Za-z][A-Za-z0-9_]*)\((.*)\)$`)

const redacted = "<REDACTED>"

// ContentHashes returns a copy of arts in which every hook, MCP server and permission artifact
// whose Hash is empty has its content hash. Nothing else is touched: a hash collect computed is
// never overwritten, other kinds keep what they have, and an artifact standing for a config file
// that did not parse keeps "" — it was never read, and "" is the key nothing can match.
//
// cmd/aguard.analyze calls this immediately after Run, before anything reads Hash.
func ContentHashes(root string, arts []model.ArtifactReport) []model.ArtifactReport {
	// The scripts' anchor must not depend on how the root was typed: `--root ~/.claude/` made
	// home == root and `aguard hash .` made home ".", so `~/…` scripts resolved somewhere else and
	// one configuration got a second identity. The same anchor Run uses.
	root = anchorRoot(root)
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

// hookHashInput: when the hook fires (event, matcher), its whole entry as written, and what the
// scripts its command names contain. The entry comes from collect (model.Hook.Entry); a hook
// artifact without one was not built from a settings file and gets no hash.
func hookHashInput(root string, h model.Hook) (string, []byte, bool) {
	entry, ok := decodeJSON([]byte(h.Entry))
	if !ok {
		return "", nil, false
	}
	in := map[string]any{"event": h.Event, "matcher": h.Matcher, "entry": redactEntry(entry)}
	if !strings.EqualFold(strings.TrimSpace(h.Type), "http") {
		// The same command hookUnits follows scripts out of — so the same scripts are bound.
		in["scripts"] = scriptDigests(root, h.OwnerRoot, scriptRefs(strings.TrimSpace(h.Command)))
	}
	canon, ok := canonicalJSON(in)
	return domainHook, canon, ok
}

// mcpHashInput: the server's whole entry in its server map — under mcpServers, or at the top level
// of a plugin file without the wrapper (P-029). The canonical form is the entry alone, so the same
// server hashes the same whichever layout holds it.
func mcpHashInput(a model.ArtifactReport, docs map[string]configDoc) (string, []byte, bool) {
	key := MCPServerKey(a) // the entry the rules scanned, a plugin's server and the "" key included
	top, ok := readConfigDoc(a.Path, docs)
	if !ok {
		return "", nil, false
	}
	raw, ok := mcpServerMap(top, a.MCPUnwrapped)[key]
	if !ok {
		return "", nil, false
	}
	v, ok := decodeJSON(raw)
	if !ok {
		return "", nil, false
	}
	canon, ok := canonicalJSON(redactEntry(v))
	return domainMCP, canon, ok
}

// redactEntry views a hook or MCP server entry: `command` and `args` are what gets executed —
// through a shell when the command is one — so they get the shell view; everything else is data.
func redactEntry(v any) any {
	entry, isObject := v.(map[string]any)
	if !isObject {
		return redactTree(v, "", viewLiteral)
	}
	out := make(map[string]any, len(entry))
	for k, e := range entry {
		mode := viewLiteral
		if k == "command" || k == "args" {
			mode = viewShell
		}
		out[k] = redactTree(e, k, mode)
	}
	return out
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
		"permissions": redactTree(v, "", viewGrant),
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
	canon, ok := canonicalJSON(redactTree(v, "", viewLiteral))
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

// guardedView is what of a value goes into a content hash: Redact's credential half — not the
// entropy catch-all, see Redact for why — with every replacement that would take away a character
// of structure refused. The refusal is the replacement's own (P-043): its match goes in as written and
// every other replacement in the value still applies. It used to be the value's — one refused span put
// the value in whole, so `Bash(curl -u admin:* --token hunter2)` hashed `hunter2`, and a quoted password
// with `#` in it would have done the same to every other secret on its line.
func guardedView(s, structure string) string {
	return redactCredentials(s, func(match, repl string) bool {
		return skeleton(match, structure) == skeleton(repl, structure)
	})
}

// skeleton is the sequence of structure characters in s, ignoring the replacement marker's own.
func skeleton(s, structure string) string {
	s = strings.ReplaceAll(s, redacted, "")
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(structure, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// redactTree returns a copy of a decoded JSON value with every string replaced by its view. A string
// under an object key is viewed as `KEY=VALUE` — for an env block the key is the signal, and
// `hunter2` alone announces nothing while `DB_PASSWORD=hunter2` does. A string right after an array
// element that starts with "-" is viewed with that flag (`["--api-key", "…"]`, viewElement).
func redactTree(v any, key string, mode viewMode) any {
	switch t := v.(type) {
	case string:
		return viewString(key, "=", t, mode)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = redactTree(e, k, mode)
		}
		return out
	case []any:
		out := make([]any, len(t))
		prev := ""
		for i, e := range t {
			s, ok := e.(string)
			if !ok {
				out[i] = redactTree(e, "", mode)
				prev = ""
				continue
			}
			out[i] = viewElement(prev, s, mode)
			prev = s
		}
		return out
	default:
		return v // json.Number, bool, nil
	}
}

// viewElement views a string in an array, after the string prev. When prev is a flag that announces it
// as a credential (announcedArg — the decision RedactArgv takes for the judge's excerpt), the element is
// forgotten from the first announced byte to its END (P-039). In an argument vector the element is one
// argument, all of it the value, while the patterns stop a value at whitespace or a quote: the joined
// reading below kept the tail ` horse` of `"--password", "correct horse"` in the digest input, where a
// word list recovers it from the published hash, and the identity then followed that fragment.
//
// The whole-element replacement may forget a secret, never structure: when the forgotten span holds a
// structure character it is refused, and the element gets the joined `flag value` reading it had before —
// never more of the secret in the input than that, and never less structure.
func viewElement(prev, s string, mode viewMode) string {
	if v, ok := viewAnnounced(prev, s, mode); ok {
		return v
	}
	flag := ""
	if strings.HasPrefix(prev, "-") {
		flag = prev
	}
	return viewString(flag, " ", s, mode)
}

// viewAnnounced is the whole-element replacement, or ok=false. The structure set is the one viewString
// gives the element; a grant is read alone, as viewString reads it.
func viewAnnounced(prev, s string, mode viewMode) (string, bool) {
	structure := literalStructure
	switch {
	case mode == viewGrant && grantRE.MatchString(s):
		return "", false
	case mode == viewShell:
		structure = shellStructure
	}
	at, ok := announcedArg(prev, s)
	if !ok || skeleton(s[at:], structure) != "" {
		return "", false
	}
	return guardedView(s[:at], structure) + redacted, true
}

// viewString views one string in its mode. A grant is unwrapped first; a string in grant mode that
// is not a grant (defaultMode, a directory) is data.
func viewString(ctx, sep, s string, mode viewMode) string {
	if mode == viewGrant {
		if m := grantRE.FindStringSubmatch(s); m != nil {
			return m[1] + "(" + viewIn("", "", m[2], shellStructure) + ")"
		}
		return viewIn(ctx, sep, s, literalStructure)
	}
	if mode == viewShell {
		return viewIn(ctx, sep, s, shellStructure)
	}
	return viewIn(ctx, sep, s, literalStructure)
}

// viewIn views s in the context that announces it, if any: the joined `ctx+sep+s` is redacted and
// the part after the unchanged prefix is kept. When the context changes nothing, or is itself
// rewritten, s is viewed alone.
func viewIn(ctx, sep, s, structure string) string {
	if ctx != "" {
		prefix := ctx + sep
		if jv := guardedView(prefix+s, structure); strings.HasPrefix(jv, prefix) && jv[len(prefix):] != s {
			return jv[len(prefix):]
		}
	}
	return guardedView(s, structure)
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
