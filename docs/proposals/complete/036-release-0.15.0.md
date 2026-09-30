<!-- SPDX-License-Identifier: MIT -->
# 036 — 发布记录 v0.15.0:每一票的严重度(035)、评分器读工具自己声明的分母(034)

- **来源**:领导 2026-09-29 说"发 v0.15.0"。发版不走 proposal,只在这里留记录
- **包含**:P-034、P-035(`complete/` 里「发布」为待发的全部)
- **分支**:`p/release-0.15.0`

## 为什么是 minor

035 给每条 `samples > 1` 的判官发现在理由里加了一段新的可见输出(`[severities: …]`),是向后兼容的新功能;
发现本身的严重度、共识门槛、折叠口径都没变,`check --fail-on` 与 `--fail-on-llm` 对同一份输入的答案**不变**。
034 只动量测台(`baselines/`、`make bench`),不进二进制。没有删 flag、没有删规则,不是 major;有新的用户可见输出,不是 patch。

同一天早些时候(只有 034 待发)问过一次要不要发 v0.15.0,当时发出去的二进制与 v0.14.0 行为完全相同,决定不发、让 034 随下一版走——就是这一版。

## changelog(从各 proposal 的「问题」一节拼,不从提交信息猜;同一段进 ROADMAP;ROADMAP 里**不放比率**)

见 `ROADMAP.md`「Shipped since v0.1.0」首段 **v0.15.0 (2026-09-29)**。两条要点:`samples: 3` 时判官发现把每张同意票的严重度按采样顺序
写进理由,发现自身仍取第一票(改取多数票在记全票面的一次运行上量过,判定一个不变,未采用);语料评分器读工具在 `out_of_scope`
里的声明,`make bench` 的成绩单并排给全量与声明内两个分母。

数字只在各 proposal 与 `baselines/results/` 里:

- P-034:`make bench`(语料 97e5af00)malicious, all 110 of 300、malicious, in scope 89 of 169;verdicts 与此前基线逐字节同。
- P-035:W2 的二进制重跑同 224 个(gpt-4.1-mini、`samples: 3`),同一份 raw 上多数票 vs 第一票 124 条带权发现里换档 2 条、
  判定翻转 0 / 224;两次同配置运行之间判官自己的旗标差 7 / 100(随机恶意)、1 / 100(随机良性)。

## 改了哪里

| 处 | 前 | 后 |
|---|---|---|
| `plugin/.claude-plugin/plugin.json` `version` | `0.14.0` | `0.15.0` |
| `.claude-plugin/marketplace.json` 插件条目 `version` | `0.14.0` | `0.15.0` |
| `ROADMAP.md`「Shipped since v0.1.0」 | 范围到 v0.14.0 | 范围到 v0.15.0,上面的 changelog 作首段 |
| `complete/034`、`035`「完成」节 `发布:` | 待发 | v0.15.0 |
| 索引:034/035 | PR 待合入;待发 | PR #37 / #40 合入;v0.15.0 |
| 索引:027、032(顺带,过期状态) | PR 待合入;tag 由人打 | PR #31 / #36 合入;tag 各在哪个提交 |
| 索引:本记录一行 | — | 新增 |
| git tag `v0.15.0` | — | **由人打**,PR 合入后,**打在 dev 上合入后的那个提交** |

不碰 `.go`、不碰 spec、不碰 npm 包内容(`make npm-dist` 从 tag 派生版本)。

## 打 tag 的顺序(照 v0.14.0 的做法)

"Rebase and merge" 在 dev 上生成的提交和发版分支上的不是同一个 SHA,tag 必须在**合入之后**、打在 dev 上
(v0.13.0 就是打在分支提交 `9a48baa` 上,至今不在 dev 历史里;v0.14.0 照下面做了,在 `cce362f`):

```bash
cd agent-guard && git fetch origin && git switch dev && git pull --ff-only
git tag v0.15.0 && git merge-base --is-ancestor v0.15.0 origin/dev && git push origin v0.15.0
```

`merge-base --is-ancestor` 不通过就停,不要 push。之后按 `.claude/rules/npm.md` 的顺序发 npm(四个平台包先、launcher 最后,
每包先 `npm view` 判存在),再 `gh release create`。

## 完成

```
合入:PR(2026-09-29;sha 合入后用 git log --grep P-036 找)
发布:v0.15.0 —— tag、push、npm、GitHub release 由人做,tag 打在 dev 合入后的提交上,顺序按 .claude/rules/npm.md
证据:TestMarketplaceEntryVersionMatchesPlugin(cmd/aguard/plugin_manifest_test.go)绿;两处 version 0.14.0 → 0.15.0;
      发布字段 待发 → v0.15.0 共 2 份(034、035);make verify 全绿
```
