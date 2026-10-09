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

## 完成的判据

- [x] `TestJudgeUsage_ReportedCountsWinOverTheDerivation`(`baselines/adapter/aguard/usage_test.go`,新):预算截断形状的样本 JSON
  (两个各带一条确定性发现的 skill,`calls 13 · skipped 1 · triage_calls 1 · retries 2`,两个 token 数)→ 折出
  `basis: reported`、`triage_calls 1`、`retries 2`、token 原样;测试先断言文档推导在同一份 fixture 上给出 2,
  保证这是一份**推导会错**的 fixture。今天没有这个折叠,编译即红
- [x] `TestScan_LedgerRowCarriesTheJudgeUsage`(同文件,新):桩二进制回答上面那份 JSON → `Scan` 返回的 ledger 行带
  `judge_usage`,是报的数;桩回答一份静态 JSON(无 `judge`)→ 行与今天逐字段相同
- [x] `TestRawKeepsTheToolsOwnBytes`(`baselines/adapter/aguard/raw_test.go`,新):桩二进制打印一份**不带** `triage_calls` / `retries`、
  带一个未知字段的判官摘要 → raw/ 里没有这两个键、未知字段还在。今天运行红:raw/ 是 `model.ScanResult` 的重编码,
  凭空出现 `"triage_calls":0,"retries":0`,未知字段丢失
- [x] `TestSumJudgeUsage_*`(`baselines/run/judgeusage_test.go`,新):逐样本加总进 `run.yaml` 的 `judge_usage:`,写明口径
  (`reported` / `derived` / `mixed`);不能逐样本都给出的合计(retries、token)不写,写缺了几个;0 次调用的样本不算"没报 token"
- [x] `TestWriteAllCarriesTheJudgeUsage`(`baselines/cmd/baseline/judgeusage_test.go`,新):判官运行的 `ledger.jsonl` 与 `run.yaml`
  带 `judge_usage`,静态运行的两份文件里没有这个词
- [x] 反向断言 `TestJudgeUsage_WithoutTheFieldsFoldsAsBefore`(usage_test.go,新):不带新字段的样本(按已提交 s3 `judge.jsonl`
  的三行造形)→ `basis: derived`,`triage_calls` 等于文档推导,retries 与 token **缺键**而不是 0
- [x] 反向断言(实测,不提交):新折叠的推导回退套在四轮已提交判官运行的 raw/ 上,s3 两轮 224/224 行的
  `triage_calls` / `questions` 与已提交 `judge.jsonl` 逐行一致,四轮 `judge_calls` 819/819、819/819、224/224、224/224 一致
- [x] 反向断言 `TestJudgeUsage_NoSummaryNoUsage`、`TestSumJudgeUsage_StaticRunHasNone`(新):没有判官摘要就没有用量块,
  `run.yaml` 里没有 `judge_usage` 键,签名那句不变;实测(不提交)对语料的一个小子集跑静态 driver,改动前后 `ledger.jsonl`、
  `verdicts.jsonl`、`scorecard.txt` 字节相同,`run.yaml` 只差 `started_at`
- [x] 反向断言:`TestVerdictFollowsTheGatePredicate`、`TestJudgePassthroughIsExplicitAndScanOnly`、`TestAJudgeRunDisclosesTheUpload`、
  `TestSignatureNamesWhatIsOurs`、`TestYAMLRoundTripKeepsTheAttribution`、`TestEveryRowPassesTheLedgersOwnCheck` 不改一字仍绿;
  `TestRawOutputNamesNoWorkDirectory` 只改一行(`keepRaw` 改收字节),断言不动
- [x] `make verify` 绿

## 不做什么

- **不动 `cmd/`、`internal/`**:二进制已经报了要的数;缺的只有 `samples`(见未决 3),那一条不做
- **不重跑、不改写 `baselines/results/` 下任何已提交文件**。按新代码,那四轮如果由 driver 写 `run.yaml`,会多出一个
  `judge_usage:` 块、口径 `derived`,数和已提交的一致(s3 两轮:calls 1769 · skipped 0 · triage_calls 104;
  samples:1 两轮:calls 1923 · triage_calls 241,只算带判官摘要的 692 个样本——走 `check` 的 127 个没有摘要,其中 55 个有静态发现,
  照字面套"每个有静态发现的 artifact 一次"会多算出 55 次从没发生的 triage);它们手写的 `run.yaml` 没有这个块,`fold:` 那段已经写着 triage 是推的。这里写清楚,不去改
- **不动 verdict 的折叠**:`fill`、`FlaggingRules`、`DimensionMap` 不改,`verdicts.jsonl` 和成绩单不受判官影响这一条照旧
- **不在 rig 里折叠判官的发现**:`judge.jsonl` 的 `static` / `judge` / `judge_any` / `escalated_rules` / `votes` 照旧手工从 raw/ 折
- **不算 `questions`、不估算金额**
- 不碰 ccaudit / cisco 两个 adapter(它们的 raw/ 本来就是工具自己的字节)

