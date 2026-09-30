// SPDX-License-Identifier: MIT
// Command reputation-refresh keeps the embedded allowlist in step with the official marketplace,
// and drafts new entries so that a human's only job is the review.
//
// A GOOD entry in internal/reputation/data/reputation.json suppresses an artifact's findings.
// It is keyed by canonical hash, so it dies the moment upstream ships a new commit — every few
// weeks for an actively maintained plugin. Left to hand maintenance the list rots, and a rotted
// allowlist is worse than none: the plugin goes back to Elevated on every fresh install while
// the entry still looks curated.
//
// Renewal runs under one rule: the marketplace's new pin is accepted only if the plugin's bytes
// are unchanged (same canonical hash — nothing to re-read) or `aguard check` on it produces the
// SAME finding fingerprint the human review covered, meaning every finding present is one a
// person already read and explained. Anything added or removed fails closed — the entry is
// left alone, the tool exits 1, and the diff is printed for whoever reviews next. It never
// invents a reason and never widens a review. The tool is therefore exactly as strong as the
// static scan it runs: upstream code that changes without moving a single finding is accepted
// unread, but that code would also score 100 without any allowlist, so the refresh adds no
// trust the scanner would not have granted an unknown plugin.
//
// Two ways a marketplace pins a plugin, one entry model:
//
//   - an external repository pinned by {"source":"url","url":…,"sha":…}: the entry records
//     source=url, sha=sha, no path;
//   - a directory vendored inside the marketplace repository itself ("source":"./plugins/x",
//     which is every Anthropic-authored plugin): the entry records source=marketplace repo,
//     sha=marketplace commit, path=the directory. The marketplace commit moves whenever ANY
//     plugin changes, which is why renewal compares the hash first and rescans only when the
//     bytes under path actually moved.
//
// Claude Desktop's built-in skills are the one source that is not a repository. They live in
// the app's own store (collect.DesktopSkillBundles) with an `updatedAt` stamp per skill, which
// their entries pin as `sha`. Renewal follows the same rule but can only run on a machine that
// has the store; elsewhere (the weekly CI job) those entries are reported as not checkable and
// left alone, never marked stale. `-add-desktop` refuses a skill whose creatorType is not
// "anthropic": a user's own upload is theirs to baseline, not something to ship in everyone's
// allowlist.
//
// Usage (from the repo root, with a built binary):
//
//	go run ./hack/reputation-refresh -aguard bin/aguard                 # renew: report only
//	go run ./hack/reputation-refresh -aguard bin/aguard -write          # renew: update the JSON
//	go run ./hack/reputation-refresh -aguard bin/aguard -add plugin-dev # draft an entry + review sheet
//	go run ./hack/reputation-refresh -aguard bin/aguard -add plugin-dev -write
//	go run ./hack/reputation-refresh -aguard bin/aguard -add-desktop import-memory   # a Claude Desktop built-in
//
// `-add -write` appends the draft with an EMPTY reason. internal/reputation's tests then fail
// until a human fills it in — deliberately: there is no path by which an unreasoned entry ends
// up green. A plugin that scans 100/100 is not written at all; an allowlist entry adds nothing.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/basdotio/agent-guard/internal/model"
	"github.com/basdotio/agent-guard/internal/reputation"
)

const (
	defaultMarketplace = "https://github.com/anthropics/claude-plugins-official.git"
	marketplaceName    = "claude-plugins-official"
	listPath           = "internal/reputation/data/reputation.json"
)

// listFile mirrors reputation.json. Field order is the on-disk order.
type listFile struct {
	Version string             `json:"version"`
	Note    string             `json:"note"`
	Entries []reputation.Entry `json:"entries"`
}

func main() {
	aguard := flag.String("aguard", "bin/aguard", "path to a built aguard binary")
	market := flag.String("marketplace", defaultMarketplace, "marketplace git URL, or a local checkout / marketplace.json path")
	write := flag.Bool("write", false, "update reputation.json in place (default: report only)")
	add := flag.String("add", "", "draft an allowlist entry for this marketplace plugin: scan it, print a review sheet and the entry; with -write, append it with reason left empty")
	addDesktop := flag.String("add-desktop", "", "draft an allowlist entry for this Claude Desktop built-in skill, read from this machine's desktop store (same contract as -add)")
	homeDir := flag.String("home", "", "home directory holding the Claude Desktop store (default: the current user's)")
	flag.Parse()

	home := *homeDir
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	var err error
	switch {
	case *add != "":
		err = runAdd(*aguard, *market, *add, *write, os.Stdout)
	case *addDesktop != "":
		err = runAddDesktop(*aguard, home, *addDesktop, *write, os.Stdout)
	default:
		err = run(*aguard, *market, home, *write, os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "reputation-refresh:", err)
		os.Exit(1)
	}
}

