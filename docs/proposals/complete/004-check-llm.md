<!-- SPDX-License-Identifier: MIT -->
# 004 — 装前检查不能用判官,CI 用户只能走 scan --root 的绕路

- **来源**:`check` 按规格恒静态,装前检查一个下载来的 skill 时用不上判官;`--llm` 本来就是显式开关,`scan` 已经对同样不可信的
  Downloads 内容提供它。移植自旧仓 agent-guard 的 P-047(私有仓)
- **依赖**:无
- **分支**:`p/004-check-llm`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

`aguard check <path>` 是在 PR 上审一个 skill / 插件 / zip 的那条命令(默认 `--fail-on high`)。它没有 `--llm`:
在 `main`(`dec64ca`,v0.18.0)上 `aguard check ./x --llm` 是 cobra 的 `error: unknown flag: --llm`,退出码 2;`--fail-on-llm` 同样不存在
(`error: unknown flag: --fail-on-llm`,退出码 2)。`cmd/aguard/main.go:683` 写着 "`check` is static-only by contract (spec §3)",
`main.go:656` 的帮助串是 "(static only)";规格 §3 的原话是"`check`(装前不可信内容)**强制静态**,不接受 `--llm`"。

想让判官看一眼单个目标的人,今天只有一条绕路:

| 绕路 | 后果 |
|---|---|
| **`scan --root <目标>`** | `scan` 的根是"运维自己的环境",所以**自动读 `<root>/.aguardignore`**(`resolveIgnorePath`,`autoBaseline: true`)。目标自带一份列着自己规则 ID 的基线,就能压掉自己的发现、让 `--fail-on` 不响——这正是 `check` 故意拒绝的形状(`TestCheckTarget_TargetSuppliedBaselineIgnored`)。为了用判官,CI 用户得重新打开一个已经关上的洞 |
| **搭一个假的 `.claude` 树,把目标放进 `skills/<name>/`** | baselines 的 adapter 就是这么做的(`baselines/adapter/aguard/stage.go` 的 `Stage`)。分数变成环境聚合分而不是这个 artifact 的分;还要记得 `--inbox off`,否则 CI 机器的 `~/Downloads` 也被扫进来;布局不对的目标(裸 MCP 源码、单个文件)根本放不进去 |

而"装前不可信内容"这个理由,在 `scan` 那边已经不成立了:`scan --llm` 对 `~/Downloads` 里**还没装**的候选逐个走 `checkTarget`,
判官照跑(`cmd/aguard/inbox.go` 的 `checkCandidate`)。同一类内容、同一条代码路径,从 `scan` 进来能判,从 `check` 进来不能。

还有一处规格自己不一致:§3 命令行那行写"强制静态,**忽略** `--llm`",下面一条写"**不接受** `--llm`"。实际行为是后者。

一句话:**最需要判官第二意见的那条命令——审一个别人写的、还没装的东西——恰恰是唯一拿不到它的;而拿到它的唯一办法比不拿更危险。**

## 初步方向

`check` 注册 `--llm` 和 `--fail-on-llm`,语义与 `scan` 完全相同:`--llm` 是同一个显式开关、发同一份脱敏摘录;
`--fail-on-llm` 同样要 `llm.authority: escalate`,没授权是报错拒绝;`--fail-on` 照旧只看确定性发现。
加载时闸门(`aguard hook`)和 `aguard approve` 构造的 `scanOpts` 永不设 `llm`,加一条测试钉住。
规格 §3 / §5.2 / §16 和几处"`check` 恒静态"的文档跟着改。baselines 的 adapter 继续只给 `scan` 传 `--llm`,只改理由。

## 完成的判据

- [x] `TestCheckCmd_LLMRunsTheJudge`(`cmd/aguard/check_llm_test.go`,新,走二进制):假端点 + `llm.enabled: true` 的 config,
  `check <skill 目录> --llm --config … --json` → 退出 0,`judge.ran == true`,有一条 `LLM-003`,
  `overall` 与同一目录 `check` 不带 `--llm` 的 `overall` 相等,`overall_effective < overall`;**同一个 skill 打成 `.zip`** 再跑一遍,
  同样 `judge.ran == true`。不带 `--quiet` 时 stderr 有 `LLM judge:` 那行,带 `--quiet` 时没有。今天:退出 2,`unknown flag: --llm`
