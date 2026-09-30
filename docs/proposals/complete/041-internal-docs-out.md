<!-- SPDX-License-Identifier: MIT -->
# 041 — 两份内部证据文档、一份语料口径放错了目录、issues 里的 sha 和里程碑编号,在没有历史的新仓里都是死引用

- **来源**:开源前清理扫描(2026-09-30)决定二(过程文档移出)的**第一步**:先做不影响团队日常流程的部分
- **依赖**:无。第二步(proposals、process.md、两个 skill、两个 commit hook、planning 三份、对应测试与 Makefile 目标)要等所有清理 PR 合完、
  导出前最后做,因为团队现在还在用那套流程
- **分支**:`p/041-internal-docs-out`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

新仓不带 git 历史、不带 proposals 和 planning。有几样东西现在就能处理,而且处理了不妨碍任何人继续在本仓工作:

| 什么 | 问题 |
|---|---|
| `docs/decisions/audit-log.zh-CN.md`(749 行) | 内部取证日志:"四 agent 并行审计"、"主会话复现"、条目冻结规则。外部读者用不上,措辞是给自己人的 |
| `docs/decisions/competitor-research-2026-09.zh-CN.md` | 对四家竞品的直接评判。benchmark 数字另有 `benchmark-review` 和 `judge-comparison` 两份中性文档承载 |
| `docs/planning/corpus-benchmark.zh-CN.md` | 是语料口径和 `baselines/results/` 的读法,做 benchmark 的人要看,却放在要整体移出的 `planning/` 里 |
| `issues/007、009、014、016、017、020` 与 `issues/README.md` | 引用 commit sha(`10d8c32`、`efca8e3`、`aa322a6`、`4d654af`)、里程碑号(M2.4、M3.1、M3.2、M3.5)、工作项号(W-009)、`docs/planning/` 路径。新仓里 sha 不存在,编号无处可查 |
| `CLAUDE.md`、`docs/README.md`、`.claude/rules/conventions.md`、`Makefile:77`、`benchmark-review` | 指向上面这些的链接和句子 |

## 初步方向

删两份 decisions;corpus-benchmark 上移到 `docs/`,自己的相对链接跟着改;issues 去 sha 和编号,把"排入 M3.1"这类话改成说事实;
`issues/README` 的"与 work-items 的分工"改成"什么该进这里";五处引用改指新位置或改成陈述。**`docs/internals/` 两份实测记录保留**:
`clean-internals.md` 五处引用它们当证据,它们是对 Claude Code 加载行为的技术测量,不是流程材料;清理报告里"移出"的建议撤回。

## 完成的判据

- [ ] `git ls-files` 范围内、`docs/proposals`、`docs/planning`、`docs/process.md`、`.claude/skills` 之外,grep `planning/corpus-benchmark|audit-log|competitor-research` 为 0。
- [ ] issues 目录 grep 七位以上十六进制、`M[0-9]\.[0-9]`、`W-0[0-9]{2}` 只剩非引用的命中(`Z-c3765e73` 这类是 item id,`EXEC-001D-1a2b3c4d` 是示例)。
- [ ] 文档链接测试绿;`make verify` 绿。
- [ ] 反向断言:`docs/internals/measurement-*.md` 两份仍在,`clean-internals.md` 指向它们的五个链接不变;`docs/decisions/` 剩下的两份不改内容。

## 不做什么

- 不删 proposals、planning 的 direction/plan/work-items、process.md、skills、commit hook、对应测试、Makefile 的 hooks/verify:那是第二步,导出前做。
- 不动 issues 的结论和状态,只去引用。
- 不改 benchmark-review、judge-comparison 的数字和结论,只改指向已删文件的两处措辞和一处路径。
- 不动 `.go`。

## 不能说什么

- 不说"竞品调研没做过":做过,归档在旧仓;对外只发 benchmark 两份文档里按来源带 n 的数。

## 工作项

| W | 一句话 | 提交信息 |
|---|---|---|
| 1 | 删两份 decisions,上移 corpus-benchmark 并修其相对链接,改五处引用 | `docs: the audit log and competitor research leave; the corpus benchmark methodology moves up to docs/ (P-041)` |
| 2 | issues 去 sha、里程碑号、planning 路径;README 分工一节改写 | `issues: state the facts instead of citing shas and milestone ids that a fresh repository will not have (P-041)` |
| 3 | 本文件、索引 | `proposals: P-041 (P-041)` |

无红测试:纯文档。反向断言是链接测试和既有内容不变。

## 未决问题

1. **`docs/internals/measurement-*.md` 留不留?** 报告说移出,实际它们是技术测量而且被手册引用。建议留。**已决(2026-09-30)**:留,本条按此做。

## 完成

```
合入:PR(2026-09-30;sha 合入后用 git log --grep P-041 找)
发布:待发
证据:残留 grep 为 0(范围见判据一);issues 的 sha/M/W 引用清零,item id 与示例保留;文档链接测试与 make verify 绿;
     反向断言:internals 两份未动,clean-internals 五个链接未动,decisions 剩余两份仅三处措辞/路径改动
```