// run renews entries whose marketplace pin moved without changing what a human reviewed, then
// gives Claude Desktop entries the same treatment against this machine's desktop store.
func run(aguard, market, home string, write bool, out io.Writer) error {
	list, err := loadList()
	if err != nil {
		return err
	}
	cl := newCloner()
	defer cl.cleanup()
	mk, err := loadMarketplace(market, cl)
	if err != nil {
		return err
	}

	changed := 0
	var stale []string
	for i := range list.Entries {
		e := &list.Entries[i]
		if e.Verdict != reputation.Good || e.Source == "" || e.SHA == "" {
			continue
		}
		if e.Source == reputation.SourceClaudeDesktop {
			continue // not a marketplace pin; renewed below against the desktop store
		}
		pin, ok := mk.pins[e.Name]
		switch {
		case !ok:
			fmt.Fprintf(out, "  %s: not pinned by the marketplace given — left as is\n", e.Name)
			continue
		case normURL(pin.URL) != normURL(e.Source) || pin.Path != e.Path:
			fmt.Fprintf(out, "! %s: marketplace source moved %s → %s — needs a human\n",
				e.Name, describe(e.Source, e.Path), describe(pin.URL, pin.Path))
			stale = append(stale, e.Name)
			continue
		case pin.SHA == e.SHA:
			fmt.Fprintf(out, "  %s %s: up to date at %s\n", e.Name, e.Version, short(e.SHA))
			continue
		}

		root, err := cl.get(pin.URL, pin.SHA)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name, err)
		}
		dir := filepath.Join(root, pin.Path)
		hash, err := hashOf(aguard, dir)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name, err)
		}
		if hash == e.Hash {
			// Same bytes under path: the marketplace moved for some other plugin. Nothing to
			// re-read, nothing to re-review — only the pin advances.
			fmt.Fprintf(out, "✓ %s: %s → %s, content unchanged\n", e.Name, short(e.SHA), short(pin.SHA))
			e.SHA = pin.SHA
			changed++
			continue
		}
		res, err := checkJSON(aguard, dir)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name, err)
		}
		got := fingerprint(res.Artifacts)
		added, removed := diff(e.Findings, got)
		if len(added)+len(removed) > 0 {
			fmt.Fprintf(out, "! %s: %s → %s changes the finding set — review needed, entry left at %s\n",
				e.Name, short(e.SHA), short(pin.SHA), short(e.SHA))
			for _, f := range added {
				fmt.Fprintf(out, "    + %s\n", f)
			}
			for _, f := range removed {
				fmt.Fprintf(out, "    - %s\n", f)
			}
			stale = append(stale, e.Name)
			continue
		}
		ver := pluginVersion(dir)
		fmt.Fprintf(out, "✓ %s: %s → %s, findings unchanged (%d); version %s → %s, hash %s\n",
			e.Name, short(e.SHA), short(pin.SHA), len(got), e.Version, ver, short(hash))
		e.Hash, e.SHA = hash, pin.SHA
		if ver != "" {
			e.Version = ver
		}
		changed++
	}

	dc, dstale, err := renewDesktop(aguard, home, &list, out)
	if err != nil {
		return err
	}
	changed += dc
	stale = append(stale, dstale...)

	if changed > 0 {
		if write {
			if err := saveList(list); err != nil {
				return err
			}
			fmt.Fprintf(out, "%d entr%s renewed, %s written\n", changed, plural(changed), listPath)
		} else {
			fmt.Fprintf(out, "%d entr%s would be renewed — re-run with -write\n", changed, plural(changed))
		}
	}
	if len(stale) > 0 {
		return fmt.Errorf("%d entr%s need%s a human review: %s", len(stale), plural(len(stale)),
			map[bool]string{true: "s", false: ""}[len(stale) == 1], strings.Join(stale, ", "))
	}
	return nil
}

