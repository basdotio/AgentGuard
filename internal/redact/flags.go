// SPDX-License-Identifier: MIT
package redact

import (
	"regexp"
	"strings"
)

// flagSecretRE catches a credential passed as a flag ARGUMENT, where the value IS the secret.
// urlCredRE only ever covered `scheme://user:pass@host`, and a skill script is far likelier to
// spell it as a flag. It is the ONE list of flags that announce a credential: redact.Argv and the
// content hash (detect.redactTree, through Announced) ask Credentials, so they read the same flags
// with no list of their own (invariant #3).
//
// Group 2 is the flag's name, and it has two tiers (P-040):
//
//   - the eight flags named outright (namedFlagRE): the name IS the credential word, and whatever
//     follows is redacted, as it always was;
//   - a flag that ENDS in a credential word: the password family or `secret` after any qualifier
//     (`--db-pass`, `--admin_password`, `--client-secret`), `key` bare or after one qualifier that
//     makes it a credential (`--private-key`, `--secret-key`, `--license-key`), and the bare `--pat`,
//     `--bearer`, `--credential(s)`. These announce a value unless it is plainly not a secret
//     (notASecret).
//
// What is deliberately NOT here, measured on the corpus and a real ~/.claude (P-040):
//
//   - a flag whose credential word is not its last (`--key-file`, `--secret-id`, `--token-endpoint`,
//     `--password-stdin`): the last-word anchor excludes the whole family with no suffix list;
//   - `key` after an open qualifier: `--meta_key`, `--space-key`, `--assignment-key` name an index,
//     not a credential;
//   - `token` compounds: `--page-token`, `--from-token`, `--sell-token` outnumbered the credential
//     ones nine to one. looseAssignRE still takes `--auth-token <v>` from 12 characters;
//   - compounds without a separator (`--dbpass`): the separator is what keeps `--bypass` and
//     `--multipass` out.
//
// Single-letter flags other than `-u` are deliberately absent: `-p` is a password to mysql, a
// port to nc and "pretty" to half a dozen others, so matching it would redact arguments that
// are not secrets in files that have nothing to do with credentials. Measured: 2,136 values after
// `-p`, 1,731 of them `mkdir -p <dir>`, none a credential.
var flagSecretRE = regexp.MustCompile(`(?i)(--(` +
	`(?:[a-z0-9]+[-_])*(?:password|passwd|passphrase|pass|pwd|secret)` +
	`|(?:(?:api|app|private|secret|access|auth|client|license|master|encryption|signing)[-_]?)?key` +
	`|token|api[_-]?key|access[_-]?token|pat|bearer|credentials?` +
	`)[=\s]+)([^\s'"]+)`)

// namedFlagRE is the first tier: the flags flagSecretRE named before P-040, whose value is redacted
// whatever its shape.
var namedFlagRE = regexp.MustCompile(`(?i)^(?:password|passwd|passphrase|pass|token|secret|api[_-]?key|access[_-]?token)$`)

// keyFileRE is a value that names a file holding a key rather than being one.
var keyFileRE = regexp.MustCompile(`(?i)\.(?:pem|key|crt|cer|der|p12|pfx|jks|pub|gpg|asc|json|txt|env)$`)

// redactFlags replaces the value of every flagSecretRE hit, except where a second-tier flag is
// followed by something that is plainly not a secret, or by nothing on its own line.
//
// The line rule is the second tier's too. Across a line break the next word is the next line, not the
// flag's argument: the judge's MCP excerpt writes an element a flag does not announce on its own line
// (`args=--private-key` then `args=~/.ssh/id_rsa`), and the excerpt's final whole-field Redact (P-037)
// would read `args=~/.ssh/id_rsa` as the value — not a path, since it starts with `args=` — and erase
// the line the exemption kept. The eight named flags keep reading across it, as P-036 pinned.
func redactFlags(s string) string {
	return flagSecretRE.ReplaceAllStringFunc(s, func(m string) string {
		g := flagSecretRE.FindStringSubmatch(m)
		if g == nil {
			return m
		}
		if !namedFlagRE.MatchString(g[2]) {
			if sep := g[1][len("--")+len(g[2]):]; strings.ContainsAny(sep, "\n\r") || notASecret(g[2], g[3]) {
				return m
			}
		}
		return g[1] + marker
	})
}

// notASecret is the exemption of the second tier (P-040): a `--no-*` switch takes no value (the word
// after it is a positional), and a URL or a path is where a credential lives, not the credential. Both
// are what the reader and the judge need to see — `--private-key ~/.ssh/id_rsa` is evidence — and what
// the content hash must keep: forgetting a path would let an approval survive a swapped key file.
// It is negative only: a password is whatever its owner typed, so no length or entropy test decides
// that a value IS one. The cost, accepted: a short standard-base64 secret that starts with `/`.
func notASecret(flag, v string) bool {
	switch {
	case strings.HasPrefix(strings.ToLower(flag), "no-"), strings.Contains(v, "://"), keyFileRE.MatchString(v):
		return true
	}
	for _, p := range []string{"/", "./", "../", "~/"} {
		if strings.HasPrefix(v, p) {
			return true
		}
	}
	return false
}
