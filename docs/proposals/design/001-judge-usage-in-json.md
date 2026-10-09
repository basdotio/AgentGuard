<!-- SPDX-License-Identifier: MIT -->
# 001 — 判官的 token、triage 调用、重试数从不进报告,成本只能从 stderr 抄

- **来源**:新发现(2026-10-09)—— 判官每次运行的 token 用量、triage 调用数和重试数只打到 stderr,`--quiet` 时
  (Downloads 每一项都是)哪里都没有;JSON 报告里没有就无法事后核对一次运行用了多少。移植自旧仓 agent-guard 的 P-042(私有仓)
- **依赖**:无
- **分支**:`p/001-judge-usage-in-json`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

`--llm` 的用量只在一个地方出现:`runJudge` 结束时往 stderr 打的一行
(`cmd/aguard/main.go:293-300`,"LLM judge: N call(s) … · R retry · F failed · P tokens in / C out")。
它有三个缺口:

| 缺口 | 后果 |
|---|---|
| **token 数不进 `--json`**。`model.JudgeSummary`(`internal/model/model.go:547-556`)只有 `ran/reason/artifacts/calls/failed/skipped/findings/endpoint`,没有 token 字段 | 本仓库 `baselines/results/aguard/` 下提交的四轮判官运行(`*-llm-*`),`run.yaml` 和 `judge.jsonl` 里没有一处记着 token 数(`grep -i token` 命中的只有一条模型说明和几个样本名)。baseline 驱动(`baselines/adapter/aguard`)读的是 `--json`,stderr 只在出错时拼进错误信息,成功运行的那行到不了 raw/ |
| **`--quiet` 时连 stderr 那行也没有**(`if !quiet && stats.Calls > 0`)。Downloads 那一路每个下载项都以 `quiet: true` 跑(`main.go:581`) | 下载项的判官用量从不出现在任何地方,哪怕是终端 |
| **triage 调用和判官调用混在一起**。`Stats.Calls` 一个数,`calls` 一个数;`samples: 3` 时判官题乘 3、triage 不乘 | 折叠 judge.jsonl 时,`triage_calls` 是按"每个有静态发现的 artifact 一次"**推出来的**,`questions = (calls − triage) / 3` 也是推的(见 `baselines/results/aguard/2026-09-29-llm-gpt-4.1-mini-s3/run.yaml` 的 `fold:`)。推导一错,每题用量就错 |

重试数同样只在 stderr。

一句话:**一个默认关、要花用户自己 API 额度的功能,报告里记了它跑没跑、跑了几次,唯独没记它用了多少。**
`docs/llm-judge.md` 自己写着"cost is the one thing about `--llm` the report itself can't show you"——那是对现状的描述,不是设计。

## 初步方向

`JudgeSummary` 加 `prompt_tokens`、`completion_tokens`、`triage_calls`、`retries`(不开 `--llm` 时整块仍然不存在)。
`judge.Stats` 在合并循环里按任务类型分计 triage;`runJudge` 不论 `quiet` 都从 `*HTTPClient.Usage()` 填字段,stderr 那行原样保留;
Downloads 的逐项汇总跟着加。人类可读报告不加任何一行。`judge.Client` 接口不动(三个测试替身实现它)。

## 完成的判据

- [ ] `TestRun_StatsCountTriageApart`(`internal/judge/consensus_test.go`,新):`samples: 3`、一个带静态发现的 skill →
  `Stats.TriageCalls == 1`,`Stats.Calls == 3 × 题数 + 1`。今天没有这个字段,编译即红
- [ ] `TestE2E_JudgeSummaryCarriesCost`(`cmd/aguard/e2e_test.go`,新):假端点在每个响应里带 `usage`,`scanEnv(…, llm: true, quiet: true)` →
  `Judge.PromptTokens` 与 `CompletionTokens` 等于端点报的总和,`TriageCalls` 等于有静态发现的 artifact 数,`Retries == 0`。
  **`quiet: true` 是故意的**:今天 quiet 时连 stderr 那行也没有,这条判据钉住"quiet 不影响记账"
- [ ] `TestScanInbox_JudgeCostAddsUp`(`cmd/aguard/main_test.go`,新):两个 Downloads 候选、`--llm` → `InboxReport.Judge` 的四个新字段等于逐项之和
- [ ] 反向断言 `TestE2E_UnreportedUsageIsNotAZero`(`cmd/aguard/e2e_test.go`,新):端点**不报** `usage` 时(现有 `judgeServer`),
  JSON 里没有 `prompt_tokens` / `completion_tokens` 两个键——"没报"不写成 0(见未决 1);`retries` / `triage_calls` 两个键在