// runAdd drafts an entry for one marketplace plugin. It does everything mechanical — locate the
// pin, fetch, scan, hash, fingerprint — and prints what the human has to read. It does NOT write
// a reason; that field is the review, and the review is the one step this tool must not do.
func runAdd(aguard, market, name string, write bool, out io.Writer) error {
	list, err := loadList()
	if err != nil {
		return err
	}
	for _, e := range list.Entries {
		if e.Name == name {
			return fmt.Errorf("%s is already listed (hash %s); edit or remove that entry instead of adding a second", name, short(e.Hash))
		}
	}
	cl := newCloner()
	defer cl.cleanup()
	mk, err := loadMarketplace(market, cl)
	if err != nil {
		return err
	}
	pin, ok := mk.pins[name]
	if !ok {
		if mk.SHA == "" {
			return fmt.Errorf("%s is not pinned: the marketplace was given as a bare marketplace.json, so vendored plugins have no commit — pass a checkout or the marketplace URL", name)
		}
		return fmt.Errorf("%s: not a plugin of this marketplace, or not pinned by (url, sha) / vendored path", name)
	}
	root, err := cl.get(pin.URL, pin.SHA)
	if err != nil {
		return err
	}
	dir := filepath.Join(root, pin.Path)
	res, err := checkJSON(aguard, dir)
	if err != nil {
		return err
	}
	fp := fingerprint(res.Artifacts)
	if len(fp) == 0 {
		fmt.Fprintf(out, "%s at %s scans clean (100/100): an allowlist entry would add nothing. Not written.\n", name, short(pin.SHA))
		return nil
	}
	hash, err := hashOf(aguard, dir)
	if err != nil {
		return err
	}
	entry := reputation.Entry{
		Hash: hash, Verdict: reputation.Good, Name: name, Publisher: publisherFor(pin),
		Version: pluginVersion(dir), Source: pin.URL, SHA: pin.SHA, Path: pin.Path,
		Reviewed: today(), Reason: "", Findings: fp,
	}

	printReviewSheet(out, name, res)
	b, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\nDraft entry (reason is yours to write — one sentence per finding class, saying why it is benign):\n%s\n", b)

	if !write {
		fmt.Fprintf(out, "\nNot written. Re-run with -write to append it to %s; the reputation tests stay red until reason is filled in.\n", listPath)
		return nil
	}
	list.Entries = append(list.Entries, entry)
	if err := saveList(list); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nAppended to %s with an EMPTY reason. Fill it in, then `go test ./internal/reputation/`.\n", listPath)
	return nil
}

// printReviewSheet lists every scoring finding with its evidence, worst first, so the reviewer
// reads exactly what the entry will suppress. Snippets come from aguard already redacted.
func printReviewSheet(out io.Writer, name string, res model.ScanResult) {
	var fs []model.Finding
	for _, a := range res.Artifacts {
		for _, f := range a.Findings {
			if f.Dimension > 0 {
				fs = append(fs, f)
			}
		}
	}
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].Severity.Rank() != fs[j].Severity.Rank() {
			return fs[i].Severity.Rank() > fs[j].Severity.Rank()
		}
		return fs[i].RuleID < fs[j].RuleID
	})
	fmt.Fprintf(out, "Review sheet — %s: %d scoring finding(s), score %d\n", name, len(fs), res.Overall)
	for _, f := range fs {
		adv := ""
		if f.Advisory {
			adv = " (advisory)"
		}
		fmt.Fprintf(out, "\n  [%s] %s%s — %s\n", f.RuleID, f.Severity, adv, f.Title)
		for i, ev := range f.Evidence {
			if i == 3 {
				fmt.Fprintf(out, "      … %d more\n", len(f.Evidence)-3)
				break
			}
			snip := strings.TrimSpace(ev.Snippet)
			if len(snip) > 140 {
				snip = snip[:140] + "…"
			}
			fmt.Fprintf(out, "      %s:%d  %s\n", ev.File, ev.Line, snip)
		}
	}
}

// ---- marketplace -------------------------------------------------------------------------

// pin is where the marketplace says a plugin comes from: a git URL, an exact commit, and — for
// plugins vendored inside the marketplace repository — the directory within it.
type pin struct{ URL, SHA, Path string }

// marketplace is a loaded marketplace: the repository and commit it was read from (empty when
// only a marketplace.json file was given) and its pins by plugin name.
type marketplace struct {
	URL, SHA string
	pins     map[string]pin
}

