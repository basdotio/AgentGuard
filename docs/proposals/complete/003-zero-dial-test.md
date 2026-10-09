<!-- SPDX-License-Identifier: MIT -->
# 003 — "绝不外连"没有一条测试钉住,baselines 的模板却说有

- **来源**:新发现(2026-10-09)—— 不变量 #1(默认零外连)是工具可信的根据,但没有任何测试钉住它;
  baselines 的说明文字却声称由测试保证。移植自旧仓 agent-guard 的 P-044(私有仓)
- **依赖**:无
- **分支**:`p/003-zero-dial-test`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

不变量 #1(`.claude/rules/invariants.md:7`)写着"绝不执行被扫描内容,绝不外连(除显式开启的 LLM judge)"。
README、`docs/architecture.md` 的简表、`cmd/aguard` 里几处注释都在复述它。**可是全仓没有一条测试断言过它**:

| 地方 | 现在写着什么 | 实际有什么 |
|---|---|---|
| `baselines/tools.yaml:33-37`(aguard 的 `uploads_samples_basis`) | "invariant #1, **enforced by tests in this repository**: aguard never connects out except for the explicitly opted-in LLM judge" | `grep -rn 'RoundTrip\|DefaultTransport' --include='*_test.go'` 只命中判官自己的单测名(`TestHTTPClient_RoundTripAndRedaction`)和 baselines 的一个 YAML 往返测试;判官的 `httptest` 单测测的是"判官发了什么",不是"别的命令没发" |
| `baselines/cmd/baseline/main.go:397-413` `uploadsFor` | 不带 `--llm` 时把上面那句原样写进每一份 run.yaml | 已提交的 `baselines/results/aguard/2026-09-24/run.yaml:10`、`2026-09-29e/run.yaml:10` 都带着这句 |
| `cmd/aguard/llm.go:112` `runLLMSetup` 的注释 | "It never prints the key and never sends anything" | 没有测试 |
| `cmd/aguard/main.go:807` `version` 命令的注释 | "Offline by construction … never a release feed" | 没有测试 |

今天这句话**碰巧是真的**:产品里唯一的 `http.Client` 在 `internal/judge/openai.go:52-65` 的 `NewHTTP`
(传 `nil` 就新建一个 `&http.Client{}`),两个调用点 `cmd/aguard/main.go:273`(`runJudge`)和 `cmd/aguard/llm.go:220`
(`runLLMTest`)都传 `nil`;信誉库是 `go:embed`,`gate`、`collect` 没有网络代码,`hack/reputation-refresh` 是另一个二进制。
但"碰巧是真的"和"被测试钉住"是两回事:

- **下一次加网络调用的人没有任何东西会变红。** 一个顺手的"setup 时验证一下 key"、"version 时查一下新版本"、
  "check 也跑判官"(P-004 做的正是最后这一条),都会把一条新的出网路径加进来而全绿。
- **对外引用的那份文件在替我们说一句没有证据的话。** run.yaml 是别人引用测量结果时读的文件(`uploadsFor` 的注释原话),
  它说"enforced by tests",而那些测试不存在。本仓库的披露纪律是"绿灯不构成证据";这里连绿灯都没有。
- **不变量本身也说不清边界。** "除显式开启的 LLM judge"没说是哪几个命令。`scan --llm` 会连,`llm test` 也会连
  (一次 Ping),`check`、`hook`、`approve` 不会 —— 这份清单只在读代码的人脑子里。

## 初步方向

`internal/judge` 加一个包级测试接缝 `var Transport http.RoundTripper`(nil = `http.DefaultTransport`,和今天一样),
`NewHTTP` 在调用方没给 client 时用它。`cmd/aguard` 加一条进程内测试:配置里**开着**判官,把接缝换成只计数、返回错误的
RoundTripper,逐个跑命令入口,断言零次 round trip;同一个计数器在 `scan --llm` 上必须看到 ≥ 1 次,证明它不是瞎的。

