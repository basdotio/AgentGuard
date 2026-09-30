<!-- SPDX-License-Identifier: MIT -->
# 014 — 发布记录 v0.11.0:外泄链两条腿各修一处(010、012、013)与语料 runner(011)

- **来源**:领导 2026-09-21 说"发"。发版不走 proposal,只在这里留记录
- **包含**:P-010、P-011、P-012、P-013(自 v0.10.0 起 `complete/` 里「发布」为待发的全部)
- **分支**:`p/release-0.11.0`

## 为什么是 minor

010 让闸门拦下更多(shell 整环境导出、`.env` 类文件读取成链),012 和 013 让闸门拦下更少(文档里的 URL、MCP 配置的鉴权头不再成链),
三条都改变 `check --fail-on high` 对同一份输入的答案;011 是维护者工具。行为变化 = v0.11.0。没有新 flag,所以不是"新功能",但改变判定结果
不能当 patch 发。

## changelog(从四份 proposal 的「问题」一节拼,不从提交信息猜;同一段进 ROADMAP;**不放比率**——四条的「不能说什么」都要求数字只进 proposal 和 planning 文档)

见 `ROADMAP.md`「Shipped since v0.1.0」首段 **v0.11.0 (2026-09-21)**。四条要点:凭证腿认 shell 整环境导出与凭证 dotfile 的读取;
指令文件里 URL 字面量不是请求,shell 围栏行连接按 shell 语义;MCP 配置单元里 URL 字面量不是出网动作,工具说明不跑链;`make bench` 语料 runner。

语料上的数字(只在这里和各 proposal 里):agent-artifact-corpus @ 7e924371,阈值 high,信誉库关 —— 良性 3,220 个误拦 219 → 147
(skillmd-138k 133 → 93 / 1996;harvested 35 → 8 / 387;skillet-wild 26 → 23 / 474;automatelab 22 → 20 / 353);
恶意能看到的 173 个拦下 60 → 71;hard-negative 4/19 不变;127 个 MCP 服务器源码在输入模型之外。

## 改了哪里

| 处 | 前 | 后 |
|---|---|---|
| `plugin/.claude-plugin/plugin.json` `version` | `0.10.0` | `0.11.0` |
| `.claude-plugin/marketplace.json` 插件条目 `version` | `0.10.0` | `0.11.0` |
| `ROADMAP.md`「Shipped since v0.1.0」 | 范围到 v0.10.0 | 范围到 v0.11.0,上面的 changelog 作首段 |
| `complete/010`、`011`、`012`、`013`「完成」节 `发布:` | 待发 | v0.11.0 |
| 索引四行 | 待合入;待发 | 合入;v0.11.0 |
| git tag `v0.11.0` | — | **由人打**,PR 合入后 |

不碰 `.go`、不碰 spec、不碰 npm 包内容(`make npm-dist` 从 tag 派生版本)。

## 完成

```
合入:PR #13 https://github.com/basdotio/agent-guard/pull/13(2026-09-21;sha 合入后用 git log --grep P-014 找)
发布:v0.11.0 —— tag、push、npm、GitHub release 由人做,顺序按 .claude/rules/npm.md
证据:TestMarketplaceEntryVersionMatchesPlugin(cmd/aguard/plugin_manifest_test.go)绿;两处 version 0.10.0 → 0.11.0;
      发布字段 待发 → v0.11.0 共 4 份(010、011、012、013);make verify 全绿
```