## 不能说什么

- 不说"每题用量现在是测出来的":`questions = (calls − triage_calls) / samples` 仍是公式,`skipped > 0` 时不精确;改的只是
  `triage_calls` 这个操作数从推的变成报的
- 不说已提交的四轮"用的是报告口径":它们是推导口径,推导在那四轮上**恰好**精确(`skipped` 全是 0,逐行核对过)
- 不说 token 是精确的:是端点自报,和 P-001 同一句;端点不报时 `judge_usage` 里就没有 token 合计
- 不说 raw/ 和以前字节相同:现在是工具自己的输出(压缩空白、`<work>` 替换照旧),字段集合随二进制版本走

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 五个新测试文件/用例 + 反向断言,跑红 | `baselines: tests — the judge fold ignores the counts aguard reports, and raw/ writes ones it never printed (P-022)` |
| 2 | raw/ 写工具自己的字节 | `baselines: raw/ keeps the bytes aguard printed instead of the rig's re-encoding of them (P-022)` |
| 3 | `ledger.JudgeUsage` + adapter 逐样本折叠:报了用报的,没报才推 | `baselines: each judged sample's ledger row carries what the judge cost, from the tool's own counts when it reports them (P-022)` |
| 4 | `run.JudgeUsage` + driver 把合计和口径写进 `run.yaml`;签名与上传说明那两句改成不再说"全在 raw/" | `baselines: run.yaml sums the judge's cost and says whether it was reported or derived (P-022)` |
| 5 | `baselines/README.md`「Measuring the judge」与布局表 | `docs: baselines README says where a judge run's cost comes from and when it is derived (P-022)` |
| 6 | 本文件、索引 | `proposals: P-022 (P-022)` |

## 未决问题

1. **逐样本的用量放哪:`ledger.jsonl` 的行上、单独一个文件,还是 driver 写 `judge.jsonl`?**
   **建议**:放 ledger 行上(`judge_usage`,`omitempty`)。ledger 本来就是一个样本一行、按样本排序,`Rules` / `Dimensions` 也是
   `Check` 不看的诊断字段;没有判官摘要时整块不出现,静态运行的 ledger 一字节不变。`judge.jsonl` 是手工折的,
   driver 写一个同名但字段不全的文件会和它撞。
   **已决(2026-10-09)**:按建议。
2. **口径按什么判?**
   **建议**:逐样本,看**工具自己的字节**里判官摘要有没有 `triage_calls` 这个键;有就是 `reported`,没有就是 `derived`。
   不按版本号判(版本串是给人看的),也不按值判(`0` 是合法的报数)。retries 与两个 token 数各自按键在不在,缺键就缺,不写 0。
   run 级:全 reported → `reported`,全 derived → `derived`,否则 `mixed`,两种各几个样本都写。
   **已决(2026-10-09)**:按建议。
3. **`questions` 要不要在 rig 里算?**
   **建议**:不算。判官摘要里没有 `samples`,rig 要么去解析 `--config` 指向的运维配置(rig 本来不读它),要么让运维再传一个
   必须和配置保持一致的参数——后者正是这条要消灭的那类"推一错全错"。`README` 写明公式和它在 `skipped > 0` 时不精确;
   给摘要加 `samples` 要动 `internal/`,记成后续,本条不做。
   **已决(2026-10-09)**:按建议。
4. **token / retries 的合计:有样本没报时怎么写?**
   **建议**:只有每个**发过调用**的样本都报了才写合计;否则不写,写 `tokens_unreported_samples: N`。0 次调用的样本不算没报
   (二进制对 0 token 本来就省略键)。对一半样本求和再印成"合计",正是这条要防的误读。retries 只在没有 `derived` 样本时写。
   **已决(2026-10-09)**:按建议。
5. **raw/ 写工具原始字节还是继续重编码?**
   **建议**:写原始字节,`json.Compact` 去掉缩进(和今天的 raw/ 一样是一行),`<work>` 替换照旧。重编码会给旧二进制的输出
   补上它没有的键、删掉新二进制多出来的键,"字段在不在"这个口径判据在 raw/ 上就失效了。另外两个 adapter 本来就写原始字节。
   **已决(2026-10-09)**:按建议。
6. **推导回退要不要加两条护栏:0 次调用时 triage 记 0、永不大于 calls?**
   **建议**:加。没发出去的 triage 不是一次调用;实测这两条在四轮已提交运行上一行都不改变。
   **已决(2026-10-09)**:按建议。