不变量 #1 改写成"今天只有这几条路径可以出网"的枚举清单,并写明钉住它的是哪条测试;baselines 那句改成点名这条测试。
不改判官行为,不加网络路径,已提交的 `baselines/results/` 不动。

## 完成的判据

- [x] `TestZeroDial_OnlyTheJudgeConnects`(`cmd/aguard/zero_dial_test.go`,新):配置**开着**判官
  (`writeJudgeConfig` 指向 `http://127.0.0.1:9`,`max_retries: 0`),`judge.Transport` 和 `http.DefaultTransport`
  各换成一个只计数、返回错误的 RoundTripper,下面每个入口跑完**两个计数器都是 0**,且入口本身没有出错
  (出错就早退的入口零次是空话):
  `scanEnv`(不带 llm;`clean` 用的也是它)和 `scan` 在评分后追加的 `gateLivenessNote`(fixture 用真的
  `gate.PlanInstall` 注册了一个指向已删除二进制的闸门,必须返回 `GATE-001`)、`scanInbox`(不带 llm)、
  `scanEnv` + `scanInbox` 带 llm 但 `llm.enabled: false`、`checkTarget`(和 `check` 命令同样的 opts)、
  `runHook` 喂 `PreToolUse`(回复必须是 `ask`,证明真扫了)和 `SessionStart`(回复里没有 "could not audit")、
  `PostToolUse` 的重扫(经 `gateOptions` + `gate.Handle` 共用一个内存 store,回复必须是 "risk accepted";
  为什么不经 `runHook` 见未决 6)、`approvePath`、`listApprovals`(这一行自己先往 store 里写一条批准,输出必须列出它
  —— 不靠前一行 `approve` 先跑,`-run` 单选这一行也成立)、`runLLMSetup`、`runLLMStatus`、
  `runVersion`(见下一条)、`collect.CollectTarget`(`hash`)。今天 `judge.Transport` 不存在,编译即红
- [x] **`version` 跑的是整段命令体,每个分支一行**:`version` 命令的函数体原样搬进 `runVersion(w, root)`,零表按
  `pluginVersionLine` 和它委托的 `versionLine` 的**每个出口**各跑一行 —— 对装了 `aguard` 9.9.9 的 root,以发布版
  `v1.0.0` / `v9.9.9` / `v10.0.0` 跑出 newer / matches / older 三种比较行,以 `dev` 跑出"dev build; not compared";
  没装插件、`plugin.json` 不带版本两个 root 走两个返回空的出口;只装了旧名 `agentguard` 的 root 走改名提示,
  新旧两个名字都装了的 root 走"旧插件还在"的后缀(这两个出口是 v0.17 改名时加的)。另有一行**不经 `-ldflags`**,
  从构建信息盖版本(`applyBuildInfo`,`go install` 装出来的二进制走的那条路,v0.18 起):构建行必须印出构建信息里的
  版本和 commit,插件行照样比较。每行都断言构建行在、插件那行是这个分支该印的那句(或没有)
- [x] **反向断言(正对照,防止测试是瞎的)**:同一个测试里、零表**之前**,同一对计数器在三条允许路径上必须看到
  判官计数器 ≥ 1、默认计数器 = 0:`scanEnv(llm: true)`、`scanInbox(llm: true)`、`runLLMTest`。
  前者证明接缝真的接在判官的 client 上,后者证明判官的请求没有绕开接缝
- [x] **每一行开始时两个计数器必须已经是空的**,零表跑完等 50 ms(`lateRequestSettle`)再收一次:
  入口返回之后才落地的请求报成"在某一行返回之后落地",不被下一行开头的清零吞掉,也不会在计数器还原后无人看见。
  只保证"报出来",**不保证记在发它的那一行**(见不能说什么)
- [x] `TestNewHTTP_TransportSeam`(`internal/judge/run_test.go`,新):不设接缝时 `NewHTTP(…, nil)` 建出的 client
  `Transport == nil`(即 `http.DefaultTransport`,和今天的 `&http.Client{}` 一样);设了接缝,请求走它;
  **反向断言**:调用方自己给的 client(现有测试都给 `srv.Client()`)原样使用,接缝不覆盖它
