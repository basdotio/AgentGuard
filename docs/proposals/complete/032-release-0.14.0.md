<!-- SPDX-License-Identifier: MIT -->
# 032 — 发布记录 v0.14.0:工具说明里的密钥(028)、整棵树的删除(029)、信标与远程指令(030)

- **来源**:领导 2026-09-29 说"发 v0.14.0"。发版不走 proposal,只在这里留记录
- **包含**:P-028、P-029、P-030(`complete/` 里「发布」为待发的全部)。P-031 被否决、无产物,不在内
- **分支**:`p/release-0.14.0`

## 为什么是 minor

028 收窄了 `MCP-001` 并加了 `MCP-005`(high),029 收窄了 `FS-003`,030 加了 `EXFIL-007`、`INJ-005`(high)。
`check --fail-on high` 对同一份输入的答案会变:原来拦下的工具目录、缓存清理现在放行,原来放行的信标、远程指令现在拦下。
行为变化 = v0.14.0。没有新 flag、没有删规则,不是 major。

## changelog(从各 proposal 的「问题」一节拼,不从提交信息猜;同一段进 ROADMAP;ROADMAP 里**不放比率**)

见 `ROADMAP.md`「Shipped since v0.1.0」首段 **v0.14.0 (2026-09-29)**。三条要点:工具说明规则从"提到一个密钥词"改成"点名一个敏感位置
或属于用户的密钥",并新增"写死的外部收件人";`rm -rf` 规则从"路径以 / 或 ~ 开头"改成"整棵树的根",认同义旗标;
新增信标(身份命令进外发 URL)和远程取指令两条。另记一条否决:防御性文字的静态修法(P-031)。

语料上的数字(只在这里和各 proposal 里;agent-artifact-corpus @ 4f964622,阈值 high,信誉库关):

| | v0.13.0 发布时 | v0.14.0 |
|---|---|---|
| 恶意 / 300 | 103 | **110** |
| 良性 / 3220 | 137 | **115** |
| hard negative / 19 | 4 | 4 |
| attribution | 65/103(63%) | 68/110(62%) |

- P-028:良性 137 → 121、恶意 103 → 105;真机 `MCP-001`/`MCP-005` 0。
- P-029:良性 121 → 115、恶意不变;真机 `FS-003` 34 → 8(剩下全是测试文件里的注入载荷字符串)。
- P-030:恶意 105 → 110、良性不变;真机修掉一个本地 `/command` 端点的假阳性后 0。

## 改了哪里

| 处 | 前 | 后 |
|---|---|---|
| `plugin/.claude-plugin/plugin.json` `version` | `0.13.0` | `0.14.0` |
| `.claude-plugin/marketplace.json` 插件条目 `version` | `0.13.0` | `0.14.0` |
| `ROADMAP.md`「Shipped since v0.1.0」 | 范围到 v0.13.0 | 范围到 v0.14.0,上面的 changelog 作首段 |
| `complete/028`、`029`、`030`「完成」节 `发布:` | 待发 | v0.14.0 |
| 索引:028/029/030 | PR 待合入;待发 | PR #N 合入;v0.14.0 |
| 索引:031(顺带,过期状态) | 否决;PR 待合入 | PR #35 合入;否决 |
| 索引:本记录一行 | — | 新增 |
| git tag `v0.14.0` | — | **由人打**,PR 合入后,**打在 dev 上合入后的那个提交** |

不碰 `.go`、不碰 spec、不碰 npm 包内容(`make npm-dist` 从 tag 派生版本)。

## 打 tag 的顺序(这次必须照做)

v0.13.0 的 tag 打在了发版分支 rebase 前的提交上(`9a48baa`),而 "Rebase and merge" 在 dev 上生成的是另一个提交(`0ab9808`,
内容相同、SHA 不同)。于是 tag 不在 dev 历史里,dev 上 `git describe --tags` 至今仍报 `v0.12.0-N`,
提交进仓的基线 `run.yaml` 也写着 `tool_version: aguard v0.12.0-N`。这次在**合入之后**:

```bash
cd agent-guard && git fetch origin && git switch dev && git pull --ff-only
git tag v0.14.0 && git merge-base --is-ancestor v0.14.0 origin/dev && git push origin v0.14.0
```

`merge-base --is-ancestor` 不通过就停,不要 push。v0.13.0 那个 tag 要不要挪,仍是维护者的决定,本记录不动它。

## 完成

```
合入:PR(2026-09-29;sha 合入后用 git log --grep P-032 找)
发布:v0.14.0 —— tag、push、npm、GitHub release 由人做,tag 打在 dev 合入后的提交上,顺序按 .claude/rules/npm.md
证据:TestMarketplaceEntryVersionMatchesPlugin(cmd/aguard/plugin_manifest_test.go)绿;两处 version 0.13.0 → 0.14.0;
      发布字段 待发 → v0.14.0 共 3 份(028、029、030);make verify 全绿
```