// loadMarketplace reads .claude-plugin/marketplace.json from a git URL (fetched through cl so
// the same checkout serves vendored renewals), a local checkout, or a bare marketplace.json.
func loadMarketplace(market string, cl *cloner) (*marketplace, error) {
	mk := &marketplace{}
	path := market
	if isURL(market) {
		dir, err := cl.get(market, "")
		if err != nil {
			return nil, fmt.Errorf("marketplace: %w", err)
		}
		mk.URL, mk.SHA = market, gitOutput(dir, "rev-parse", "HEAD")
		cl.alias(market, mk.SHA, dir)
		path = dir
	}
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		if mk.URL == "" {
			mk.URL, mk.SHA = mirrorIdentity(path)
			// A local checkout at a known commit IS that commit: serve vendored plugins from it
			// instead of fetching the same tree again (and so work offline).
			cl.alias(mk.URL, mk.SHA, path)
		}
		path = filepath.Join(path, ".claude-plugin", "marketplace.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("marketplace: %w", err)
	}
	pins, err := parsePins(raw, mk.URL, mk.SHA)
	if err != nil {
		return nil, err
	}
	mk.pins = pins
	return mk, nil
}

// mirrorIdentity says which repository and commit a local marketplace directory is. A git
// checkout answers from its own metadata. Claude Code's mirror under ~/.claude/plugins/
// marketplaces/ is NOT a checkout — it is downloaded as a tree — but it carries the commit in a
// `.gcs-sha` file; the repository is then known only when the directory is the official
// marketplace by name (measured: `.gcs-sha` matched the upstream HEAD, and the vendored
// subdirectory hashed identically). Anything else returns "" and vendored plugins are skipped
// with a message rather than pinned to a guess.
func mirrorIdentity(dir string) (url, sha string) {
	if u := gitOutput(dir, "remote", "get-url", "origin"); u != "" {
		return u, gitOutput(dir, "rev-parse", "HEAD")
	}
	b, err := os.ReadFile(filepath.Join(dir, ".gcs-sha"))
	if err != nil {
		return "", ""
	}
	sha = strings.TrimSpace(string(b))
	if len(sha) != 40 {
		return "", ""
	}
	if filepath.Base(filepath.Clean(dir)) == marketplaceName {
		return defaultMarketplace, sha
	}
	return "", sha
}

