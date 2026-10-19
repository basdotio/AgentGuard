<!-- SPDX-License-Identifier: MIT -->
# 074 — EXFIL-001 at medium when every credential is a named key and the destination is not spelled out

- **Source**: the maintainer, 2026-10-18 ("好" to: after the judge, go back to the static rules for precision)
- **Depends on**: P-061 (the destination check this extends), P-064 / the SkillHarm held-out of 2026-10-18 (the malicious sets measured)
- **Branch**: `p/074-named-key-unreadable-destination` (the code is reverted in the same branch; the measurement and this record stay)
- **Rejected**: 2026-10-19 — it cost 3 of 123 on the only wild malicious held-out; the maintainer agreed ("好") with not trading that recall for this precision. See Done.

## Problem

Static high on benign skill trees, by rule, main at P-069 over all 2,880 benign and 393 malicious skill trees of the corpus:
`EXFIL-001` 100 benign (81 of them flagged by nothing else) against 24 malicious (11 alone); the next rule, `EXEC-001`,
23 / 21 against 10 / 8; `FS-001` the other way, 18 / 4 against 42 / 27. Of the 100 benign `EXFIL-001` findings, 72
read a NAMED key (`WX_SECRET`, `PERPLEXITY_API_KEY`, `$DD_API_KEY` in a documented `curl`), the shape of every API client;
of the 24 malicious, 3 did — the rest dump the environment (`env | grep TOKEN`, `env | curl`) or read a key file.

## Initial direction

A third case beside P-061's two downgrades: every credential line in the file names its secret (`credUnnamed` false),
at least one off-box destination is unreadable (a base URL from configuration, a parameter), and every destination the
file does spell out carries a credential's service token → `EXFIL-001` at medium, not high. A named key sent to a
spelled-out host that is not its service stays high: that is the theft shape. The change, for the record:

```go
+	// credUnnamed is set by any credential line that names no secret: a whole-environment dump
+	// (`env |`, `dict(os.environ)`), a credential file (`~/.ssh/id_rsa`, `.env`). Only a file whose
+	// every credential read names its secret can be the API-client shape (P-074).
+	credUnnamed bool
+		if len(credentialServiceTokens(raw)) == 0 {
+			c.credUnnamed = true
+		}
+// namedToUnknown reports the API-client shape (P-074): every credential the file reads is a named
+// secret, the file sends somewhere it does not spell out (a base URL from config or a parameter), and
+// every destination it DOES spell out is the credential's own service. Measured on 2,880 benign skill
+// trees: 100 carried EXFIL-001 at high, 72 of them a named key; of 393 malicious, 24 and 3. Data may
+// still leave — so it stays a finding — but nothing in the file says it left for anyone else.
+func (c chain) namedToUnknown() bool {
+	if c.credUnnamed || len(c.credService) == 0 || !c.destUnreadable {
+		return false
+	}
+	for _, h := range c.destHosts {
+		if !hostMatchesService(h, c.credService) {
+			return false
+		}
+	}
+	return true
+}
+
+		case c.namedToUnknown():
+			f.Severity = model.SevMedium
+			f.Why = "The same file reads named credentials and makes outbound requests to a destination it does not spell out (a base URL from configuration or a parameter); every destination it does spell out belongs to the credential's own service. This is how an API client is written — and also how a key could be sent elsewhere — so it is reported at medium: a surface to check, not a chain the file shows."
```

## Measured

Static only (no judge), `-j 8`, corpus `af3714a2` (the samples of `6c7eef52`), main `33a3f0b` against the change;
`results/aguard/2026-10-19-p074-{skills,other-surfaces,heldout}-{main,changed}/`. Static runs are deterministic, so
there is no raw archive: the binary and the corpus commit rebuild them.

| list | n | main | changed |
|---|---:|---:|---:|
| benign skill trees (development) | 2,880 | 158 | **133** |
| benign trees of the other surfaces (MCP servers, hooks, connectors, permissions) | 1,287 | 133 | **109** |
| malicious skill trees + other surfaces (development) | 393 + 147 | 92 + 35 | 92 + 35 |
| held-out ClawHub benign (totals) | 216 | 6 | 5 |
| held-out SkillHarm, 10 per risk past the vendored 20 (totals) | 120 | 3 | 3 |
| **held-out ClawHub malicious (totals)** | 123 | **68** | **65** |

**Reading.** On development data the change is what it was meant to be: 49 fewer benign trees at high across the
corpus, about a third of `EXFIL-001`'s benign load, and no malicious tree lost. On the one wild malicious set it loses
3 of 123 — trees that read a named key and send it to a destination built at run time, which is the shape it lowers,
because real malicious skills write it too. The three were not read: reading them would spend the held-out. They are
still reported, at medium, and no longer meet the default `--fail-on high`.

The two sides are not the same size: 49 of 4,167 benign (1.2%) against 3 of 123 wild malicious (2.4%), and a missed
attack costs more than a flag someone reads. The rule cannot tell an API client from a key thief when the file does not
say where the key goes; lowering it opens exactly that shape. Not shipped. A later change on this rule should make the
report say WHICH case a finding is (named key, destination not spelled out) rather than move its severity.

## Done

```
Rejected: 2026-10-19 — benign high −49 across the corpus, development malicious unchanged, wild malicious held-out 68 → 65; code reverted, runs kept
Evidence: results/aguard/2026-10-19-p074-skills-{main,changed}: benign 158 → 133 of 2,880, malicious 92 → 92 of 393
Evidence: results/aguard/2026-10-19-p074-other-surfaces-{main,changed}: benign 133 → 109 of 1,287, malicious 35 → 35 of 147
Evidence: results/aguard/2026-10-19-p074-heldout-{main,changed}: ClawHub malicious 68 → 65 of 123, benign 6 → 5 of 216; SkillHarm held-out 3 → 3 of 120
```