- [x] `TestCheckCmd_FailOnLLMNeedsAuthority`(同文件,新,走二进制):`authority: escalate` 下 `--llm --fail-on-llm high` → 退出 1;
  `authority: advisory` 下同样的命令 → 退出 2,stderr 点名 `llm.authority: escalate`(拒绝,不是忽略)。今天两条都退出 2,报的是 `unknown flag: --llm`(cobra 报第一个不认识的 flag;单独 `check <x> --fail-on-llm high` 报 `unknown flag: --fail-on-llm`)。
  这一条只在 fixture **没有**确定性 high 时成立 —— 有 high 时默认的 `--fail-on high` 先返回 1,拒绝根本不出现;拒绝出现时也已经在
  判官发完请求之后。由下一条(W7)补上
- [x] `TestFailGateFlags_RefusedBeforeTheJudge`(同文件,新,走二进制;W7):fixture **带一条确定性 high**,端点计数。
  `check` 与 `scan` 上,缺授权的 `--fail-on-llm high`(带与不带 `--llm`)、`--fail-on-llm hgih`、`--fail-on hgih` → 一律退出 2、
  stderr 点名原因、端点 **0** 个请求、stdout 为空(没扫、没印报告)。反向:`authority: escalate` + `--llm --fail-on-llm high`
  → 退出 1、判官照跑。`TestFailGate_DeterministicHitDoesNotMaskARefusal`(同文件,进程内):`failGate` 在 `--fail-on` 已命中时
  照样拒绝兑现不了的 `--fail-on-llm`,不返回退出 1
- [x] 反向断言:`--fail-on` 在 `check --llm` 下仍只看确定性发现 —— 同一个 fixture、`authority: escalate`、`--llm` 加默认的
  `--fail-on high` → 退出 0(判官那条 high 够得着 `--fail-on-llm`,够不着 `--fail-on`)。`TestFailGate`、`TestFailGate_*` 不改一字仍绿
- [x] 反向断言:`TestGateScannerNeverEnablesLLM`(`cmd/aguard/check_llm_test.go`,新,进程内):一份**判官就绪**、`authority: escalate`
  的 config 指向一个计数端点;`gateOptions(…).Scan`、`.ScanRoot`、`approvePath`、`runHook`(`PreToolUse[Skill]` 和 `SessionStart`)
  各跑一遍 → 端点收到 **0** 个请求,`Scan` / `ScanRoot` 的结果 `Judge == nil`。改动前后都绿:它钉的是"闸门不跟着 `check` 开判官"。
  W8 收紧:每条 hook 回复还要**读起来是一次审计** —— `PreToolUse` 不含 `GATE-000`、点名 `test-runner` 并带 `NN/100`;
  `SessionStart` 以 `AgentGuard audited … at session start` 开头。只断言回复非空不够:一条什么都没扫的 `GATE-000` 也非空、也是 0 个请求
- [x] 反向断言:`TestCheckCmd_ConfigAloneNeverCallsTheJudge`(同文件,新,走二进制):判官就绪的 config、**不带** `--llm` →
  端点 0 个请求,JSON 里没有 `judge` 键,stderr 为空。改动前后都绿
- [x] 反向断言:`check` 不带 `--llm` 的输出逐字节不变 —— `origin/main` 和本分支用同一组 `-ldflags` 各编一个二进制,
  对恶意 skill / 良性 skill / 单文件 / zip 四个目标跑文本、`--json`(去掉 `scanned_at`)、`--md -`、`--sarif` 等,`diff` 为空,退出码相同。
  `TestGateAgreesWithCheck`、`TestCheckTarget_*`、`TestMarkdownFlag_*` 不改一字仍绿。**范围是合法阈值**:W7 之后,阈值打错时
  `check`(和 `scan`)在采集前退出 2、不印报告,`origin/main` 是先印完整报告再退出 2 —— 退出码相同,stdout 不同(见「不能说什么」)