- [x] `TestZeroDial_ClaimsNameTheTest`(`cmd/aguard/zero_dial_test.go`,新):用 `runtime.FuncForPC` 取上面那条测试的
  **真名**,断言 `baselines/tools.yaml` 里 aguard 的 `uploads_samples_basis` 和 `.claude/rules/invariants.md` 都含它。
  改测试名而不改这两处 → 红;今天 tools.yaml 只写"enforced by tests in this repository" → 红。
  外加**集合相等**:正对照(`zeroDialControl`)里每条路径必须是不变量 #1 清单里的一条 `` - `路径`: ``,清单里每条必须有
  正对照看着它出网(两个方向);每条路径还必须出现在 `baselines/tools.yaml` 那句和 spec §16.4 的出网清单那一行里
- [x] `TestZeroDial_NoClientOutsideTheJudge`(`cmd/aguard/zero_dial_source_test.go`,新):`go/parser` 读 `cmd/`、`internal/`
  全部非测试 `.go`,`internal/judge` 之外不许出现 `net/http` 的 `Client`/`Transport` 类型(字面量、`new()`、变量声明、
  对默认 transport 的类型断言 + `Clone` 都会经过这个名字),`judge.Transport` 不许在非测试文件里被赋值、取地址或带初值;
  import 了 `cmd/`、`internal/` 以外的本模块包也红(遍历范围不够了)。`internal/judge` **不整包豁免**:包内不许出现
  `http.Transport`;`http.Client` 只许是 `NewHTTP` 里那个 `http.Client{Transport: Transport}` 字面量的类型(键值全写名字、
  `Transport` 的值就是接缝这个标识符),或 `*http.Client` 字段/参数/返回值的类型(声明,不造 client)。
  **反向断言**:读到的文件数 > 0,必须在 `internal/judge` 找到 `var Transport` 的声明,且 `NewHTTP` 里那种字面量
  **恰好一个** —— 否则这条检查什么都没守
- [x] 反向断言:`TestHTTPClient_RoundTripAndRedaction`、`TestHTTPClient_CountsTokens`、`TestRun_RetriesOnlyRetryableErrors`、
  `TestLLMCommands_SetupTestStatus`、`TestE2E_*`(七条;开判官的那几条经 `NewHTTP(…, nil)` 打 `httptest`)、
  `TestAJudgeRunDisclosesTheUpload`、`TestPluginVersionLine`、`TestPluginVersionLine_LegacyName`、`TestApplyBuildInfo`
  **不改一字**仍绿 —— 判官的行为、`uploadsFor` 的行为、`version` 的比较与盖版本都没变
- [x] `make verify` 绿;`go.mod` 第二行仍是 `go 1.23.5`,`go version` 无工具链切换

## 不做什么

- **不加任何网络路径**:产品代码里 `net/http` 的使用只多一个包级变量的读取,出网的入口还是那两条。
  另一处产品代码改动是 `version` 命令的函数体原样搬进 `runVersion(w, root)`(`main.go` 一个闭包变一行调用,
  `version.go` 多一个函数),输出逐字节不变(证据见「完成」)
- **不改判官行为**:请求内容、重试、超时、`CheckEndpoint`、`Usage()` 都不动;`NewHTTP` 的签名不动,
  调用方传 `nil` 时得到的 client 在接缝为 nil 时与今天逐字段相同
- **不改 `version` 的行为**:`pluginVersionLine` / `versionLine` / 几个提示函数、`applyBuildInfo` 一字不动
- **不做重定向处理**(`CheckRedirect` / 跨主机重定向带走 Bearer 头):本条不碰 `http.Client` 的任何字段,除了 `Transport`
- **不做 CI 网络隔离 job**:那是能看见裸 socket 和子进程的一层,本条只钉经过 `judge.Transport` /
  `http.DefaultTransport` 的那一层,外加一条堵自带 transport 的源码检查
