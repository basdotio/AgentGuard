// SPDX-License-Identifier: MIT
// Package detect runs the static rule engine over collected artifacts and produces
// findings. Secret redaction happens HERE, before any snippet is stored in a finding —
// so the report AND the (optional M5) LLM judge consume the same redacted view and a
// real credential never leaves the machine (spec §16.3, the privacy invariant). The
// implementation itself lives in internal/redact, one level below collect, so the notes the
// collector writes go through the same one.
//
// The content hash of hooks, MCP servers and permission lists is computed here too
// (contenthash.go), over Redact's credential half: no artifact's Hash may be a digest of
// credential material, and none may depend on where the configuration sits on disk.
package detect

import (
	"strings"

	"github.com/basdotio/AgentGuard/internal/redact"
)

// Redact returns s with any secret-looking value replaced by <REDACTED>. Idempotent.
// This is the ONLY way finding snippets are produced (spec §16.3, privacy invariant).
//
// It is redact.Secrets, and must stay a delegation: the patterns moved to internal/redact so the
// collector — which this package depends on, and so cannot import it — redacts with the SAME
// implementation. A pattern added here instead of there is a second redactor, and two redactors
// drift (see that package's doc).
func Redact(s string) string { return redact.Secrets(s) }

// RedactArgv is Redact for an argument vector — redact.Argv: every element redacted, and one that
// follows a flag read together with that flag, so `"--api-key", "<key>"` loses the key as the one
// string `--api-key <key>` does (P-036). The judge's MCP excerpt calls it. A delegation, like Redact.
func RedactArgv(args []string) []string { return redact.Argv(args) }

// redactCredentials is Redact without the entropy catch-all — redact.Credentials, the half the
// content hash takes (contenthash.go; why only that half is explained on redact.Secrets).
func redactCredentials(s string) string { return redact.Credentials(s) }

// announcedArg is redact.Announced: whether a flag announces the argument after it, and where in that
// argument the value starts. The content hash forgets that span whole, as RedactArgv does for the
// judge (P-039) — the same decision, not a second reading of the flag patterns.
func announcedArg(flag, arg string) (int, bool) { return redact.Announced(flag, arg) }

// redactClip builds a finding snippet: REDACT FIRST, then bound the length. Every snippet
// in every finding goes through here — the two steps are collapsed into one call precisely
// so the order cannot be got wrong at a call site.
//
// The order is the whole point (spec §16.3). Clipping first cuts a credential in half, and
// half a credential no longer matches the patterns that would have removed it: a token whose
// surviving head is shorter than the entropy pass's 24-character minimum shipped to the
// report in the clear. That is not hypothetical — a URL long enough to push its query string
// across the cap does it, and the leaked head is the start of the key, the useful half.
// Redacting while the token is still whole means what gets truncated is already <REDACTED>.
func redactClip(s string) string { return clip(Redact(s)) }

// trimLine trims surrounding whitespace and a trailing CR so line-oriented matching
// is CRLF-safe.
func trimLine(s string) string { return strings.TrimSpace(strings.TrimSuffix(s, "\r")) }
