<!-- SPDX-License-Identifier: MIT -->
# 022 — 基准折叠判官运行时,triage 调用数和每题用量是推出来的:aguard 的 JSON 已经报了真数,rig 一个都不读

- **来源**:P-001 记下的后续(`docs/proposals/complete/001-judge-usage-in-json.md`「问题」表第三行、「不做什么」"不进 baselines"),2026-10-09
- **依赖**:P-001(`JudgeSummary` 的 `triage_calls` / `retries` / `prompt_tokens` / `completion_tokens`)
- **分支**:`p/022-bench-fold-reads-judge-usage`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

基准 rig 每个样本读一次 `aguard … --json`(`baselines/adapter/aguard/aguard.go` 的 `Adapter.run`,解码成
`model.ScanResult`),然后只折叠一样东西:`fill` 按闸门谓词把确定性发现折成一个词。`res.Judge` 一个字段都不读。
判官运行的 `judge.jsonl` 是**手工**从 raw/ 折出来的(各判官 `run.yaml` 头一行就写着 "Written BY HAND"),
其中的用量字段按 `2026-09-29-llm-gpt-4.1-mini-s3/run.yaml` 的 `fold:` 推导:

- `triage_calls` = "每个有静态发现的 artifact 一次"
- `questions` = `(calls − triage) / 3`

P-001 之后,`--json` 的判官摘要里有 aguard 自己数的 `triage_calls`、`retries`,和端点报的 token 数。rig 不读它们,于是:

| 现象 | 后果 |
|---|---|
| **推导只在 `skipped == 0` 时成立**。`max_calls` 预算或 `total_timeout` 截掉计划尾部时,被截掉的往往正是 triage(它排在每个 artifact 计划的最后) | 临时探针(真实 `judge.Run`,两个各带一条静态发现的 skill,`samples: 3`,`max_calls: 13`):aguard 报 `calls 13 · skipped 1 · triage_calls 1`,推导给出 triage 2、questions `(13−2)/3` = 3 余 2;真数是 4 题。**每题用量错,而 rig 看不出来** |
| **retries 和 token 推不出来**,rig 也不记 | 判官运行的 `run.yaml` 里没有一处记着重试数和 token 数,而这正是 P-001 加这些字段的理由 |
| **raw/ 不是工具的原始输出**。`keepRaw` 把解码后的 `model.ScanResult` 重新 `json.Marshal` 再写盘(另外两个 adapter 写的都是工具自己的字节) | `triage_calls` / `retries` 在当前 model 里不带 `omitempty`:用今天的 rig 去测一个 P-001 之前的二进制,raw/ 里会凭空出现 `"triage_calls":0,"retries":0`。按"字段在不在"选口径的折叠,会把"没报"读成"报了 0",questions 变成 `calls / 3` |

已提交的四轮判官运行(`results/aguard/*-llm-*`)不受第一行影响:实测把文档里的推导套在它们的 raw/(取自旧仓 agent-guard
在 P-039 把 raw/ 移出树之前的那次提交)上,s3 两轮 224/224 行的 `triage_calls` / `questions` 与已提交的 `judge.jsonl` 逐行一致,
四轮 `skipped` 全是 0,也没有一个样本带新字段。**错的不是已经发的数,是下一轮**:下一轮用的二进制已经报了真数,
而 rig 照旧推;要是那一轮设了 `max_calls`,推出来的数就是错的。

## 初步方向

把用量折叠搬进 rig,并按"字段在不在"选口径:aguard 的 adapter 从**工具自己的字节**读判官摘要,带了 `triage_calls` 就用报的数
(calls、triage_calls、retries、两个 token 数),没带才退回文档里的推导;逐样本记进 `ledger.jsonl`,逐项加总进 `run.yaml`
的 `judge_usage:`,并写明这一轮用的是哪个口径。raw/ 改为写工具输出的原始字节(压缩空白、照旧替换 `<work>`)。
静态运行(没有判官摘要)的 ledger 和 run.yaml 一个字节不变。不重跑、不改写任何已提交的结果。
