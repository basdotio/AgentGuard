// SPDX-License-Identifier: MIT
// Package redact is the one implementation of secret redaction (spec §16.3, invariant #3): every
// snippet a finding carries, every excerpt the optional judge sends, and every piece of file or
// config text a note quotes passes through Secrets before it is stored.
//
// It is a leaf package for a reason that is structural, not stylistic. The rule engine (detect)
// depends on the collector (collect), and the collector writes notes too — an @import line, a
// plugin key, a hook event key. While the implementation lived in detect, collect could not reach
// it, and those notes quoted their text unredacted. A second copy of the patterns in collect would
// have closed that gap by opening a worse one: two redactors drift, and the one nobody is looking
// at is the one that leaks. detect.Redact delegates here unchanged.
//
// Redaction is BEST-EFFORT. It removes what looks like a secret by shape — a known token prefix, a
// value a key or flag announces as a credential, URL userinfo, a long high-entropy run — and
// nothing else; the privacy argument that makes that acceptable is that content does not leave the
// machine by default (spec §16.4).
package redact

import (
	"math"
	"regexp"
	"strings"
)

// redactPatterns match known secret prefixes. Each replaces the SECRET with <REDACTED>.
var redactPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{16,}`),                                    // Anthropic (before generic sk-)
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{16,}`),                                        // OpenAI-style
	regexp.MustCompile(`sk_live_[A-Za-z0-9]{16,}`),                                     // Stripe live
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),                                             // AWS access key id
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`),                                   // GitHub tokens
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),                                 // GitHub fine-grained PAT
	regexp.MustCompile(`glpat-[A-Za-z0-9_-]{16,}`),                                     // GitLab PAT
	regexp.MustCompile(`AIza[A-Za-z0-9_-]{30,}`),                                       // Google API key
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),                                 // Slack
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{6,}`), // JWT
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),                           // PEM private key
}

// credKeys are key names that ANNOUNCE their value is a credential.
//
// `auth` is spelled out instead of the earlier `auth[a-z]*`: the wildcard also swallowed
// `author` and `authority` (aguard's own `llm.authority` among them), which the 12-char floor
// used to hide and the floor below would not.
const credKeys = `api[_-]?key|secret|token|password|passwd|auth(?:entication|ori[sz](?:ation)?)?|bearer|access[_-]?key|client[_-]?secret`

// carrier is an auth SCHEME word sitting between the key and the value
// (`Authorization: Bearer <token>`). It belongs to the SEPARATOR, not the value: treated as
// the value it would consume `Bearer`, and the scan would then resume past it — redacting the
// word and shipping the token itself in the clear.
const carrier = `(?:(?:bearer|basic|token)\s+)?`

// assignRE catches an explicit assignment: `key=value`, `key: value`, `"key": "value"`.
// The separator allows ZERO spaces after `:`/`=` so `TOKEN=abcdef…` is redacted
// (a prior `\s+` there was the B1 gap — PERM-001 hits it directly).
//
// The value floor is 4, not 12. The key here has already named itself a credential, so the
// value's LENGTH says nothing about whether it is one: `password=hunter2` is a bad password,
// not a non-password, and at 7 characters it used to reach the report — and, with `--llm`
// against a remote endpoint, the network — in the clear. 12 is the right floor for a value
// with NO key vouching for it; that is entropyTokenRE's job and its threshold is unchanged.
var assignRE = regexp.MustCompile(`(?i)(` + credKeys + `)(["']?\s*[:=]\s*["']?` + carrier + `)([A-Za-z0-9/+_.\-]{4,})`)

// looseAssignRE is the same pair separated by whitespace ALONE (`Authorization Bearer abc…`).
// With no `:`/`=` the shape also fits ordinary English — "the secret sauce is" — so it keeps
// the 12-char floor: length is the only thing here that separates a credential from a word,
// and a doc snippet with its prose replaced by <REDACTED> costs the report its readability
// without protecting anything.
var looseAssignRE = regexp.MustCompile(`(?i)(` + credKeys + `)(["']?\s+` + carrier + `)([A-Za-z0-9/+_.\-]{12,})`)

// keywordValues are language literals, not credentials. `auth: true` carries no secret, so
// redacting it would buy no privacy and cost the reader the one thing a snippet is for.
var keywordValues = map[string]bool{
	"true": true, "false": true, "null": true, "nil": true, "none": true,
	"yes": true, "no": true, "undefined": true, "empty": true,
}

// carrierWords is the same list as carrier, rejected as a VALUE. Redact is idempotent by
// contract (it runs again over already-redacted model output, judge.go:157) and the carrier
// group is optional: once the token behind it is `<REDACTED>`, which the value class cannot
// match, the engine backtracks and offers `Bearer` itself as the value. A second pass would
// then chew the scheme word, a third the key — the line eroding one call at a time.
var carrierWords = map[string]bool{"bearer": true, "basic": true, "token": true}