- [x] 反向断言:`baselines/adapter/aguard/passthrough_test.go` 一字不改仍绿 —— adapter 仍然只给 `scan` 传 `--llm`
- [x] `make verify` 绿;`go version` 不切工具链;`go.mod` 第二行仍是 `go 1.23.5`,无新依赖

## 不做什么

- **`--fail-on` 的语义不动**:只看确定性发现,`check` 默认仍是 `high`;任何模型输出都改不了它的答案(不变量 #4)
- **加载时闸门、`aguard approve`、`clean` 永不设 `llm`**,也不加任何能让它们开判官的配置项。闸门有 30s deadline、fail-open、
  每次加载都会触发,答案取决于端点有没有回话的提示不是闸门
- **不改任何默认**:`--llm` 在 `check` 上默认关;`llm.enabled` 默认 `false`;`--fail-on-llm` 默认空
- **baselines 的行为不动**:adapter 仍只给 `scan` 传 `--llm`(`passthrough_test.go` 一字不改),已提交的结果不重跑。
  只改它的理由(README 一行、adapter 两处注释、驱动 flag 的帮助串)
- **`plugin/` 不动**:`/aguard-vet` 要不要提供"深度检查"这一步是插件对话流程的决定,另起
- **`README.md` / `README.zh-CN.md` 不动**(见未决 3)
- **`.claude/rules/invariants.md` 不动**:不变量 #1 由并行的 P-003 改写成列举式;两条都合入后,那张列表里要有 `check --llm`(写在 PR 里)。
  **后合入的那一条负责**,一共四处:不变量 #1 的出网路径列表、规格 §16.4 的"出网的路径只有两条"、`baselines/tools.yaml` 的
  `uploads_samples_basis`、`TestZeroDial_OnlyTheJudgeConnects` 的正对照加一行 `check --llm`(它在零表里的那行 `check` 不带 `--llm`,照旧该是 0)
- **不改判官出网时擦除 home 的逻辑**(并行的 P-005):`checkTarget` 不设 `scanOpts.home`;P-005 在它的分支上让判官**总是**擦掉
  OS 用户的 home(`os.UserHomeDir()`),外加一个绝对路径的扫描 home,所以两条都合入后 `check --llm` 发出去的内容同样被擦。
  后合入的那一条加一条 `check --llm`(目录和 `.zip` 各一次)的出网断言,让断言自己说覆盖了没有(写在 PR 里)
- 不动 `internal/judge`、`internal/report`、`internal/model` 的代码,不动 `go.mod` / `go.sum`
- 不给 `check` 加 Downloads 那样的逐项汇总:一个目标就是一个 `ScanResult.Judge`

## 不能说什么

- 不说"CI 里的 `check` 现在会用判官":它是显式 opt-in,不传 `--llm` 时阈值合法的输出一个字节都不变
- 不说"不带 `--llm` 的 `check` / `scan` 在任何参数下都逐字节不变":W7 之后,阈值打错或 `--fail-on-llm` 缺授权时它们在采集前就
  退出 2,stdout 为空;以前是先扫完、印完报告再退出 2(退出码相同)。config 也提前读了:config 和 `--root` / 目标同时有错时,
  先报的是 config 那条
- 不说"`--fail-on-llm` 缺授权一直都会被拒绝":W7 之前(已发布的 `scan` 也一样),只要确定性发现先够到 `--fail-on`
  (`check` 默认就是 `high`),运行退出 1,拒绝根本不出现;出现时判官的请求也已经发出、报告已经印过
- 不说"判官现在能让 `check` 失败":只有 `--fail-on-llm`,且要 `llm.authority: escalate`;`--fail-on` 永远看不见它
- 不说"闸门现在也有判官"或"闸门和 `check --llm` 给同一个答案":闸门对应的是不带 `--llm` 的 `check`
  (两者的确定性那一半本来就相同,判官只动 `overall_effective`)
- 不说"走 `check` 的 benchmark 样本现在也被判了":adapter 没改,放不进假 home、交给 `check` 的那部分裸树照旧是静态的
- 不说"把不可信内容送给判官是安全的":脱敏是尽力而为;非本地端点照旧出 `LLM-002`;CI 里审别人的 PR,`--llm` 就意味着那份内容的脱敏摘录离开机器

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 四条新测试,跑红(两条二进制测试因 `unknown flag` 红;两条反向断言改动前就绿) | `cmd: tests — check has no --llm or --fail-on-llm, so a single target cannot reach the judge (P-004)` |
| 2 | `check` 注册 `--llm` / `--fail-on-llm`,传 `quiet`,按 `cfg.LLM.MayEscalate()` 走 `failGate`;`gate.go` 注释改理由 | `cmd: check takes --llm and --fail-on-llm with scan's semantics, and the load-time gate still never asks (P-004)` |
| 3 | 规格 §3 / §5.1 / §5.2 / §11 / §16.4 / §17 | `spec: check runs the judge only when asked with --llm, and the load-time gate never does (P-004)` |
| 4 | `docs/llm-judge` / `docs/architecture` / `docs/install-gate` 三个对子、`config.example.yaml`(`ROADMAP.md` 不用动:本仓库的 ROADMAP 没有"`check` 从不调判官"那句) | `docs: the llm-judge, architecture and install-gate pairs stop saying check is always static (P-004)` |
| 5 | `internal/judge/judge.go` 与 `internal/config/config.go` 的包注释、`.claude/rules/judge.md`、`.claude/rules/gate.md` | `judge, config, rules: package docs and the judge and gate rules name check --llm and keep the gate static (P-004)` |
| 6 | `baselines/README.md`、adapter 两处注释、`-aguard-extra-args` 的帮助串 | `baselines: the adapter still passes --llm to scan only, now for the reason that is still true (P-004)` |
| 7 | `check` / `scan` 的 RunE 在采集**之前**用 `validateFailGates` 校验两个阈值和授权;`failGate` 先校验再判;规格 §3 与 `docs/llm-judge` 对子各一句。测试与修复同一提交,红在「完成」里 | `cmd: a misspelt --fail-on-llm or a missing llm.authority is refused before anything is scanned or sent, and a deterministic hit no longer hides it behind exit 1 (P-004)` |
| 8 | `TestGateScannerNeverEnablesLLM` 读每条 hook 回复的内容(`assertGateAudited`),不只看长度 | `cmd: tests — the gate-never-asks test reads each hook reply as an audit, so a GATE-000 that scanned nothing can no longer pass it with zero requests (P-004)` |
| 9 | 本文件、索引 | `proposals: P-004 (P-004)` |

## 未决问题

1. **`check --fail-on-llm` 不带 `--llm` 时怎么办?**
   **建议**:和 `scan` 一模一样,同一个 `failGate` 调用:没授权就拒绝;授权了,没有 LLM 发现时它退化成同级的确定性闸门。
   不另加一条"`--fail-on-llm` 需要 `--llm`"的报错 —— 那会让 `check` 和 `scan` 的同名 flag 语义分叉,而方向写的是"语义与 scan 相同"。
   **已决(2026-10-08)**:按建议。
2. **`check` 的 Short 帮助串 "(static only)" 改成什么?**
   **建议**:`Pre-install gate: scan a single skill/dir/file (static; --llm adds the judge)`。帮助串不是报告输出,不在"逐字节不变"的范围里。
   **已决(2026-10-08)**:按建议。
3. **README 对子要不要加一行 `check --llm`?**
   **建议**:不加。README 的判官一节没有说过 `check` 是静态的,"off by default (needs both config and `--llm`)"在本条之后仍然成立;
   `README.md:174` 的 "statically scan one skill/dir/file" 说的是不带 `--llm` 的默认;完整参考是 `docs/llm-judge.md`,在那里写。
   README 两份一起动,换来的是一行重复。
   **已决(2026-10-08)**:按建议。
4. **`TestGateScannerNeverEnablesLLM` 要不要先重构,让闸门的 `scanOpts` 能被直接检查?**
   **建议**:不重构。计数端点加 `Judge == nil` 是**行为**上的观察:将来哪怕有人换一种方式在闸门里打开判官(比如读 config 的 `JudgeReady()`),
   它也会红;检查一个结构体字段只能抓到"把 `llm: true` 写进字面量"这一种。
   **已决(2026-10-08)**:按建议。
5. **baselines 要不要顺手让走 `check` 的样本也带上 `--llm`?**
   **建议**:不。已提交的每一轮判官运行都是在 `scan` 那条路上量的,走 `check` 的样本是静态的;现在改,下一次重跑量的就不是同一件事,
   而且会悄悄改变一列已经发出去的数。要量,是另一份 proposal 的测量决定。
   **已决(2026-10-08)**:按建议。

## 完成

红的证据全部在本仓库测得:W1 的测试打在 `main` 的 `dec64ca` 上;W7 是"测试 + 修法"一个提交,红是把该提交的 `cmd/aguard/main.go`
换回 W6 的版本、测试不动,再跑同一组测试得到的(换回后立刻还原,工作区干净)。

```
合入:PR 待开(2026-10-09;sha 合入后用 git log --grep P-004 找)
发布:待发
证据:TestCheckCmd_LLMRunsTheJudge(cmd/aguard/check_llm_test.go);W1 红(exit 2,error: unknown flag: --llm)→ W2 后绿:目录与 zip 各自 judge.ran = true、4 次调用、有 LLM-003,overall 83 = 静态 check 的 83,overall_effective 58;--quiet 时 stderr 无 "LLM judge"
证据:TestCheckCmd_FailOnLLMNeedsAuthority(同文件);W1 红(三条都是 exit 2,unknown flag: --llm)→ W2 后绿:escalate + --fail-on-llm high → exit 1;advisory + --fail-on-llm high → exit 2,stderr 点名 llm.authority: escalate;反向断言 --llm + 默认 --fail-on high → exit 0(判官的 high 够不着 --fail-on)
证据:反向断言 TestGateScannerNeverEnablesLLM(同文件);W1 时(改动前)与改动后都绿,计数端点 0 个请求、Scan / ScanRoot 的 Judge == nil。变异验证:gate.go 里 gateOptions 的 Scan 加 llm: true → 红(Judge.Ran = true、Calls 3,端点 6 次请求);ScanRoot 加 llm: true → 红(端点 6 次);approvePath 加 llm: true → 红(端点 3 次);三处都已还原
证据:反向断言 TestCheckCmd_ConfigAloneNeverCallsTheJudge(同文件);改动前后都绿:判官就绪的 config、不带 --llm → 0 个请求、JSON 无 judge 键、stderr 为空
证据:W7 TestFailGateFlags_RefusedBeforeTheJudge(同文件);main.go 换回 W6 时红 6/8 行:check 缺授权(--llm)exit 1 且 stderr 有 "LLM judge: 4 call(s)"、check 缺授权(无 --llm)exit 1、check --fail-on-llm hgih exit 1 且 4 次调用、check --fail-on hgih exit 2 但端点 4 次请求且 stdout 有完整 JSON、scan 缺授权 exit 1 且 4 次调用、scan --fail-on-llm hgih exit 2 但端点 4 次请求且 stdout 有 JSON → W7 后 8/8 绿:6 行拒绝全是 exit 2、0 次请求、stdout 为空;反向两行(escalate + --llm --fail-on-llm high,check 与 scan)前后都是 exit 1 且判官有请求
证据:W7 TestFailGate_DeterministicHitDoesNotMaskARefusal(同文件);main.go 换回 W6 时红 2/2(typo 与 no authority 都返回 failExit,即 exit 1)→ W7 后都返回普通错误(exit 2)。TestFailGate、TestFailGate_*(6 条)不改一字仍绿
证据:W8 TestGateScannerNeverEnablesLLM 收紧后在正确代码上绿。弱点演示(临时测试,未提交,未改产品代码):PreToolUse 一个没装的 skill、SessionStart 配一份解析不了的 config,两条回复都是 GATE-000、都非空、端点 0 次请求 —— 旧断言 reply.Len() > 0 两条都过;assertGateAudited 两条都红("the gate did not audit, so a zero request count proves nothing")
证据:反向断言 check 不带 --llm 逐字节不变 —— origin/main(dec64ca)与本分支同一组 -ldflags 各编一个二进制,4 个目标(恶意 skill / 良性 skill / 单文件 / zip)× 8 种输出(文本、--json、--md -、--sarif、--quiet、--fail-on low、--fail-on critical、判官就绪的 --config)= 32 例,stdout / stderr / SARIF / 退出码 0 处不同。只规整了同一个二进制两次运行本来就不同的:scanned_at;zip 目标的临时解压目录名 aguard-inbox-N 与由它派生的 SARIF 指纹 aguard/v1(main 的二进制对同一个 zip 跑两次,6 个指纹全不同;既有问题,不属本条)
证据:错误路径按设计不同(见「不能说什么」):check <恶意 skill> --fail-on hgih --json,main exit 2 + stdout 4610 字节,本分支 exit 2 + stdout 0 字节,stderr 相同
证据:反向断言不改一字仍绿 —— TestGateAgreesWithCheck、TestFailGate、TestFailGate_*(6 条)、TestCheckTarget_*(8 条)、TestMarkdownFlag_*(3 条)、baselines/adapter/aguard 的 passthrough_test.go(一字未改)
证据:移植 —— 旧仓 8 个代码提交(module path 已换成 AgentGuard、P 号已换成 P-004)按顺序 git am -3 打在 main 的 dec64ca 上;唯一冲突是 W4 的 ROADMAP.md 那一段(本仓库的 ROADMAP 是重写过的,没有那句"check 从不调判官"),丢掉该段,其余 7 个文件原样应用;其余 7 个提交无冲突、无手工改动
证据:不做什么 —— git diff --stat origin/main -- internal/report internal/model internal/gate internal/score internal/collect internal/detect plugin README.md README.zh-CN.md ROADMAP.md .claude/rules/invariants.md go.mod go.sum baselines/adapter/aguard/passthrough_test.go baselines/results baselines/tools.yaml hack 为空;internal/judge/judge.go、internal/config/config.go、cmd/aguard/gate.go、baselines/adapter/aguard/aguard.go 只改注释,baselines/cmd/baseline/main.go 只改 -aguard-extra-args 的帮助串
证据:make verify: all gates passed;go version go1.23.5(无工具链切换);go.mod 第二行 go 1.23.5,无新依赖;cmd/aguard 覆盖率 46.7% → 48.0%;真机扫描不适用(collect / detect 未动)
```

两条都合入后要补的(后合入的那一条负责,见「不做什么」):不变量 #1 的出网路径列表、规格 §16.4 的"出网的路径只有两条"、
`baselines/tools.yaml` 的 `uploads_samples_basis`、`TestZeroDial_OnlyTheJudgeConnects` 正对照里的一行 `check --llm`(P-003);
一条 `check --llm`(目录与 `.zip`)发出去的内容已擦 home 的出网断言(P-005,它让判官总是擦 `os.UserHomeDir()`,所以 `check --llm` 被它覆盖)。
**已补前四处**(2026-10-09,P-003 先合入,本条后合入,所以由本条负责):不变量 #1 的列表与零表、规格 §16.4 与 §13 ④、
`baselines/tools.yaml` 的 `uploads_samples_basis` 都改成三条路径;`TestZeroDial_OnlyTheJudgeConnects` 正对照加 `check --llm`,
零表加 `check --llm` 但 `llm.enabled: false`。先加两行测试,`TestZeroDial_ClaimsNameTheTest` 当场点名三处文档没写(红),补完转绿。
最后那条擦 home 的出网断言留给 P-005:它在本条之后合入。