- [ ] 反向断言:不带 `--llm` 时 `Judge == nil`(`TestScanEnv_JudgeRequestedButNotEnabled`,`cmd/aguard/main_test.go:925`)不改一字仍绿;
  `TestText_JudgeLineStates`(`internal/report/text_test.go:515`)、`TestHTML_JudgeSectionPlaceholder`(`internal/report/html_test.go:124`)
  不改一字仍绿——人类可读报告一个字节不变
- [ ] 反向断言:`TestConsensus_TriageIsNotSampled`、`TestConsensus_CostsWhatItSays`、`TestRun_RetriesOnlyRetryableErrors`、
  `TestHTTPClient_CountsTokens` 不改一字仍绿
- [ ] `make verify` 绿

## 不做什么

- **人类可读报告不加任何一行**:text / markdown / html / sarif 的判官那句(`report.judgeLine`)不动。用量给机器读,报告给人读
- **stderr 那行不动**:内容、格式、`--quiet` 时不打印,都和今天一样
- **`judge.Client` 接口不动**:`Usage()` 只在 `*HTTPClient` 上,`runJudge` 本来就持有具体类型;接口上加方法会碎掉三个测试替身
  (`scriptedClient`、`fakeClient`、`countingTriageClient`)
- **不进 baselines**:adapter 把 `model.ScanResult` 原样写进 raw/,新字段自动带进去;把它们折进 judge.jsonl 是另一件事,本条不做
- **不估算金额**:单价因厂商、档位、时间而变,报告里只有 token
- 不改 `max_calls` 的语义,不加任何新的网络调用

## 不能说什么

- 不说"成本现在可以精确计算":token 是**端点自报**的,`internal/judge/openai.go:68` 的注释原话是 "a cost baseline, not an accounting guarantee"。
  不报 usage 的端点在 JSON 里就是缺这两个键
- 不说"题数现在是直接记录的":`(calls − triage_calls) / samples` 在没有 skipped 的运行里精确,有 skipped 时只是上界;本条记的是调用,不是题
- 不说 Downloads 的用量"以前就在":以前 quiet 路径哪里都没有它

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 三条新测试 + 一条反向断言,跑红 | `judge, cmd: tests — the JSON judge summary has no tokens, triage calls or retries, and quiet runs record nothing (P-001)` |
| 2 | `judge.Stats` 在合并循环里按任务类型分计 `TriageCalls` | `judge: Stats counts triage calls apart from judge calls (P-001)` |
| 3 | `JudgeSummary` 加 `prompt_tokens` / `completion_tokens`(缺省 = 端点没报)、`triage_calls`、`retries`;`runJudge` 不论 quiet 都填 | `model, cmd: the JSON judge summary carries tokens, triage calls and retries, quiet or not (P-001)` |
| 4 | Downloads 段逐项加总四个新字段 | `cmd: the Downloads judge summary adds up every item's cost (P-001)` |
| 5 | spec §8 `Judge` 注释;`docs/llm-judge.md` 与 zh 对子的"成本是报告看不到的唯一东西"和 JSON 字段表;`main.go` 那行注释 | `docs: spec §8 and the llm-judge pair say the judge's cost is in the JSON summary (P-001)` |
| 6 | 本文件、索引 | `proposals: P-001 (P-001)` |

## 未决问题

1. **端点不报 `usage` 时,token 两个字段写 0 还是不写?**
   **建议**:不写(`omitempty`)。一次真实调用的 prompt token 不可能是 0,所以"缺这个键"就等于"端点没报",比一个看起来像测量值的 0 诚实。
   `retries` 和 `triage_calls` 照 `calls` / `failed` / `skipped` 的样子**总是出现**:它们是我们自己数的,0 就是 0。
   **已决(2026-10-08)**:按建议。
2. **要不要顺手把 p50 / p95 延迟放进 JSON?**
   **建议**:不。延迟取决于网络和机器,放进 JSON 让同一输入两次运行的报告差得更多;stderr 那行继续有它。
   **已决(2026-10-08)**:按建议。
3. **字段名用 `prompt_tokens` / `completion_tokens`(OpenAI 的叫法),还是 `tokens_in` / `tokens_out`(stderr 那行的叫法)?**
   **建议**:`prompt_tokens` / `completion_tokens`。和端点返回的键同名,读 raw/ 的人不用换算;stderr 是给人看的,两边各用各的习惯。
   **已决(2026-10-08)**:按建议。