- **不加静态导入白名单**(见未决 2)。`TestZeroDial_NoClientOutsideTheJudge` 不是它:它不管谁 import
  `net/http`、`net`、`os/exec`,只管有没有人在接缝之外造 `Client`/`Transport`、有没有人在生产代码里动接缝 ——
  这是计数器自己的盲区,补的是本条测试的视野,不是"不执行"那一半
- **不加 `check --llm`**:P-004 并行在做;两边都合入后由后合的那一个改五处(见未决 3)
- **不修闸门 pending 跨进程丢失**:P-007 并行在做(见未决 6)
- **不改已提交的 `baselines/results/`**:2026-09-24、2026-09-29e 两份 run.yaml 里那句是当时写下的记录
- **不改 `baselines/cmd/baseline` 的 Go 代码**:`uploadsFor` 原样转发 registry 的句子,改的是 `tools.yaml` 里那句本身
- 不改 README / `docs/architecture*.md` 的不变量简表(见未决 5)
- 不加依赖,不碰 `go.mod` / `go.sum`

## 不能说什么

- **不说"已证明零出站"/"verified zero egress"**。**也不说"进程内经过 `net/http` 的请求都看得见"**:计数器只看得见
  经过 `judge.Transport` 或 `http.DefaultTransport` 的请求。看不见的:自带 `http.Transport` 的 client(本模块的产品代码由
  源码检查补上,`internal/judge` 也在内;`hack/`、`baselines/` 是别的二进制,不在它读的范围里)、**依赖在它自己代码里造的
  这种 client**(源码检查只读本模块;今天二进制里别的模块都不 import `net/http`、`os/exec`,`pflag` import `net` 只为
  IP 类型的 flag —— 读代码的结论,加第四个直接依赖前要先读它)、裸 `net.Dial`、`exec` 一个 `curl`(今天产品代码里两者都
  没有 —— `os/exec` 零导入,`net` 只用来 `ParseIP` —— 但那是读代码的结论,不是这条测试的结论)、最后一行返回 50 ms 之后
  才发出的异步请求、只写在 cobra `RunE` 闭包里的代码、**包级 `init()`**(测试装上计数器之前它就跑完了;`applyBuildInfo`
  在 `version` 的构建信息那一行里另跑一次,`init()` 里别的东西不在视野里)
- **不说"判官包里的 client 由正对照看着"**:正对照只看着它自己那三条路径用的 client。能说的只是:判官包内**只许
  `NewHTTP` 那一个** client,它的 transport 是接缝。`NewHTTP` 里那个字面量的 `Transport` 值,源码检查只认标识符
  `Transport`,不追它解析到哪:在 `NewHTTP` 里用 `var Transport = …` 或同名参数遮住包级接缝,源码检查看不出来
  (`Transport := …` 会被当成给接缝赋值而红)。遮住之后的值要么写出 `http.Transport`(源码检查红),要么是
  `http.DefaultTransport`(正对照红),要么来自依赖或一个自己拨号的 RoundTripper —— 即上面两条盲区
- **不说 `-run` 单选的一行"证明了零"**:26 行(正对照 3 + 零表 23)每一行单独选中都能跑过,但单选时正对照不跑,那一行的零不证明计数器接在了
  判官的 client 上。证据以整张表一起跑为准
- **不说零表跑的是"命令"**:每一行调的是命令调用的那个函数(`scanEnv`、`checkTarget`、`runHook`、`runVersion`…),
  不是 cobra 的 `RunE` 闭包;闭包里在那个函数之外加一行请求,这条测试看不见。`version` 是唯一一个把整段闭包体搬进
  函数的,其余命令的闭包里还有渲染、`failGate` 之类本条没跑的代码
- **不说 `version` 的每一行代码都跑过**:每个出口一行,但几个只拼字符串的提示函数(`updateHint`、`renameHint`、
  `leftoverHint`、`switchCommand`)内部按安装渠道(桌面 / CLI)和 marketplace 再分的支,零表只走到其中 fixture 那一种
  (CLI 安装、本项目的 marketplace)
- **不说异步请求会记在发它的那一行**:晚到的请求报成"在 X 返回之后落地",落在后面某一行运行期间的记在那一行头上 ——
  会红,但行名可能不对
