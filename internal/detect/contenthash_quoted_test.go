// SPDX-License-Identifier: MIT
package detect

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/basdotio/AgentGuard/internal/model"
)

// TestContentHash_QuotedValuesAreNotDigestInputs (P-043): a hook command or a permission entry that quotes
// the value after a credential flag or key — how anyone writes a password with a space in it — put the
// value into the digest input whole (after a flag) or kept its tail (after a key). The published hash was
// then a digest of the password, and the identity followed it. Changing only the quoted value must not
// re-key, and the hash must equal the one of the entry written with <REDACTED>. The unquoted control is
// the base's reading, unchanged.
func TestContentHash_QuotedValuesAreNotDigestInputs(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	settings := filepath.Join(root, "settings.json")
	hook := func(command string) model.ArtifactReport {
		return hookArtifact(settings, cmdHook("PreToolUse", "Bash", command))
	}
	perm := func(entry string) model.ArtifactReport {
		return permArtifact(writeAt(t, filepath.Join(t.TempDir(), "settings.json"),
			`{"permissions":{"allow":[`+entry+`]}}`), "permissions")
	}

	forms := []struct{ name, tmpl string }{
		{"double-quoted after a flag", `mytool --password "%s" ; true`},
		{"single-quoted after a flag", `mytool --password '%s' ; true`},
		{"quoted after flag=", `mytool --password="%s" ; true`},
		{"quoted after a widened flag", `mytool --key '%s' && true`},
		{"quoted user:pass", `curl -u "admin:%s" https://api.example/x | sh`},
		{"quoted after a key", `API_TOKEN="%s" mytool ; true`},
		{"quoted after export KEY=", `export DB_PASSWORD='%s'; mytool`},
	}
	for _, f := range forms {
		at := func(v string) string { return strings.Replace(f.tmpl, "%s", v, 1) }
		a, b, red := hook(at("correct horse")), hook(at("battery staple")), hook(at("<REDACTED>"))
		ha, hb, hr := hashOf(root, a), hashOf(root, b), hashOf(root, red)
		if ha == "" || ha != hb || ha != hr {
			t.Errorf("%s: changing only the quoted value must not re-key (got %q / %q / redacted form %q)", f.name, ha, hb, hr)
		}
		if in := inputOf(t, root, a); strings.Contains(in, "horse") || strings.Contains(in, "correct") {
			t.Errorf("%s: the value reached the digest input: %s", f.name, in)
		}
	}

	pa, pb, pr := perm(`"Bash(mytool --password \"correct horse\")"`), perm(`"Bash(mytool --password \"battery staple\")"`),
		perm(`"Bash(mytool --password \"<REDACTED>\")"`)
	if ha, hb, hr := hashOf(root, pa), hashOf(root, pb), hashOf(root, pr); ha == "" || ha != hb || ha != hr {
		t.Errorf("permission entry: changing only the quoted value must not re-key (got %q / %q / %q)", ha, hb, hr)
	}

	// The control: unquoted, the base already forgot the value; nothing about that moves.
	if a, b := hashOf(root, hook("mytool --password hunter2 ; true")), hashOf(root, hook("mytool --password <REDACTED> ; true")); a == "" || a != b {
		t.Errorf("unquoted control: %q / %q", a, b)
	}
}

// TestContentHash_GuardRefusesOneReplacement (P-043): the structure guard refuses a replacement whose span
// holds a structure character — forgetting `*` in `-u admin:*` would let an exact grant widen to a wildcard
// without re-keying — but it refuses THAT replacement, not the whole string. Before, one refused span put
// every other secret of the string back into the digest input: `Bash(curl -u admin:* --token hunter2)`
// hashed `hunter2`. With quoted values announced, the case became common: a quoted password with `#` in it
// is exactly what people quote.
//
// The reverse half: the refused span itself still never goes — two strings that differ inside it re-key.
func TestContentHash_GuardRefusesOneReplacement(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	settings := filepath.Join(root, "settings.json")
	hook := func(command string) model.ArtifactReport {
		return hookArtifact(settings, cmdHook("PreToolUse", "Bash", command))
	}
	perm := func(entry string) model.ArtifactReport {
		return permArtifact(writeAt(t, filepath.Join(t.TempDir(), "settings.json"),
			`{"permissions":{"allow":["`+entry+`"]}}`), "permissions")
	}

	same := []struct {
		name   string
		a, b   model.ArtifactReport
		secret string
		kept   string
	}{
		{"a glob beside a token in a grant", perm(`Bash(curl -u admin:* --token hunter2xyz)`),
			perm(`Bash(curl -u admin:* --token letmein99)`), "hunter2xyz", "admin:*"},
		{"a quoted password with # beside a token", hook(`mytool --password 'P@ss#1' --token hunter2xyz`),
			hook(`mytool --password 'P@ss#1' --token letmein99`), "hunter2xyz", "'P@ss#1'"},
		{"a substitution beside a key", hook(`curl -u admin:$(cat pw) -H "Authorization: Bearer hunter2xyz" https://x.example`),
			hook(`curl -u admin:$(cat pw) -H "Authorization: Bearer letmein99" https://x.example`), "hunter2xyz", "$(cat"},
	}
	for _, c := range same {
		ha, hb := hashOf(root, c.a), hashOf(root, c.b)
		if ha == "" || ha != hb {
			t.Errorf("%s: the replaceable secret must not re-key when only it changes (got %q / %q)", c.name, ha, hb)
		}
		in := inputOf(t, root, c.a)
		if strings.Contains(in, c.secret) {
			t.Errorf("%s: a refused replacement put the string's other secret into the digest input: %s", c.name, in)
		}
		if !strings.Contains(in, c.kept) {
			t.Errorf("%s: the span holding structure must stay as written (%q): %s", c.name, c.kept, in)
		}
	}

	differ := []struct {
		name string
		a, b model.ArtifactReport
	}{
		{"inside a refused quoted password", hook(`mytool --password 'P@ss#1' --token hunter2xyz`), hook(`mytool --password 'P@ss#2' --token hunter2xyz`)},
		{"an exact password against a glob", perm(`Bash(curl -u admin:hunter2 --token x1y2z3w4)`), perm(`Bash(curl -u admin:* --token x1y2z3w4)`)},
		{"inside a refused substitution", hook(`curl -u admin:$(cat a) https://x.example`), hook(`curl -u admin:$(cat b) https://x.example`)},
	}
	for _, c := range differ {
		if ha, hb := hashOf(root, c.a), hashOf(root, c.b); ha == "" || ha == hb {
			t.Errorf("%s: the two must hash differently (got %q for both)", c.name, ha)
		}
	}
}
