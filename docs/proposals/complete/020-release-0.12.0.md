<!-- SPDX-License-Identifier: MIT -->
# 020 — 发布记录 v0.12.0:配置面四条规则(016)、判官 LLM-009 只提示(019)、量测台与第二列(015、017)

- **来源**:领导 2026-09-26 说"发版"。发版不走 proposal,只在这里留记录
- **包含**:P-015、P-016、P-019(`complete/` 里「发布」为待发的全部)+ P-017(已合入 [PR #17](https://github.com/basdotio/agent-guard/pull/17),
  代码随本版发布;**它的 proposal 文件仍在 `design/`、没有「完成」节**,由作者补,本记录不动它)
- **分支**:`p/release-0.12.0`

## 为什么是 minor

016 加了两条 high 规则(`EXEC-010`、`PERM-007`)并开始收集 settings 的 `env` 块,`check --fail-on high` 对同一份输入的答案会变;
019 改了 `overall_effective` 和 `--fail-on-llm` 对 `LLM-009` 的处理,`--llm` 用户看到的有效分会变。行为变化 = v0.12.0。
015、017 是维护者工具(`baselines/`),不影响二进制。没有新 flag,不是 patch。

## changelog(从各 proposal 的「问题」一节拼,不从提交信息猜;同一段进 ROADMAP;ROADMAP 里**不放比率**)

见 `ROADMAP.md`「Shipped since v0.1.0」首段 **v0.12.0 (2026-09-26)**。四条要点:配置面四种明写的攻击形状有了规则,settings `env` 块开始被收集,
三种与良性字面无法区分的形状明确不做;判官的 MCP 配置问题照问照报但永不升级;`hack/corpus-runner` 换成 `baselines/`,127 个从未交给二进制的
样本进了分母;cc-audit 成为对照表的第二列。另有两条非 proposal 修复:文档链接测试不再走 git 忽略路径(PR #18),规划文档与 Makefile 对"benchmark
不进 CI"说法一致(PR #15)。

语料上的数字(只在这里和各 proposal 里;agent-artifact-corpus @ 5c3db6ee,阈值 high,信誉库关):

- P-015:分母 173 → 300(127 个 MCP 服务器源码交给 `check`,14 个 high),召回 71/173 → 85/300;良性 147/3220 不变;hard negative 4/19。
- P-016:恶意 85 → 87/300(`mcp-env-hijack`、`userpromptsubmit-perm-rewrite`);良性 147 不变。
- P-017:cc-audit v3.23.9 同一份语料 185/300,良性 1191/3220,attribution 39%(aguard 59%);provisional。
- P-019:glm-5.3-flash、`samples: 1`、819 子集(300 恶意 / 19 hn / 500 良性):判官多拦的良性 7 → 2、hard negative 5 → 4、恶意 151 → 149;
  非 Cisco 173 上静态 73、加判官 135、cc-audit 111。判官仍默认关,`--fail-on-llm` 前置的方差实验与全量良性未做。

## 改了哪里

| 处 | 前 | 后 |
|---|---|---|
| `plugin/.claude-plugin/plugin.json` `version` | `0.11.0` | `0.12.0` |
| `.claude-plugin/marketplace.json` 插件条目 `version` | `0.11.0` | `0.12.0` |
| `ROADMAP.md`「Shipped since v0.1.0」 | 范围到 v0.11.0 | 范围到 v0.12.0,上面的 changelog 作首段 |
| `complete/015`、`016`、`019`「完成」节 `发布:` | 待发 | v0.12.0 |
| 索引三行 + 本记录一行 | 待合入;待发 | 合入;v0.12.0 |
| git tag `v0.12.0` | — | **由人打**,PR 合入后 |

不碰 `.go`、不碰 spec、不碰 npm 包内容(`make npm-dist` 从 tag 派生版本)。

## 完成

```
合入:PR(2026-09-26;sha 合入后用 git log --grep P-020 找)
发布:v0.12.0 —— tag、push、npm、GitHub release 由人做,顺序按 .claude/rules/npm.md
证据:TestMarketplaceEntryVersionMatchesPlugin(cmd/aguard/plugin_manifest_test.go)绿;两处 version 0.11.0 → 0.12.0;
      发布字段 待发 → v0.12.0 共 3 份(015、016、019),017 无该字段(见「包含」);make verify 全绿
```