- **不说"所有命令都测了"**:`hook install/uninstall/status`、`approvals forget`、`clean --apply/--undo/--ask` 没进零表;
  它们只动本地文件,但没有被这条测试跑过。不变量里的清单写的是**跑过的那些**
- **不说新命令会自动被拦住**:已经在零表里的入口一旦出网就红;**新加的**命令要自己进表,测试不会替你发现它
- 不说已提交的两份 run.yaml"当时就是对的":那句话写下时测试不存在;它现在才变成真话

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 三条新测试,跑红(编译红:`judge.Transport` 不存在;tools.yaml 不点名) | `judge, cmd: tests — nothing counts what the commands send, and the baselines claim names no test (P-003)` |
| 2 | `judge.Transport` 接缝,`NewHTTP` 在 client 为 nil 时用它 | `judge: a nil client takes its transport from a test seam, so a test can count every request (P-003)` |
| 3 | 不变量 #1 改写成枚举清单 + 点名测试;spec §16.4 与 §13 同步 | `rules, spec: invariant #1 lists the only two paths that may connect out and names the test that pins it (P-003)` |
| 4 | `baselines/tools.yaml` 那句点名测试(顺手去掉句尾指向旧仓编号的 `(P-021)`) | `baselines: the registry's upload claim names the test that enforces it (P-003)` |
| 5 | 零表补上 `scan` 在评分后追加的闸门存活探测,不变量清单同步 | `cmd, rules: the zero table also covers the gate-liveness probe scan appends after scoring (P-003)` |
| 6 | `version` 的函数体抽成 `runVersion`,零表那一行跑整段命令体、对着装好的插件、以发布版跑 | `cmd: the version row runs the command's whole body against an installed plugin, so a request from either half of it is seen (P-003)` |
| 7 | 不变量 #1、spec §16.4 改成"只看得见两个 transport"并逐条列盲区;源码检查堵产品代码里自带 transport 的 client | `rules, spec, cmd: invariant #1 says the counters see two transports and lists what they miss; a source check closes the own-transport gap for product code (P-003)` |
| 8 | 每行开头断言计数器为空,表尾等 50 ms 再收一次 | `cmd, rules, spec: a request that lands after its row returned is reported, not discarded by the next row's reset or lost when the counters are put back (P-003)` |
| 9 | 正对照与不变量 #1 的出网清单是同一个集合,tools.yaml 和 spec §16.4 也要写出每条路径 | `cmd, rules: the positive control and invariant #1's list of outbound paths are one set, so check --llm cannot join the control without joining the list (P-003)` |
| 10 | 闸门存活探测那行要找到 fixture 里的死注册,`approvals` 那行要列出被批准的 skill | `cmd: the gate-liveness row must find the fixture's dead registration and the approvals row must list the approved skill, so neither zero is about nothing (P-003)` |
| 11 | 源码检查不再整包豁免 `internal/judge`:包内只许 `NewHTTP` 那一个接缝 client;文件头、不变量 #1、spec §16.4/§13 同步,并把"依赖自己造的 client"列成盲区 | `cmd, rules, spec: inside internal/judge only NewHTTP's seam client may be built, so a second judge client with a transport of its own is red instead of dialling unseen (P-003)` |
| 12 | `approvals` 那行自己写入要列出的批准,不靠 `approve` 那行先跑 | `cmd: the approvals row seeds the approval it lists, so it passes when -run selects it alone instead of depending on the approve row having run (P-003)` |
| 13 | `version` 按 `pluginVersionLine` / `versionLine` 的每个出口各跑一行(含 v0.17 加的改名两出口),外加一行从构建信息盖版本;不变量 #1、spec 的盲区里加上 `init()` | `cmd, rules, spec: the version command runs once per return of pluginVersionLine and versionLine and once stamped from build info, so a request added to any branch is seen, not only the newer one (P-003)` |
| 14 | 本文件「完成」、索引 | `proposals: P-003 (P-003)` |

## 未决问题

