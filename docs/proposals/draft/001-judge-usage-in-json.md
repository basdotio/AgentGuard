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
