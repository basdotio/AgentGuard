<!-- SPDX-License-Identifier: MIT -->
# 021 — 报告里的判官数字靠一份没进仓的量测台改动跑出来,别人复现不了

- **来源**:新发现(2026-09-27,对话提出);P-019 的真机复测和 2026-09-25 的判官对照报告都用了它
- **依赖**:P-015(量测台)、P-019(判官 LLM-009 只提示,它的判据 3 就是靠这条透传跑的)
- **分支**:`p/021-bench-judge-passthrough`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

2026-09-25 的判官对照(glm-5.3-flash,819 子集)和 P-019 的 8 个样本复测,都是用 `baselines/` 的 driver 跑的,但 driver
和 aguard adapter 从设计上**不给子进程任何多余的东西**:四个环境变量、固定的 flag、几十秒超时,为的是操作者机器上的
配置漏不进样本(P-015)。要让它带着 `--llm` 跑,我在本地给 adapter 加了三个字段、给 driver 加了三个 flag,一直没提交。

后果有三条:

1. **数字不可复现。** 报告和 P-019 的完成节引用了判官实测,而跑出它们的代码只在一台机器的工作区里。P-015 立量测台的
   理由正是"一个没法说清谁、哪天、哪个版本跑的数不是测量",这批数正好违反它。
2. **下面还要用两次。** 判官进闸门前要跑 `samples: 3` 的方差和全量 3,220 良性(plan §3.1 第 5 步),每次从 stash 里翻出来
   再手打 flag,迟早和主干漂开。
3. **一个隐含的假话。** `tools.yaml` 给 aguard 写的 `uploads_samples: false` 依据是"判官,基线运行不开它"。带 `--llm` 跑的
   那次,脱敏摘录确实出了机器,而 `run.yaml` 照抄了那句"没出"。

## 初步方向

把本地那份改动进仓,再补它缺的那块披露:adapter 三个可选字段(`ExtraArgs` 只加到 `scan`、`ExtraEnv` 只加点名的变量、
`Timeout` 覆盖),driver 三个 flag(`-aguard-extra-args`、`-aguard-env`、`-aguard-timeout`),`run.yaml` 新增 `tool_extra_args`,
带 `--llm` 时 `uploads_samples` 翻成 true 并换一句为这次运行写的依据,scorecard 签名行也印出来。不带 flag 时逐字节不变。

## 完成的判据

- [ ] `TestJudgePassthroughIsExplicitAndScanOnly`(`baselines/adapter/aguard`):stub 二进制记录 argv 和环境;`scan` 收到附加参数、
      `check` 没收到;点名的变量到了子进程,当前进程的金丝雀变量两种情况下都没到;不设字段时 argv 和环境与原来一样。
- [ ] `TestPickPassesJudgeFlagsThroughOnlyWhenAsked`(`baselines/cmd/baseline`):三个 flag 原样到 adapter,裸 `NAME` 从当前进程取值;
      不传时三个字段全空。
- [ ] `TestAJudgeRunDisclosesTheUpload`:带 `--llm` 时 `uploads_samples: true` 且依据提到 `--llm`、脱敏、`raw/`、verdicts 不受影响;
      其他附加 flag 或没有 flag 时沿用登记表原话。
- [ ] 反向断言:不带任何新 flag 跑全量 3,539 个,`verdicts.jsonl` 与已提交的 `baselines/results/aguard/2026-09-24/verdicts.jsonl`
      逐字节相同(`cmp`)。
- [ ] `make verify` 绿。

## 不做什么

- ~~不把判官运行的结果放进 `baselines/results/`~~ **2026-09-28 推翻**(同事提议,领导拍板):判官结果进 `results/aguard/<日期>-llm-<模型>/`
  单独目录,按判官口径折 verdicts,`run.yaml` 顶部写明不是基线,`raw/` 作为唯一一手证据一并提交。第一份是 `2026-09-25-llm-glm-5.3-flash/`。
  当时的理由(判官数依赖模型、日期和采样)仍成立,答案改成"进,但标清楚",而不是"不进"。
- 不改 verdict 的折叠:仍只看确定性发现,`--llm` 运行的 verdicts 就是静态数。判官发现的折叠留给读 `raw/` 的人。
- 不给 cc-audit 或其他 adapter 加同样的口子;driver 对非 aguard 工具忽略这三个 flag,`run.yaml` 不记。
- 不动 `internal/`,不动语料。
- 不提供把整份环境继承给子进程的选项。

## 不能说什么

- 引用 `--llm` 运行的任何数字时必须带模型、`samples`、子集大小和日期,并和静态列分开;`run.yaml` 的 `tool_extra_args`
  和翻成 true 的 `uploads_samples` 就是为了让人一眼看出那次运行不是基线。
- 不说"基线运行不出机器":现在是"默认不出;传了 `--llm` 就出,而且 run.yaml 会写"。

## 工作项

| W | 一句话 | 提交信息 |
|---|---|---|
| 1 | 三个测试,HEAD 上编译不过 | `baselines: tests for a judge passthrough that is explicit, scan-only and disclosed in run.yaml — red until it exists (P-021)` |
| 2 | adapter 字段、driver flag、`uploadsFor`、`run.yaml` 字段与签名 | `baselines: the aguard adapter takes extra args, named env and a longer timeout, and a --llm run says so in run.yaml (P-021)` |
| 3 | README 一节、`tools.yaml` 依据改写、本文件、索引 | `baselines: how to measure the judge, and why nothing from it goes under results/ (P-021)` |

## 未决问题

1. **判官结果要不要也提交进 `baselines/results/`?** 建议不要,理由在「不做什么」第一条。**已决(2026-09-27)**:不提交,只提交跑法。
   **改决(2026-09-28)**:提交,见「不做什么」第一条的划线说明。
2. **要不要一并把 2026-09-25 那次 819 子集的跑法(配置、子集清单)进仓?** 建议不要:配置里有端点和模型名会过期,子集清单
   是从 `corpus samples` 分层抽的,README 给了抽法就够。**已决(2026-09-27)**:不进,老板说"不停下来问设计",按建议。

## 完成

```
合入:PR(2026-09-27;sha 合入后用 git log --grep P-021 找)
发布:v0.13.0
证据:TestJudgePassthroughIsExplicitAndScanOnly(baselines/adapter/aguard/passthrough_test.go)、
     TestPickPassesJudgeFlagsThroughOnlyWhenAsked、TestAJudgeRunDisclosesTheUpload(baselines/cmd/baseline/passthrough_test.go);
     W1 在 HEAD 上编译失败(unknown field agExtra / ExtraArgs undefined),W2 后绿;
     反向断言:不带新 flag 的全量运行 verdicts.jsonl 与 results/aguard/2026-09-24 逐字节相同(cmp 退出 0,2026-09-27,二进制 v0.12.0-2-gb3f3e2a,3,539 个全部 scored);
     不带 flag 时 argv 与环境不变 = TestJudgePassthroughIsExplicitAndScanOnly 的 plain 段
```
