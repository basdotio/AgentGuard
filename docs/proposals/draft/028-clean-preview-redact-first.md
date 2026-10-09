<!-- SPDX-License-Identifier: MIT -->
# 028 — The restore preview of `clean --undo` can print the head of a secret that crosses its 100-byte column limit: it cuts first and redacts second

- **Source**: new finding, made while P-018 was implemented (P-018 moved `Redact` into `internal/redact` and listed
  `internal/clean` among the callers that stay unchanged; reading those callers showed the order is inverted there)
- **Depends on**: none
- **Branch**: `p/028-clean-preview-redact-first`

<!-- No "Status" line: the directory the file sits in is its state (draft/ design/ complete/ rejected/), see README.md. -->

## Problem

Before `clean --undo` (and its `--dry-run`) puts a quarantined skill back, `internal/clean/preview.go` prints what is in
the box: the first three non-empty lines of a file, the first five entry names of a directory, a symlink's target, and
the titles of the worst static findings. Every one of those four lines is written as `detect.Redact(clip(x))`: the text
is first cut to 100 bytes plus `…`, and only the cut piece is redacted.

Invariant #3 says the opposite order, everywhere: "redact first, then truncate" — `detect.redactClip` exists precisely so
that order cannot be got wrong at a call site, and its comment describes this exact failure. A secret that starts before
column 100 and ends after it is cut into a head that no longer matches its pattern (a GitHub token needs 20 characters
after `ghp_`, a Google key 30 after `AIza`) and is shorter than the entropy pass's 24-character floor, so the head of the
key — the useful half — reaches the terminal, and from there a CI log or a pasted transcript.

Who is affected: an operator restoring their own quarantined skill whose first lines (a long `curl` with a key in the
query string, a config line) or whose file names carry a credential past column 100. The preview's own comment says
content is redacted because a preview that leaked a credential "would be a self-inflicted version of the leak the
scanner reports"; for a secret that crosses the column limit, that is what it does today.

## Initial direction

Redact before cutting, through one local helper of the same shape as `detect.redactClip` (`clip(detect.Redact(s))`),
used by all four lines; the 100-byte width and the `…` mark stay as they are. Measure first which of the four lines can
actually carry text that was not written by the tool, and look for the same inversion elsewhere in the repository.