1. **接缝只换 `judge.Transport`,还是也换 `http.DefaultTransport`?**
   **建议**:两个都换,各配一个计数器。`judge.Transport` 是定下的接缝,正对照用它证明"判官的请求走接缝";
   `http.DefaultTransport` 是兜底:判官之外任何一处新加的 `http.Get` / `http.DefaultClient` 都走它,在零表入口上会被数到。
   只换后者其实也能测今天的判官(nil Transport 就是 DefaultTransport),但哪天判官的 client 换了自己的 Transport,
   测试就会从"数得到"悄悄变成"数不到"——正对照会抓到,但接缝让它不必依赖这个巧合。
   **已决(2026-10-08)**:按建议。
2. **要不要顺手加一条静态导入白名单测试(`go/parser` 扫非测试 `.go`:`net/http` 只许 `internal/judge`,`net` 只许 `ParseIP` 一类,`os/exec` 零)?**
   **建议**:不在本条做。它该和不变量 #1 的另一半("不执行",`os/exec`)一起设计,而 CI 网络隔离才是能看见裸 socket
   和子进程的那一层;本条只钉经过两个 transport 的请求,并在「不能说什么」里写明这个边界。
   **已决(2026-10-08)**:按建议。`TestZeroDial_NoClientOutsideTheJudge` 不是这里问的导入白名单,见「不做什么」。
3. **枚举清单里写不写 P-004 的 `check --llm`?**
   **建议**:不写。P-004 在并行分支上,本分支的代码里它不存在;清单写的是今天的事实。两边都合入后,后合的那个补一行、
   把 `check --llm` 从零表挪到正对照(PR 里说明)。后合的那一个要改的五处,写死在这里以免只留在 PR 上 ——
   ① 不变量 #1 的出网清单加一条 `` - `check --llm`: ``(连同"只有两条路径"的条数);② spec §16.4 出网清单那一行;
   ③ `baselines/tools.yaml` 那句的 "(scan --llm, llm test)";④ 零表 `check` 那一行的注释("the opts `check` passes"——
   P-004 之后 `check` 传的是 `llm: useLLM`,零表那行要么改名为"不带 `--llm` 的 check",要么改 opts);
   ⑤ `zeroDialControl` 加一行 `check --llm` 正对照(`checkTarget` 带 `llm: true`)。①②③⑤ 由
   `TestZeroDial_ClaimsNameTheTest` 的集合相等逼着一起改,④ 只能靠人看。
   **已决(2026-10-08)**:按建议。
4. **已提交的两份 run.yaml 里"enforced by tests in this repository"要不要改?**
   **建议**:不改。results 是测量记录,写下即冻结;改 registry 那句,之后的运行自然带上点名的版本。
   **已决(2026-10-08)**:按建议。
5. **枚举清单要不要也写进 README / `docs/architecture*.md` 的不变量简表?**
   **建议**:不。简表那句"除显式开启的 LLM judge"仍然对,而且它们自己写着"全文在 `.claude/rules/invariants.md`";
   清单再复制进两对双语文档,就多出四份会各自漂移的拷贝 —— `TestZeroDial_ClaimsNameTheTest` 只看 `invariants.md`、
   `tools.yaml` 和 spec §16.4 出网清单那一行。
   **已决(2026-10-08)**:按建议。
6. **(实现时发现,不在本条范围)闸门在弹窗里给的"同意"从来记不下来。** `gate.LoadStore`(`internal/gate/approvals.go:96-125`)
   只把 `approvals` 读回来,**不读 `pending`**;而每个 hook 事件是一个新进程(`runHook` 每次都 `LoadStore`)。
   于是 `PreToolUse` 判 `ask` 时写进磁盘的那条 pending,在 `PostToolUse` 的进程里永远是空的,`handlePost` 在
   `pendingFor` 处直接返回,重扫和"risk accepted"那条路径在真实运行里走不到。
   **建议**:不在本条修 —— 它改的是闸门的行为,和出网无关。P-007 是修它的那一份。本条的零表因此经 `gate.Handle`
   驱动 `PostToolUse` 的重扫,那是它唯一可能出网的一半;P-007 合入之后可以改回 `runHook`。
   **已决(2026-10-08)**:按建议。

