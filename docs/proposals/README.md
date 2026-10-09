<!-- SPDX-License-Identifier: MIT -->
# Proposals

每份 proposal 回答"要做什么、做到什么程度算完、明确不做什么"。新建照 [TEMPLATE.md](TEMPLATE.md)。
编号三位递增、全局唯一,建文件时取 `origin/main` 索引里最大号加一。本仓库的编号从 **001** 重新开始;
旧仓 `agent-guard` 的 P-001 到 P-041 是那边的历史,这里不续。

什么样的改动要先写 proposal:动了规则、不变量、数据模型、采集面,或者任何"做到什么程度算完"不显然的事。
一处错别字、一条 CI、一行注释不用。规矩的英文摘要在 [../../CONTRIBUTING.md](../../CONTRIBUTING.md)。

## 目录即状态

文件在哪个子目录,就是什么状态;文件里**不写** `状态:` 行,两份真相必漂。状态变更是一次 `git mv`,文件名不变,
`git log --follow` 和 `git log --grep P-NNN` 照常。**draft 和 design 只在 `p/NNN-slug` 分支上**,`main` 上只有
`complete/` 和 `rejected/`;在做什么看 `git branch -r --list 'origin/p/*'` 和打开的 PR。

| 目录 | 含义 | 文件里有什么 |
|---|---|---|
| `draft/` | 提出了,人还没说值不值得设计(分支上) | 只有问题、来源、后果、初步方向 |
| `design/` | 人说值得设计。补判据、不做什么、不能说什么、工作项,把未决问题**一次问完**;人答完即接受,接着实现;直到开 PR 都留在这里(分支上) | 六节齐全 |
| `complete/` | 做完了,人说交付;「完成」一节填好,带证据行,随 PR 到 `main`。**发版记录也放这里**(`NNN-release-x.y.z.md`,不走 draft/design) | 六节 + 「完成」;发版记录是包含的 P、changelog、改了哪里、证据行 |
| `rejected/` | 做完或做到一半人说不要,或与另一份是同一件事;同样随 PR 到 `main` | 正文写否决理由,或并入哪份 |

"进行中"不是目录:`p/NNN-*` 分支存在就是进行中。四个目录各带一个 `.gitkeep`,空着也在。

## 提交与回溯

- 分支名 `p/NNN-slug`;每个提交信息末尾带 `(P-NNN)`。本仓库**不装 hook 强制**这两条,靠 review。
- 从代码到 proposal:`git blame` → 提交信息末尾 `(P-NNN)` → 本目录(四个子目录里找编号)。
- 从 proposal 到代码和版本:`git log --grep 'P-NNN'`;`git tag --contains <合入提交>`;
  文末「完成」一节直接记着合入的 PR 和发布版本。

## 索引

| P | 标题 | 来源 | PR / 发布 |
|---|---|---|---|
| [001](complete/001-judge-usage-in-json.md) | 判官的 token、triage 调用、重试数从不进报告,成本只能从 stderr 抄 | 判官用量只打到 stderr,`--quiet` 时(Downloads 每一项)哪里都没有;移植自旧仓 P-042 | PR 待合入;待发 |
| [002](complete/002-rules-version.md) | 报告不说自己是哪一版规则跑出来的,两份报告分不清是规则变了还是输入变了 | 新发现(2026-10-09):两次扫描得分不同时,报告说不出规则表变没变;移植自旧仓 P-043 | PR 待合入;待发 |
| [003](complete/003-zero-dial-test.md) | "绝不外连"没有一条测试钉住,baselines 的模板却说有 | 新发现(2026-10-09);移植自旧仓 agent-guard 的 P-044 | PR 待合入;待发 |
| [004](complete/004-check-llm.md) | 装前检查不能用判官,CI 用户只能走 scan --root 的绕路 | `check` 恒静态,装前检查用不上判官;`scan` 已对同样不可信的 Downloads 内容提供 `--llm`;移植自旧仓 P-047 | PR 待合入;待发 |
| [005](complete/005-judge-egress-paths.md) | BYO 判官把用户名、绝对路径和不带键名的 env 值发给模型厂商 | `--llm` 摘录里带家目录绝对路径和用户名,MCP env 值不带键名发出;移植自旧仓 P-045 | PR 待合入;待发 |
| [006](draft/006-judge-rendering.md) | 模型的整段 evidence 被渲染成证据,triage reason 能逃出 markdown | 判官 snippet 是模型的整段 evidence、triage reason 未脱敏能逃出代码跨度、模型文本无长度上限;移植自旧仓 P-046 | 分支上 |
