<!-- SPDX-License-Identifier: MIT -->
# 098 — On the server pass, a credential-read that reaches for nothing beyond the server's own configuration weighs medium

- **Source**: the maintainer, 2026-10-22 ("接着查判官在良性服务器上那 63 个误报", then "好" to the trade-off below)
- **Depends on**: P-062 (the server pass's own-service control this extends), P-074 (the unreadable-destination ground this does not reopen), P-049 (categories must be shown by the text)
- **Branch**: `p/098-server-credential-overreach`

## Problem

v0.31.0 with the judge, on the 543 benign server trees of P-059's denominator still in the corpus, flags 156 high; 93 of
those are static and 63 the judge's alone, every one `LLM-010`. 46 of the 63 rest on `credential-read`, and read one by one
they are a server reading what it is configured with — `os.getenv("STRIPE_API_KEY")`, `settings.atproto_password`,
`load_dotenv()` — to log in to its own service. P-062's own-service control lowers that only when the static chain can
read the destination, and these servers build it at run time or call through a client library. On Cisco's 127 malicious
servers the 15 flagged by `credential-read` alone read something else: the environment whole, `/etc/passwd`,
`/proc/<pid>/environ`, the metadata service, the user database.

Iron rule #2 is not in question: this lowers the judge's own finding, as P-049, P-062 and P-089 do.

## Initial direction

Two file-level predicates in `detect`, computed over the FULL file when the excerpt is built (beside `goesHome`):

- `CredentialOverreach` — a code line names a source no server owns: another program's secrets (`.ssh/`,
  `.aws/credentials`, `id_rsa`, `authorized_keys`, `.kube/config`, `.docker/config.json`, `.git-credentials`, `.netrc`),
  system account and network files (`/etc/passwd|shadow|gshadow|sudoers|group|hosts|resolv.conf|hostname`), anything
  under `/proc/` or `/sys/`, `/environ`, a metadata service (`169.254.169.254`, `metadata.google.internal`,
  `/latest/meta-data`), `getpwall|getpwnam|getpwuid`, `/var/log/`, the keychain, a browser cookie store, a hidden file
  in a world-writable temp directory; or the whole environment enumerated, where a child process given a copy
  (`{...process.env, X}`, `os.environ.copy()`) does not count unless the same line sends or serialises it.
- `CredentialSentElsewhere` — the file reads a credential (the chain's leg, or a `password|secret|token|api_key|…` name
  anywhere) and a destination it spells out is not that credential's service. A credential with no service in its name
  has none to match, so any spelled-out off-box host counts. An unreadable destination does not: that is P-074's ground.

On the server pass, a grounded `credential-read` vote in a file where both are false weighs medium, with
`[category not shown by the text: credential-read (only the server's own configuration)]`. No prompt or excerpt
change (`judge-prompt`, `judge-excerpt` unchanged).

## Done criteria

- [x] `TestRun_ServerOwnConfigurationCredentialReadWeighsMedium`: medium for an own key with no destination, a settings
  password for its own login, a child process given a copy of the environment; high for the environment enumerated,
  `/etc/passwd`, another process's environ, `/proc/cpuinfo`, a credential kept in a hidden temp file, the metadata
  service, a settings password posted to a spelled-out foreign host. `TestRun_ServerOwnKeyCredentialReadWeighsMedium`
  (P-062) unchanged and passing — its "own key to an unrelated host" case is what `CredentialSentElsewhere` exists for
- [x] Live at `samples: 3`: Cisco's 127 malicious servers, the 543 benign server trees, and the 12 other malicious MCP
  samples (five real npm packages from DataDog's set); main (v0.31.0) against the branch
- [x] `docs/llm-judge.md` / `.zh-CN`, `.claude/rules/judge.md`; `make verify`

## Out of scope

The other `LLM-010` categories on benign servers (`remote-execution` 10, `software-source` 4, `exfiltration` 3). The
static `EXFIL-001` on servers. Skills: the server pass runs on server source only.

## Must not claim

Not "`LLM-010` false positives fixed": benign servers flagged high stay above the skill rate. A server that reads its own
configured key and sends it to a destination built at run time is lowered — the same shape P-074 declined to lower
statically; here it stays reported at medium, and the judge's other categories still apply. The two malicious servers
lowered are miscategorised (an SQL injection, a directory traversal) and would need their own category to be caught high.

## Measured

Live, gpt-4.1-mini at `samples: 3`, corpus `6d7aa48e`. Main is v0.31.0 as released (run 2026-10-22); the branch is the
final version (run 2026-10-23). Results `results/aguard/2026-10-23-llm-gpt-4.1-mini-p098-{cisco-servers,benign-servers,other-malicious-mcp}-{main,branch}-s3/`;
raw archive `aguard-judge-raw-2026-10-p098.tar.gz` on release v0.31.0.

| set | n | main | branch | not flagged because of this change |
|---|---:|---:|---:|---:|
| other malicious MCP samples (12; 5 real npm packages) | 12 | 6 | 6 | **0** |
| Cisco malicious servers | 127 | 107 | 103 | **2** |
| benign server trees (P-059's 547 less four no longer in the corpus) | 543 | 156 | 124 | **43** |

"Not flagged because of this change" counts branch samples whose only high finding was lowered with this note — the
change's own effect; the totals also carry a rerun's movement. A first version without `/proc/`, `/sys/` and hidden temp
files cost six Cisco servers (`/proc/mounts`, `/proc/cpuinfo`, credentials kept in `/tmp/.auth_cache`); adding them
brought four back and none of the 41 benign trees it had lowered.

**Reading.** Benign server trees flagged high, in expectation: 28.7% → 20.8%. The cost is two synthetic servers the judge
had put in the wrong category; no real malicious sample moved.

## Done

```
Merged: PR #124 (2026-10-23; find the sha with git log --grep P-098)
Evidence: results/aguard/2026-10-23-llm-gpt-4.1-mini-p098-*-s3; raw archive on v0.31.0, sha256 d3a83c22012ddcca44b73c90f1aebe9442e41e5858aa937c1ea348aa2819d997
Evidence: schema 88 files / 36,425 lines
```