// parsePins is the pure half of loadMarketplace. A plugin's source is either a string — a path
// inside the marketplace repository, pinned by the marketplace's own (mkURL, mkSHA) when those
// are known — or an object, of which only {"source":"url","url":…,"sha":…} pins a commit.
func parsePins(raw []byte, mkURL, mkSHA string) (map[string]pin, error) {
	var doc struct {
		Plugins []struct {
			Name   string          `json:"name"`
			Source json.RawMessage `json:"source"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("marketplace.json: %w", err)
	}
	pins := map[string]pin{}
	for _, p := range doc.Plugins {
		var dir string
		if json.Unmarshal(p.Source, &dir) == nil {
			if mkURL == "" || mkSHA == "" {
				continue // vendored, but we do not know which commit we are looking at
			}
			pins[p.Name] = pin{URL: mkURL, SHA: mkSHA, Path: filepath.Clean(strings.TrimPrefix(dir, "./"))}
			continue
		}
		var s struct{ Source, URL, SHA string }
		if json.Unmarshal(p.Source, &s) != nil || s.Source != "url" || s.URL == "" || s.SHA == "" {
			continue
		}
		pins[p.Name] = pin{URL: s.URL, SHA: s.SHA}
	}
	return pins, nil
}

// publisherFor names who ships the plugin: the marketplace itself for vendored plugins, the
// repository owner "via" the marketplace for external ones.
func publisherFor(p pin) string {
	if p.Path != "" {
		return marketplaceName
	}
	u := strings.TrimSuffix(strings.TrimSuffix(p.URL, "/"), ".git")
	parts := strings.Split(u, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + " via " + marketplaceName
	}
	return "via " + marketplaceName
}

// normURL makes "…/repo", "…/repo.git" and "…/repo/" compare equal: a local checkout's
// remote and the marketplace manifest spell the same repository differently.
func normURL(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimSuffix(u, "/")
	u = strings.TrimSuffix(u, ".git")
	return strings.ToLower(u)
}

func describe(url, path string) string {
	if path == "" {
		return url
	}
	return url + " :" + path
}

func isURL(s string) bool { return strings.Contains(s, "://") || strings.HasSuffix(s, ".git") }

// ---- git ----------------------------------------------------------------------------------

// cloner fetches (url, sha) checkouts into temp dirs once each and removes them at the end.
//
// Two maps, not one, and the split is a scar: dirs answers "where is (url, sha)?", owned answers
// "did WE create this directory?". Only owned directories are ever removed. The first version
// kept one map, aliased the operator's own marketplace mirror into it so vendored plugins could
// be served offline — and cleanup() then deleted ~/.claude/plugins/marketplaces/
// claude-plugins-official from a real machine. A path we were handed is never ours to delete.
type cloner struct {
	dirs  map[string]string
	owned map[string]bool
}

func newCloner() *cloner { return &cloner{dirs: map[string]string{}, owned: map[string]bool{}} }

func (c *cloner) get(url, sha string) (string, error) {
	key := normURL(url) + "@" + sha
	if d, ok := c.dirs[key]; ok {
		return d, nil
	}
	d, err := cloneAt(url, sha)
	if err != nil {
		return "", err
	}
	c.dirs[key] = d
	c.owned[d] = true
	return d, nil
}

// alias registers an EXISTING directory under (url, sha) — the marketplace fetched at HEAD is
// also the marketplace at the commit HEAD turned out to be, and an operator's local mirror is
// the marketplace at the commit its `.gcs-sha` names. Aliased directories are served, never
// removed: whether cleanup owns them is decided by who created them, not by who refers to them.
func (c *cloner) alias(url, sha, dir string) {
	if url != "" && sha != "" {
		c.dirs[normURL(url)+"@"+sha] = dir
	}
}

// cleanup removes the temp checkouts this cloner created — and nothing else.
func (c *cloner) cleanup() {
	for d := range c.owned {
		_ = os.RemoveAll(d)
	}
	c.owned = map[string]bool{}
}

// cloneAt fetches exactly one commit of url into a fresh temp dir (sha == "" → the default
// branch tip). Shallow by construction: the tree is all we hash.
func cloneAt(url, sha string) (string, error) {
	dir, err := os.MkdirTemp("", "aguard-refresh-*")
	if err != nil {
		return "", err
	}
	// Resolve the temp dir before handing it to aguard. On macOS $TMPDIR sits under /var, a
	// symlink to /private/var; aguard resolves the root's symlinks when it relativises evidence
	// paths, `Rel` then fails against the unresolved root, and the path degrades to its last two
	// segments (`scripts/server.cjs` instead of `skills/brainstorming/scripts/server.cjs`). A
	// fingerprint recorded on one machine would then never match on another — measured: every
	// line of an identical finding set showed as both added and removed.
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	ref := sha
	if ref == "" {
		ref = "HEAD"
	}
	steps := [][]string{
		{"init", "-q"},
		{"remote", "add", "origin", url},
		{"fetch", "-q", "--depth", "1", "origin", ref},
		{"checkout", "-q", "FETCH_HEAD"},
	}
	for _, args := range steps {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if b, err := cmd.CombinedOutput(); err != nil {
			_ = os.RemoveAll(dir)
			return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, bytes.TrimSpace(b))
		}
	}
	return dir, nil
}

// gitOutput runs a git query in dir and returns its trimmed stdout, or "" on any failure —
// callers treat "" as "unknown" and report, never guess.
func gitOutput(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	b, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// ---- aguard -------------------------------------------------------------------------------

// checkJSON runs `aguard check <dir> --json`. Exit code 1 is the gate saying "findings at or
// above the threshold" — expected here, the JSON is still complete. Exit code 2 is a failure.
func checkJSON(aguard, dir string) (model.ScanResult, error) {
	cmd := exec.Command(aguard, "check", dir, "--json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 1 {
			return model.ScanResult{}, fmt.Errorf("aguard check: %v: %s", err, bytes.TrimSpace(stderr.Bytes()))
		}
	}
	var res model.ScanResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		return model.ScanResult{}, fmt.Errorf("aguard check --json: %w", err)
	}
	return res, nil
}

// hashOf runs `aguard hash <dir>` and returns the canonical hash (first field of the last line).
func hashOf(aguard, dir string) (string, error) {
	b, err := exec.Command(aguard, "hash", dir).Output()
	if err != nil {
		return "", fmt.Errorf("aguard hash: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) == 0 || len(fields[0]) != 64 {
		return "", fmt.Errorf("aguard hash: unexpected output %q", strings.TrimSpace(string(b)))
	}
	return fields[0], nil
}

// pluginVersion reads .claude-plugin/plugin.json's version, or "" when absent.
func pluginVersion(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil {
		return ""
	}
	var m struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	return m.Version
}

// ---- list file ----------------------------------------------------------------------------

func loadList() (listFile, error) {
	var list listFile
	raw, err := os.ReadFile(listPath)
	if err != nil {
		return list, err
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return list, fmt.Errorf("%s: %w", listPath, err)
	}
	return list, nil
}

func saveList(list listFile) error {
	list.Version = today()
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(listPath, append(b, '\n'), 0o644)
}

func today() string { return time.Now().UTC().Format("2006-01-02") }

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
