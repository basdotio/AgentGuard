<!-- SPDX-License-Identifier: MIT -->
# 009 — 发布记录 v0.10.0:闸门放过 ToB 恶意样本的修复(005)和 `--md`(008)

- **来源**:领导 2026-09-20 说"发一版本"。发版不走 proposal,只在这里留记录(领导 2026-09-20 定)
- **包含**:P-005、P-008(自 v0.9.0 起 `complete/` 里「发布」为待发的全部)
- **分支**:`p/release-0.10.0`

## 为什么是 minor

005 收紧了闸门行为(装了 hook 的用户装 skill 时会被拦下更多),008 加了新 flag。行为变化 + 新功能 = v0.10.0。

## changelog(从两份 proposal 的「问题」一节拼,不从提交信息猜;同一段进 ROADMAP)

**v0.10.0 (2026-09-20) — the gate stops trusting what it already flagged, and the report can be pasted.**

- **Gate (P-005).** Three structural rules — `OBF-006`, `SUP-005`, `SUP-006` — now score **high**, so the default gate (`check --fail-on high`,
  the load-time hook) blocks the artifacts they fire on; `OBF-007` stays low. Measured before the change: the four Trail of Bits
  `overtly-malicious-skills` samples were all flagged by the report and all passed the gate at 88/100 — and were then **recorded as trusted**
  by content hash, so the same bytes were never asked about again. Now a pass that still carries a medium finding is announced on every
  load and is never remembered as trusted. Rule counts on one real install of 80 skills: zero hits before and after. Three cross-machine
  checks from the proposal (blocked-count does not rise on the 952-sample machine; `security-guidance` is not blocked; per-rule hit counts there)
  are still listed as unverified in `docs/proposals/complete/005-…`.
- **`--md` (P-008).** `aguard scan|check --md <path>` writes the report as GitHub-flavoured markdown; `--md -` writes it to stdout in place
  of the terminal report, so `aguard check ./new-skill --md - | gh pr comment N --body-file -` posts a vetting result without retyping it.
  Same content and order as the terminal, written before the gate, unaffected by `--verbose`. Every name, path and snippet from the scanned
  tree is emitted inside a code span, so a file named `@someone` or `<img src=…>` cannot ping a person or load an image from the comment.

## 改了哪里

| 处 | 前 | 后 |
|---|---|---|
| `plugin/.claude-plugin/plugin.json` `version` | `0.9.0` | `0.10.0` |
| `.claude-plugin/marketplace.json` 插件条目 `version` | `0.9.0` | `0.10.0` |
| `ROADMAP.md`「Shipped since v0.1.0」 | 范围到 v0.9.0 | 范围到 v0.10.0,上面的 changelog 作首段 |
| `complete/005`、`complete/008`「完成」节 `发布:` | 待发 | v0.10.0 |
| git tag `v0.10.0` | — | **由人打**,PR 合入后 |

不碰 `.go`、不碰 spec、不碰 npm 包内容(`make npm-dist` 从 tag 派生版本)。

## 完成

```
合入:PR(见索引;sha 合入后用 git log --grep P-009 找)
发布:v0.10.0 —— tag、push、npm、GitHub release 由人做,顺序按 .claude/rules/npm.md
证据:TestMarketplaceEntryVersionMatchesPlugin(cmd/aguard/plugin_manifest_test.go)绿;两处 version 0.9.0 → 0.10.0;
      发布字段 待发 → v0.10.0 共 2 份(005、008);make verify 全绿
```