移植时追加的:

7. **`version` 那几行怎么对上本仓库 v0.17 / v0.18 的 `version`?** 旧仓的 `pluginVersionLine` 是一个函数六个出口;
   本仓库 v0.17 把插件从 `agentguard` 改名为 `aguard`,`pluginVersionLine` 多了"只装了旧名"和"新旧都装了"两个出口,
   比较挪进 `versionLine`;v0.18 起没被 `-ldflags` 盖章的二进制从构建信息取版本(`applyBuildInfo`,在 `init()` 里跑)。
   **建议**:零表按两个函数的**每个出口**各一行(八行),插件用本仓库的名字和 marketplace(`aguard@AgentGuard`,旧名
   `agentguard@AgentGuard`),每个 root 用现成的 `writePluginInstalls` 搭;再加一行从构建信息盖版本,让 `applyBuildInfo`
   在计数器下跑一次;`init()` 本身记进盲区。`runVersion` 只是搬家,不碰盖版本的逻辑 —— 它读的还是那三个包级变量。
   **已决(2026-10-09)**:按建议。

## 完成

```
合入:PR #22(2026-10-09;sha 用 git log --grep P-003 找)
发布:待发
证据:TestZeroDial_OnlyTheJudgeConnects(cmd/aguard/zero_dial_test.go);W1 时编译红(cmd/aguard/zero_dial_test.go:85: undefined: judge.Transport),W2 后绿:正对照三行 scan --llm / Downloads 项 / llm test 都被判官计数器看见、默认计数器 0;零表 23 行(14 个入口 + version 9 行)两个计数器都是 0,且每个入口成功跑完(Pre 回 ask、Post 重扫回 risk accepted、SessionStart 有审计结果、闸门存活探测报 GATE-001、approvals 列出自己写入的那条)
证据:TestNewHTTP_TransportSeam(internal/judge/run_test.go);W1 时编译红(run_test.go:306: undefined: Transport),W2 后绿;反向断言:调用方给的 srv.Client() 照样打到自己的 server,接缝计数不变
证据:TestZeroDial_ClaimsNameTheTest;W2 后两处都红("baselines/tools.yaml says aguard's no-upload claim is enforced by tests, but does not name TestZeroDial_OnlyTheJudgeConnects" + "invariant #1 does not name the test that pins it")→ W3、W4 后绿;集合相等(W9):正对照加一行 check --llm、文档不动 → 红 3 条(不变量 #1 没列、tools.yaml 没写、spec §16.4 没写);不变量 #1 加一条 check --llm、无正对照 → 红 1 条;不变量 #1 删掉 llm test → 红 1 条
证据:变异(未提交,跑完即还原,工作区 git status 为空)—— checkTarget 里强开 o.llm → Downloads 项(不带 --llm)/ check / hook Pre / Post 重扫 / approve 五行红(判官计数器 3 / 4 / 3 / 6 / 4);NewHTTP 改回 &http.Client{} → 正对照三行红(判官计数器 0,默认计数器 13 / 3 / 1)且源码检查红 2 条(那个字面量不是接缝形状;接缝字面量 0 个),TestNewHTTP_TransportSeam 同时红
证据:(W6)version 一行跑整段命令体;W5 时在 version 的 cobra 闭包里加 http.Get(127.0.0.1:9) → 全绿(只调 pluginVersionLine,看不见);W6 后同一个请求放进 runVersion → 红:"version sent 1 request(s) through http.DefaultTransport to [127.0.0.1:9]";放在 runVersion 外、闭包里 → 照绿(记进不能说什么)
证据:(W6)输出逐字节不变 —— W5 与 W6 各用同一组 -ldflags(-X main.version=v1.0.0 -X main.commit=abc1234 -X main.date=2026-10-09)和不带 -ldflags(-buildvcs=false,走构建信息、落回 dev)各构建一次,version --root 四个 root 上 cmp 全部相同:装了 aguard 9.9.9 的临时 root(237 B / 2 行,含 "plugin aguard 9.9.9 is newer than this binary (1.0.0)")、只装了旧名 agentguard 0.16.0 的临时 root(312 B / 2 行,改名提示)、本机 ~/.claude(312 B / 2 行)、不存在的 root(74 B / 1 行);不盖章的构建 226 / 303 / 303 / 65 B,同样逐字节相同
证据:(W7、W11)TestZeroDial_NoClientOutsideTheJudge(cmd/aguard/zero_dial_source_test.go);runLLMStatus 里 (&http.Client{Transport: &http.Transport{}}).Get(127.0.0.1:9) → TestZeroDial_OnlyTheJudgeConnects 照绿(盲区实测),源码检查红 2 条(llm.go:233 的 http.Client、http.Transport);internal/judge 里另造一个自带 transport 的 client、从 checkTarget 调用:W10(判官包整包豁免)三条 TestZeroDial_* 全绿 → W11 后源码检查红 2 条(zz_isolated.go:8 的 http.Client、http.Transport);NewHTTP 里 var Transport = http.DefaultTransport 遮住接缝 → 源码检查绿、正对照三行红(判官计数器 0,默认计数器 13 / 3 / 1),记进不能说什么
证据:(W8)runLLMStatus 起一个 goroutine,20 ms 后 http.Get:W7 时单次运行 3/3 绿(漏掉),-count=5 时记在下一轮的 "scan --llm" 头上(记错行);W8 后 -count=5 次次红:"a request landed after "hash" returned, before the counters are put back"
证据:(W10、W12)闸门存活探测那行:fixture 不注册闸门 → 红("want the fixture's dead registration reported as GATE-001, got []");gateLivenessNote 直接返回 nil → 红;approvals 那行:listApprovals 有记录时什么都不印 → 红;W11 时 -run 单选 approvals 一行红("approvals did not list the skill approved in the row above, so it read nothing: \"\"")→ W12 后绿
证据:(W13)version 九行;pluginVersionLine / versionLine 的八个出口前各插一个 http.Get(127.0.0.1:9),外加 applyBuildInfo 里一个,一次一处:W12(一行 version)只有 newer 那处红,matches / older / dev / 没装插件 / 无版本 / 只装旧名 / 新旧都装 / applyBuildInfo 八处全绿;W13 后九处全红,每处都让自己那一行红(newer 那处同时让"新旧都装"和"构建信息"两行红 —— 它们本来就走 newer 比较),其余八处只红自己那一行
证据:26 行逐行单独 -run:26/26 绿,每次恰好跑 1 行;go test -race -count=5 -run TestZeroDial 绿
证据:反向断言不改一字 —— git diff origin/main -- cmd/aguard/e2e_test.go cmd/aguard/main_test.go cmd/aguard/buildinfo_test.go internal/judge/judge_test.go baselines/cmd/baseline/passthrough_test.go 为空;internal/judge/run_test.go 只有新增行(+47 −0);TestHTTPClient_RoundTripAndRedaction、TestHTTPClient_CountsTokens、TestRun_RetriesOnlyRetryableErrors、TestLLMCommands_SetupTestStatus、TestE2E_* 七条、TestPluginVersionLine、TestPluginVersionLine_LegacyName、TestApplyBuildInfo、TestAJudgeRunDisclosesTheUpload 照绿
证据:不做什么 —— git diff --stat origin/main -- baselines/results baselines/cmd internal/gate internal/collect internal/detect cmd/aguard/buildinfo.go README.md README.zh-CN.md docs/architecture.md docs/architecture.zh-CN.md go.mod go.sum 为空;产品代码的改动是 internal/judge/openai.go(+11 −1:一个包级变量和它的注释,NewHTTP 里一个字段)、cmd/aguard/main.go(+1 −9)与 version.go(+18 −0):version 命令体原样搬进 runVersion
证据:make verify: all gates passed;go version go1.23.5(无工具链切换),go.mod 第二行 go 1.23.5;collect / detect 未改,不需要真机扫描
```