7. **driver 的两句说明——签名里的 "anything these flags added lives in raw/ and is not committed"、上传说明里的
   "the judge's output exists only in raw/"——要不要跟着改?**
   **建议**:改。签名只在有 `judge_usage` 时补半句"用量在 run.yaml 的 judge_usage 里",静态运行和没有用量的运行一个字不变;
   上传说明本来就只在 `--llm` 运行里出现,把"判官的输出只在 raw/"改成"判官的发现只在 raw/,用量在 judge_usage"。
   签名印在每份成绩单的最上面,不改它就会在每份判官成绩单上说一句不再全对的话。
   **已决(2026-10-09)**:按建议。

## 完成

```
合入:PR 待开(2026-10-09;sha 合入后用 git log --grep P-022 找)
发布:待发
证据:TestJudgeUsage_ReportedCountsWinOverTheDerivation、TestJudgeUsage_UnreportedTokensStayAbsent、TestScan_LedgerRowCarriesTheJudgeUsage(baselines/adapter/aguard/usage_test.go)、TestSumJudgeUsage_*(baselines/run/judgeusage_test.go)、TestWriteAllCarriesTheJudgeUsage(baselines/cmd/baseline/judgeusage_test.go);W1 编译红(usage_test.go:30:48: undefined: ledger.JudgeUsage;usage_test.go:36:9: undefined: judgeUsage;judgeusage_test.go:22:19: undefined: run.SumJudgeUsage;judgeusage_test.go:22:3: unknown field JudgeUsage in struct literal of type run.Run),W3 / W4 后绿
证据:TestRawKeepsTheToolsOwnBytes(baselines/adapter/aguard/raw_test.go);W1 时把 usage_test.go 暂移走单跑,运行红:raw/ carries "triage_calls", which the tool never printed —— raw 文件里是 "judge":{…,"triage_calls":0,"retries":0},另有 raw/ dropped a field the tool printed;W2 后绿
证据:临时探针(真实 judge.Run,两个各带一条静态发现的 skill,samples 3,不提交):不设预算 calls 14 · triage 2;max_calls 13 → calls 13 · skipped 1 · 报 triage 1、推 2,questions 报 (13−1)/3 = 4、推 (13−2)/3 = 3 余 2;max_calls 12 → 报 1、推 2
证据:端到端(真二进制 + 本仓 driver + 本机回环假端点,不连模型、不出网;20 个样本 = s3 samples.jsonl 前 10 恶意 + 前 10 良性,samples 3):不设预算 → run.yaml judge_usage basis reported · calls 108 · skipped 0 · triage_calls 9(推导也是 9)· retries 0 · tokens 10800 / 756(= 端点每次报的 100 / 7 × 108);max_calls 6 → calls 105 · skipped 3 · triage_calls 6,推导 9;逐样本 3 个(两个 hook、一个 permission)报 0 推 1,questions 报 2 推 1.67;ledger 每行的 triage_calls / retries 与该样本 raw 里二进制打印的值相同
证据:反向断言 TestJudgeUsage_WithoutTheFieldsFoldsAsBefore(usage_test.go,五例,三例按已提交 s3 行造形);实测(临时测试,不提交)新折叠套在四轮已提交判官运行的 raw/(旧仓 agent-guard P-039 之前那次提交里的 raw/)上:s3 两轮 judge_calls / triage_calls / questions 各 224/224 一致,口径全是 derived,合计 calls 1769 · triage_calls 104;samples:1 两轮 judge_calls 819/819 一致,692 个带摘要、127 个走 check 的无摘要不出用量,合计 calls 1923 · triage_calls 241;两条护栏(0 次调用记 0、不大于 calls)在四轮上改变 0 个样本
证据:反向断言(静态)TestJudgeUsage_NoSummaryNoUsage、TestSumJudgeUsage_StaticRunHasNone;实测同一个二进制、同 20 个样本跑静态 driver,origin/main 源码对本分支:ledger.jsonl、verdicts.jsonl 字节相同,scorecard.txt 与 run.yaml 只差开始时间那一行,两边都没有 judge_usage;raw/ 解码后只差 scanned_at(两次扫描)
证据:不改一字仍绿 —— TestVerdictFollowsTheGatePredicate、TestJudgePassthroughIsExplicitAndScanOnly、TestAJudgeRunDisclosesTheUpload、TestSignatureNamesWhatIsOurs、TestOurOwnRunIsNotProvisional、TestYAMLRoundTripKeepsTheAttribution、TestEveryRowPassesTheLedgersOwnCheck、TestPlaceableArtifactIsScored;TestRawOutputNamesNoWorkDirectory 只把 keepRaw 的参数换成 json.Marshal(res) 的字节,断言不动
证据:不做什么 —— git diff --stat origin/main -- cmd internal baselines/results baselines/adapter/ccaudit baselines/adapter/cisco baselines/adapter/sarif go.mod go.sum plugin 为空;fill / FlaggingRules / DimensionMap 不在 diff 里
证据:make verify: all gates passed(golangci-lint 0 issues);go version go1.23.5(无工具链切换),go.mod 的 go 指令仍是 go 1.23.5,无新依赖
```
