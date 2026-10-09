<!-- SPDX-License-Identifier: MIT -->
# 002 — 报告不说自己是哪一版规则跑出来的,两份报告分不清是规则变了还是输入变了

- **来源**:新发现(2026-10-09):同一份内容两次扫描得分不同时,报告里没有任何东西说明规则表变没变;
  `rules_version` 是离线复算 `overall` 的前提。移植自旧仓 agent-guard 的 P-043(私有仓)
- **依赖**:无
- **分支**:`p/002-rules-version`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

一份 `scan --json` 关于"它是谁跑出来的"只有一个字段:`tool_version`,内容是构建时的版本号
(`make build` 注入 `git describe`,如 `v0.17.0-8-g4eb7716`、`-dirty`;`go install …@latest` 从 build info 取模块版本;
在 checkout 里直接 `go build` 则是 `dev`)。它标的是**一次提交**,不是**一套规则**,两个方向都错:

| 情形 | 实测(本仓 origin/main dec64ca,`git rev-list` / `git describe`) | 后果 |
|---|---|---|
| 版本号变了,规则没变 | v0.16.0 → v0.17.0:4 个提交,**0 个**碰 `internal/detect/` | 两份报告 `tool_version` 不同、发现不同时,读者没法排除"是规则改了";只能去翻 changelog |
| 规则变了,规则表文件没变 | v0.17.0 → v0.18.0:10 个提交里 `a882e42` 让 slash command 跑全套规则 —— 改的是 `detect.go` / `collect.go` 的角色判定,`rules.go` / `rules_data.go` 一字未动;只有 `4eb7716`(`EXEC-012`)碰了规则表 | 按"规则表文件动没动"判断会漏掉前者 |
| 规则变了,版本号看起来一样 | `git describe`:`a882e42`(slash command 跑全套规则)和 `5a60282`(信誉名单续期,不碰检测)都是 `v0.17.0-1-g…`;`881c417` 是 `v0.17.0-7-g881c417`,下一个提交 `4eb7716` 加了 `EXEC-012`,是 `v0.17.0-8-g4eb7716` | 只差一个后缀的两个版本号,读者没法知道中间隔没隔着一条规则 |
| 不经 `make` 的构建 | 本仓 checkout 里 `go build ./cmd/aguard` → `aguard dev (commit none, built unknown)`,`tool_version: "dev"` | 完全没有信息 |

`aguard version`、`docs/rules.md` 页眉也都不说规则表是哪一版。于是:

- `baselines/results/` 的成绩单要归因到规则集(`docs/corpus-benchmark.zh-CN.md` §6:"benchmark 头部要钉规则集哈希,否则数字无法归因";
  同文件的成绩单头部样例早就留了 `rules_version: <sha256 前 12 位>` 一栏),今天只能钉到提交;
- 一个用户拿两份报告来问"为什么这条发现没了",回答的第一步——"规则一样吗"——没有答案;
- 想对着产出一份报告的规则离线复算它的 `overall`,先得知道是哪套规则,今天报告里没有这个信息。

## 初步方向

在 `internal/detect` 里算 `RulesVersion()`:对 `builtinRules()` 按引擎遍历顺序,把每条的 ID、维度、严重度、
advisory 与几个 `*Only` 标志、正则源码(`re` / `except`)做规范化哈希,再折进一个手动递增的 `rulesEpoch`
(规则表之外的检测逻辑变了就加一)。正则源码字段是未导出的,所以只能在 `detect` 包里算,`hack/gen-rules` 拿不到。

它出现在三处:`ScanResult.RulesVersion`(`scan --json` / `check --json`)、`aguard version` 行末、`docs/rules.md` 页眉
(现有的漂移检查随之把"改了规则却没跑 `make docs`"拦下)。只交付 `rules_version` 这一个字段;
输入摘要(`inputs_digest`)和离线复核命令(`aguard verify`)不在本条。