// flagUserPassRE catches `curl -u user:pass` / `--user user:pass`. Only the half after the
// colon is secret; the username survives, exactly as it does in urlCredRE.
//
// This does over-redact: `docker run -u root:root` becomes `root:<REDACTED>`. That is the
// deliberate side to err on — a cosmetic loss in an unrelated line against a real
// `admin:s3cr3t` reaching the report. Redact is a pure string function with no file context,
// so "is this line a network command" is not a question it can ask.
// The `-u` arm carries a left boundary so it is a FLAG and not the tail of a word
// (`sort-u a:b`); the boundary sits inside group 1, which the replacement echoes back.
var flagUserPassRE = regexp.MustCompile(`(?i)((?:^|\s)-u\s+|--user[=\s]+)([^\s'":]*:)([^\s'"]+)`)

// entropyTokenRE finds long opaque tokens; those with high Shannon entropy are redacted
// as a catch-all for keyword-less / bespoke secrets (spec §16.3 "high-entropy strings").
//
// `=` is matched only as TRAILING base64 padding, never inside the token. With `=` in the
// character class `MY_THING=Zm9v…MA==` matched as ONE token and the whole line became
// `<REDACTED>` — the value was protected, but the variable name went with it, and which
// setting held the secret is the half the operator acts on.
var entropyTokenRE = regexp.MustCompile(`[A-Za-z0-9+/_\-]{24,}={0,2}`)

// urlCredRE catches credentials embedded in a connection string / URL:
// `scheme://user:PASSWORD@host` (and `scheme://:PASSWORD@host`). The password is often
// short and keyword-less, so neither assignRE nor the entropy pass would catch it.
var urlCredRE = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://[^:@/\s]*:)([^@/\s]+)(@)`)

// Secrets returns s with any secret-looking value replaced by <REDACTED>. Idempotent.
// It is the ONLY way finding snippets are produced (spec §16.3, privacy invariant); detect.Redact
// is this function.
//
// It is two passes, kept apart so the content hash (detect/contenthash.go) can take the first without
// the second: Credentials removes what is ANNOUNCED as a credential — by a key, a flag, a URL
// userinfo or a known token prefix — and redactEntropy removes any long opaque token whatever it
// is. A snippet wants both. A hash wants only the first: a digest cannot leak a high-entropy string
// (that is what high entropy means), while the catch-all also matches a base64 payload, and a
// payload replaced before hashing is a payload that can be swapped without changing the hash.
func Secrets(s string) string { return redactEntropy(Credentials(s)) }

// Credentials is Secrets without the entropy catch-all: every value that something in the
// text announces as a credential.
func Credentials(s string) string {
	out := urlCredRE.ReplaceAllString(s, `$1<REDACTED>$3`)
	out = flagUserPassRE.ReplaceAllString(out, `$1$2<REDACTED>`)
	out = redactFlags(out) // flagSecretRE, flags.go
	out = redactValueHalf(assignRE, out)
	out = redactValueHalf(looseAssignRE, out)
	for _, re := range redactPatterns {
		out = re.ReplaceAllString(out, "<REDACTED>")
	}
	return out
}

// redactEntropy is the catch-all for keyword-less, bespoke secrets: long opaque tokens with high
// Shannon entropy (spec §16.3 "high-entropy strings").
func redactEntropy(s string) string {
	return entropyTokenRE.ReplaceAllStringFunc(s, func(tok string) string {
		// Test the token WITHOUT its base64 padding: `=` carries no information, and counting
		// it would drag the entropy of a genuine key below the threshold that removes it.
		if secretish(strings.TrimRight(tok, "=")) {
			return "<REDACTED>"
		}
		return tok
	})
}

// redactValueHalf replaces the VALUE half of every key/value hit, keeping the key and the
// separator so the report still names WHICH setting held a secret, and leaving language
// literals alone (see keywordValues). re must have the key/separator/value group layout of
// assignRE.
func redactValueHalf(re *regexp.Regexp, s string) string {
	return re.ReplaceAllStringFunc(s, func(m string) string {
		g := re.FindStringSubmatch(m)
		if g == nil {
			return m
		}
		if v := strings.ToLower(g[3]); keywordValues[v] || carrierWords[v] {
			return m
		}
		return g[1] + g[2] + "<REDACTED>"
	})
}

// secretish reports whether a long token looks like an opaque credential: mixed
// letters+digits and high per-char entropy. English words / long identifiers score
// low and are kept; opaque base64/hex tokens are redacted (over-redaction is the safe
// side for a privacy invariant).
func secretish(tok string) bool {
	if len(tok) < 24 {
		return false
	}
	hasLetter, hasDigit := false, false
	for _, c := range tok {
		if c >= '0' && c <= '9' {
			hasDigit = true
		} else if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			hasLetter = true
		}
	}
	if !(hasLetter && hasDigit) {
		return false
	}
	return shannon(tok) >= 3.6
}

func shannon(s string) float64 {
	freq := map[rune]float64{}
	for _, c := range s {
		freq[c]++
	}
	n := float64(len(s))
	h := 0.0
	for _, f := range freq {
		p := f / n
		h -= p * math.Log2(p)
	}
	return h
}
