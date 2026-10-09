<!-- SPDX-License-Identifier: MIT -->
# 005 — BYO 判官把用户名、绝对路径和不带键名的 env 值发给模型厂商

- **来源**:新发现(2026-10-09)—— 开 `--llm` 时,发给用户自配模型端点的摘录里带着 `$HOME` 绝对路径和用户名
  (hook 命令、MCP 参数、memory 内容、triage 里的文件位置),MCP 的 env 值不带键名发出,按键名的脱敏因此从不触发。
  移植自旧仓 agent-guard 的 P-045(私有仓)
- **依赖**:无
- **分支**:`p/005-judge-egress-paths`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

`--llm` 对着在线端点跑时,请求体(`internal/judge/openai.go` 的 `chatRequest`:`model`、`messages[system,user]`、`temperature`)
里的 user 消息由 `prompt.go` 的 `userPrompt` 拼出,内容是各趟的 `Behavior` / `Declared`,再加 triage 的
`[RULE] file:line snippet`。`Request.Artifact`(`kind:name` 标签,`run.go` 的 `planFor` 里赋值)**从不进请求体**——这一点是对的。
但另外三样东西会出去,而 `detect.Redact` 一样都不管:

| 漏出去的 | 从哪进的请求体 | 后果 |
|---|---|---|
| **家目录绝对路径,即用户名** | **内容**:hook command(`excerpt.go` `hookExcerpt`)、MCP 的 command / args / env(`mcpExcerpt` 经 `detect.ConfigStrings`)、`CLAUDE.md` 与 memory 正文、skill 脚本和描述、解码出来的 blob。**triage**:`run.go` `triageItems` 把静态发现的 `File:Line Snippet` 原样拼进去,其中 `EXFIL-005` 的 `Evidence.File` 是导入方的**绝对路径**(`collect/imports.go` `importCredentialFinding`),`~/.claude.json` 上的发现经 `detect.relPath` 的兜底变成 **`<用户名>/.claude.json`** | 厂商侧每次调用都拿到"这台机器的用户名 + 目录结构"。`Redact` 只认 secret 形状;它的高熵类含 `/`,于是**带数字的长临时路径**偶尔会被前半截抹掉,但留下的后半截恰好是用户名(`<REDACTED>.d/alice/…`);真机上的 `/Users/alice` 只有 12 个字符,根本不碰 |
| **Claude Code 的项目目录编码** | memory 文件的静态发现,`Evidence.File` 是 `projects/-Users-alice-work-x/memory/MEMORY.md`,经 triage 出去 | 同上,换了一种拼法;`-Users-alice-work-x` 不到 24 字符、不带数字,熵规则也不碰 |
| **MCP env 的值,不带键名** | `detect.collectStrings` 只收值,所以 `{"DB_PASS":"hunter2"}` 出去时是一行孤零零的 `hunter2` | 按键名脱敏(`redact.go` 的 `assignRE`)**结构上不可能触发**:它要看见键名。而且 `collectStrings` 走 map 的随机顺序,同一份配置两次运行的请求体字节不同 |

另有一处没有上限:skill 的 `Declared`(SKILL.md 的 description)整段照发,`parse.ReadMarkdown` 读到 1 MiB 为止;
intent 与 injection 两趟各发一次,`samples: 3` 时再乘三。

实测(本仓库 `origin/main`,dec64ca;fixture 是下面判据第一条那条 e2e 测试的:家目录以无数字的标记段 `alicemarker` 结尾,
里面一个 hook、一个 MCP server、`CLAUDE.md` 的 `@~/.env` 导入、一个 skill、一个 memory 文件,先在本地跑过一遍):
**13 个请求体里 11 个带这个用户名,1 个带 `hunter2`,没有一个带 `DB_PASS=<REDACTED>`**。
完整的家目录一次都没出现——熵规则把带数字的临时路径前半截抹成了 `<REDACTED>`,留下的正好是用户名那半截。
同一份 MCP 配置规划两次,第二次的值顺序就不同;3,000 个 `é` 的 description 在 intent 与 injection 两趟各发 6,000 字节。

一句话:**用户选了一个在线端点,同意的是"发脱敏摘录";实际发出去的还有他是谁、他的目录长什么样,和一个没被认出来的密码。**

## 初步方向

在 `internal/judge` 里**构造摘录的那一刻**把家目录的各种写法(原样、`EvalSymlinks` 之后、Claude Code 的项目目录编码)换成 `~`,
triage 的 `<用户名>/x` 兜底只改那一个结构前缀;MCP server 按排好序的 `key=value` 行渲染,让键名脱敏能触发、请求体字节稳定;
`Declared` 截到 1,000 字节。**不进 `detect.Redact`**:它是所有静态 snippet 的唯一收口,改它会改 text/JSON/SARIF 输出并重算 SARIF 指纹。

## 完成的判据

下面的测试一部分是旧仓实现之后两轮评审(评审 1–7、复审 1–4)补出来的;移植时整组一起带上,标注保留,方便对照它们各自钉住的是哪个漏洞。

- [ ] `TestE2E_JudgeBodiesCarryNoHomeOrKeylessSecret`(`cmd/aguard/e2e_test.go`,新):照 `TestE2E_CredentialImportNeverReachesTheJudge`
  的截包写法,fixture 的家目录是 `TempDir()/home.d/alicemarker`(**标记段不带数字**,熵规则没法让这条测试因为错的理由变绿),里面有:
  一个 hook(command 里有家目录,其中一处是 `EvalSymlinks` 形态)、`~/.claude.json` 的一个 MCP server(args 里有家目录,
  env 是 `{"DB_PASS":"hunter2","NODE_OPTIONS":"--require <家目录>/…"}`,后者让 `EXEC-010` 落在 `alicemarker/.claude.json` 上进 triage)、
  `projects/<编码后的家目录>/memory/MEMORY.md`(正文有家目录,外加一句 `INJ-001` 让编码路径经 triage 出去)、`CLAUDE.md`
  (正文有家目录 + `@~/.env`,`EXFIL-005` 的绝对路径进 triage)、一个 skill(description、脚本、一段 base64 blob 里都有家目录)。断言:
  **没有一个请求体**含家目录、它的 `EvalSymlinks` 形态、它的编码形态或 `alicemarker`;没有一个含 `hunter2`;至少一个含 `DB_PASS=<REDACTED>`;
  没有一个含任何 artifact 的 `kind:name` 标签;`~/notes`、`~/logs/audit.log`、`~/.claude/CLAUDE.md`、`~/.claude.json` 都在(换了,没删)。
  今天(`origin/main` dec64ca,W1 测试先行跑过):红,13 个请求体里 11 个带 `alicemarker`、1 个带 `hunter2`、0 个带 `DB_PASS=<REDACTED>`、0 个带那四个 `~/` 路径
- [ ] `TestScanInbox_JudgeBodiesCarryNoHome`(`cmd/aguard/e2e_test.go`,新;和上一条共用截包的 `capturingJudge`):`HOME` 指到 `TempDir()/home.d/bobmarker`,
  `~/Downloads` 里一个候选 skill 的脚本引用家目录,`scanInbox(…, llm: true)` → 没有请求体含 `bobmarker`。今天:红(1 个请求体带 `bobmarker`,没有 `~/notes`)
- [ ] `TestEgress_*`(`internal/judge/egress_test.go`,新,table-driven):原样 / `EvalSymlinks` / 编码三种形态都换成 `~`;
  **反向**:`/Users/alicemarker2/x`、`/data/Users/alicemarker/x` 不动(边界),散文里的裸用户名不动,三段式 `x/alice/.claude.json` 不动,
  空家目录与 `/` 是恒等;`alice/.claude.json` 只在 file 位置改成 `~/.claude.json`;`<REDACTED>.d/alice/…` 和 `/home/first.<REDACTED>`
  这两种被熵规则吃掉一半的形态、以及被 snippet 200 字节截断切在用户名里的 `/Users/alic…` 被补齐(未决 7);截在用户名之前的 `/Users/…` 不动。
  今天:编译红(`newEgress` 未定义)
- [ ] `TestPlan_MCPExcerptIsKeyedAndByteStable`(`internal/judge/plan_test.go`,新):同一份 MCP 配置规划 30 次,`Behavior` 字节相同;
  含 `command=npx`、`env.DB_PASS=<REDACTED>`,不含 `hunter2`;**反向**:`env.LOG_LEVEL=debug` 原样在(不是凭据名的值照发)。
  今天:红(第 2 次规划字节就不同,`hunter2` 原样、不带键名)。
  键名表本身由 `TestMaskCredentialValue`(`internal/judge/excerpt_test.go`,新)逐行钉住,反向行是 `NODE_OPTIONS`、`API_BASE`、`args` 的值不掩
- [ ] `TestPlan_DeclaredIsCappedOnARuneBoundary`(`internal/judge/plan_test.go`,新):长 description → `Declared` ≤ 1,000 字节、
  是合法 UTF-8,且 intent 那趟 SKILL.md unit 的 text 与 `Declared` 逐字节相等(落地比对的是发出去的字节)。今天:红(intent / injection 两趟各 6,000 字节)。
  评审 2:W1 版用 3,000 个 `é`,两字节、1,000 是偶数,切点本来就落在字符起点,删掉 rune 回退照样绿;W9 改成 2,000 个 `中`
  与 `a`+3,000 个 `é` 两行(前置断言第 1,000 字节落在字符中间),外加"最多退回一个字符"
- [ ] `TestConfigLines_SameLeavesAsConfigStrings`(`internal/detect/configlines_test.go`,新):`ConfigLines` 去掉键前缀后的值多重集合等于 `configStrings`——
  判官和静态读的是同一批叶子。今天:编译红(`ConfigLines` 未定义)
- [ ] 评审 1(相对 `--root` 关掉了替换;`CLAUDE_CONFIG_DIR` 和 `check --llm` 两处没覆盖):`TestE2E_RelativeRootStillStripsTheHome`、
  `TestE2E_ConfigDirUnderTheHomeStripsTheUserHome`、`TestCheckTarget_JudgeBodiesCarryNoHome`(`cmd/aguard/e2e_test.go`);
  `TestRun_EmptyHomeStillStripsTheUserHome`、`TestRun_ScanHomeAndUserHomeAreBothStripped`、`TestRun_ScanHomeInsideTheUserHomeKeepsItsPlace`、
  `TestEgress_RelativeHomeIsResolved`、`TestEgress_LaterHomeInsideAnEarlierIsDropped`(`internal/judge/egress_test.go`)
- [ ] 评审 3(先替换再脱敏让家目录下 16–23 字节的 token 逃过熵规则):`TestEgress_RedactsBeforeItStripsTheHome`,覆盖每个吃原始内容的构造器和 triage 的文件位置
- [ ] 评审 4(MCP 键排序 + 只截头,排在最前的填充键每次都把 env 挤出去):`TestPlan_MCPExcerptLeadsWithWhatTheServerRuns`、`TestRun_ShortenedMCPExcerptIsDisclosed`(含反向:装得下的配置零 note)
- [ ] 评审 5(项目目录编码逐字节):`TestEgress_NonASCIIHomeIsEncodedPerCharacter`(含反向:逐字节拼法不动)
- [ ] 评审 6(截断补齐跑在原始内容上):`TestEgress_ClipRepairIsForStaticSnippetsOnly`(含反向:两条静态 snippet 路径照补)
- [ ] 复审 1(`capLine` 的 rune 回退没有测试管:填充全是 ASCII,切点天然落在字符起点):`TestPlan_MCPLineCapCutsOnARuneBoundary`(`internal/judge/plan_test.go`)
- [ ] 复审 2(`LLM-000` 只被"截了一行"那半触发过;lead 键按恰好相等认没有测试管):`TestRun_DroppedMCPLinesAreDisclosedWithoutACap`、`TestPlan_MCPLeadKeysMatchExactly`(`internal/judge/plan_test.go`)
- [ ] 复审 3(截断补齐只认原样写法,编码写法 `projects/-Users-alic…` 原样发):`TestEgress_RepairsAnEncodedHomeTheSnippetCapCut`(`internal/judge/egress_test.go`,含反向:截在用户名之前、没截、更长名字的尾巴、原始内容、一段式家目录都不动)
- [ ] 复审 4(文档说"给定写法"和"三种写法",实际是取绝对路径并规整、一段式家目录不换编码写法):`TestEgress_HomeIsReplacedInItsAbsoluteCleanedForm`(`internal/judge/egress_test.go`);llm-judge、architecture 两对和 spec 改写
- [ ] 上面带"评审 / 复审"的各条在本仓库复测:补测试的按变异跑红,改代码的在修复前的代码上跑红,数字记在「完成」
- [ ] 反向断言,**断言一字不改**仍绿:`TestE2E_CredentialImportNeverReachesTheJudge`、`TestRun_GroundedFindingGetsRealLineNumbers`、
  `TestRun_UngroundedFindingIsDroppedAndCounted`、`TestGround_ChecksRedactedTextNotDisk`、`TestPlan_MCPUsesTheSameViewTheScannerSees`、
  `TestPlan_EveryKindIsFencedAndRedacted`、`TestBehaviorExcerpt_PayloadBelowPaddingReachesTheModel`、`TestDecodedPayloads_RedactsSecret`。
  其中直接调用 `planFor` / `behaviorExcerpt` / `decodedPayloads` 的 8 处调用多一个 `egress{}` 实参(零值 = 不替换),除此之外一行不动
- [ ] 反向断言:同一份 fixture 上**不带 `--llm`** 的 `scan --json`,`origin/main` 的二进制与本分支的二进制输出除版本号外逐字节相同——静态输出没动
- [ ] `make verify` 绿;`collect`/`detect` 有改动,跑真机扫描并记头部

## 不做什么

- **不动 `detect.Redact`**(`credKeys`、`assignRE`、熵规则都不动):它是所有静态 snippet 的唯一收口,改它会改 text/JSON/SARIF/HTML/markdown 输出、
  重算 SARIF 指纹。本条的替换只发生在判官构造摘录时
- **不动 `detect.relPath`,不动 `EXFIL-005` 的 `Evidence.File`**:用户 `.aguardignore` 里的 glob 是对着 `e.File` 匹配的
- **不替换裸用户名**:只换完整的家目录路径形态,和 triage 里 `<用户名>/x` 这一个两段式结构前缀;用户名可能是个常用词
- 不加 `ExcerptVersion`
- **不碰模型输出的渲染**:`judge.go` 的 `finding()` / `barrierFinding()`、`ground.go` 不动(P-006 并行在改,避免冲突)
- 报告里 LLM 发现引用的 `file` 不变:`sourceUnit.file` 是给报告用的,不出网,不改
- 不改提示词,不改 `judge.Client` 接口,不碰 `openai.go`(`NewHTTP` 不动),不加依赖,不动 `go.mod` / `go.sum`

## 不能说什么

- 不说"请求体里不再有身份信息":散文里的裸用户名、git 作者名、邮箱,家目录**之外**的绝对路径(`/opt`、`/srv`、临时目录),
  家目录的兄弟目录(`/Users/alice.bak`)都照发。本条换掉的是**两个具体目录**的几种写法:OS 用户的家目录(`os.UserHomeDir()`,即 `$HOME`)
  和被扫环境的家目录(绝对 `--root` 的上一级)。`$HOME` 指的不是本人(`sudo` 下、服务账户下),或者别的用户的家目录出现在内容里,照发。
  (评审 1 之前连这句都太强:相对 `--root` 时一个都没换,`check --llm` 从没换过,`CLAUDE_CONFIG_DIR=~/.config/claude` 时只换了 `~/.config`)
- 不说先脱敏再替换没有代价(评审 3 的修法):带数字、中间没有熵字符类之外字节的长家目录(典型是临时目录)会被熵规则整段吃掉,
  发出去是 `<REDACTED>.claude/…`——不泄露,但判官看不到 `~`,路径信息少了一截
- 不说 MCP 摘录总能装下 `env`:`command`、`args`、`env`、`url`、`headers` 排最前、每行 500 字节,但 `args` 元素(或名字以 `env.` 开头的顶层键)
  足够多时,排在后面的 lead 键仍会被挤出 6,000 字节。挤出去会在文本里写明、出一条 `LLM-000`,但模型没看到它
- 不说 `Redact` 现在认识路径或 `DB_PASS`:静态报告里一条规则命中 `DB_PASS=hunter2` 那一行时,snippet 仍是明文,而 triage 会把那条
  snippet 原样发出去。本条只改判官**自己构造**的 MCP 摘录
- 不说 `~` 一定是用户的家:`scan --root /proj/.claude` 时 `/proj` 和用户的家目录**都**换成 `~`,同一个请求体里的两个 `~` 指的不是同一处
  (被扫环境的家目录落在用户家目录之内时只换外层,那种情况下 `~` 只有一个意思)
- 不说凭据名掩码是完整的:它是键名启发式,只用在 MCP 配置这一处——那里键名是结构,不是散文。叫 `DB_CONN` 的密码照样只靠 `Redact`
- 不说项目目录编码规则是 Claude Code 文档写的:"非 ASCII 字母数字的**字符**一律换成一个 `-`"是从真机目录名反推的(评审 5 之前按字节换,
  非 ASCII 家目录的编码形态原样漏);BMP 之外的字符(emoji)怎么编码没观测过,按一个 `-` 处理。它一变,编码形态就重新漏
- 不说静态 snippet 里的家目录残片都补齐了:只补三种已知的切法(熵规则吃头、吃尾,200 字节截断切在用户名里);
  几种切法叠在同一个家目录上的其他组合,或者别的 `Redact` 规则(键名、URL 凭据)整段吃掉路径后留下的东西,不在覆盖里。
  第三种(截断)**只补静态 snippet**(评审 6):原始内容结尾的 `/Users/alic…` 是作者写的,照发。
  编码写法(`projects/-Users-alic…`)只补截断这一种(复审 3):它整段落在熵规则的字符类里,熵规则要么整段吃掉要么不碰,没有吃一半的形态;
  别的 `Redact` 规则切进编码写法中间留下的东西照样不在覆盖里
- 不说每个家目录的"三种写法"都换了(复审 4):换的是 `filepath.Abs` 之后的写法(相对的按工作目录解析,路径被 `Clean` 规整),
  只有调用方原字节才有的写法(内容里的 `/Users/./alice`)照发;只有一段的家目录(`HOME=/root`)**故意**不换编码写法,`-root` 照发(未决 6),
  原样写法 `/root/…` 照换
- 不说判官的 MCP 判断和以前一样:模型现在看到的是带键名的排序行,不再是一串值;`baselines/results/` 里已提交的 `LLM-009` 判官运行是在旧渲染上量的

## 工作项

W1–W8 是旧仓的首轮实现,W9–W15 是评审 1–7 的修复,W16–W20 是复审 1–4 的修复;移植时一条一个提交,顺序不变。

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 六组新测试,跑红 | `judge, detect, cmd: tests — judge request bodies carry the home path, the username and a keyless env password (P-005)` |
| 2 | `judge.egress`:家目录三种形态 + 两种半截形态 → `~`,`<用户名>/x` 结构前缀;`Options.Home`;在每个摘录、triage、能力摘要里先替换再 `Redact` | `judge: every excerpt replaces the home directory with ~ before redaction, so no request body names the user (P-005)` |
| 3 | `scanOpts.home`:环境扫描给 `filepath.Dir(root)`,Downloads 给 `os.UserHomeDir()`,经 `runJudge` 进 `Options.Home` | `cmd: the scan and Downloads judges are told which home to strip (P-005)` |
| 4 | `detect.ConfigLines`:同一批字符串叶子,按键排序渲染成 `key=value` 行;`configEntry` 一个读条目的函数;`configStrings` 包内版本(导出名留一个提交的过渡包装,判官还在用) | `detect: ConfigLines renders a config entry as sorted key=value lines from the same leaves the scanner reads (P-005)` |
| 5 | MCP 摘录改用 `ConfigLines`,键名是凭据名的值不发;去掉 `ConfigStrings` 过渡包装 | `judge: the MCP excerpt is sorted key=value lines and a value whose key names a credential is not sent (P-005)` |
| 6 | `Declared` 截到 1,000 字节,落在 rune 边界 | `judge: a declared purpose is capped at 1,000 bytes on a rune boundary (P-005)` |
| 7 | spec §5.2 / §16 不变量 3、`docs/llm-judge.md` 与 zh 对子的 Privacy、`docs/architecture.md` 与 zh 对子 | `docs: spec, llm-judge and architecture pairs say the judge strips the home directory and sends MCP config by key (P-005)` |
| 8 | 静态 snippet 的 200 字节截断切在用户名里的形态也补齐(未决 7,实现中发现) | `judge: a home the static snippet cap cut inside the username is completed before triage sends it (P-005)` |
| 9 | 评审 2:rune 边界那条测试改用 3 字节字符和错开一字节的 2 字节字符,删掉回退就红 | `judge: the declared-purpose cap test cuts inside a character, so deleting the rune walk-back turns it red (P-005)` |
| 10 | 评审 5:`projectDirName` 按字符编码 | `judge: a non-ASCII home is encoded one '-' per character, as Claude Code names the project directory (P-005)` |
| 11 | 评审 1:`Run` 总是加上 OS 用户家目录;家目录取绝对路径;后一个家目录落在前一个之内的写法丢掉;`scanEnv` 从绝对 root 取上一级;Downloads 的覆写删掉 | `judge, cmd: the judge always strips the OS user's home as well as an absolute scan home, so an empty or relative home no longer sends paths unchanged (P-005)` |
| 12 | 评审 3:先 `Redact` 再替换(文件位置也一样) | `judge: excerpts are redacted before the home is stripped, so a short token under the home is judged as the run the report saw (P-005)` |
| 13 | 评审 6:截断补齐只给静态 snippet(`egress.snippet`,triage 与串通摘要) | `judge: only static snippets get the clipped-home repair, so raw text ending in a path fragment is sent as written (P-005)` |
| 14 | 评审 4:MCP 摘录 lead 键在前、每行 500 字节、按行装预算、截了出 `LLM-000` | `judge: the MCP excerpt leads with command, args, env, url and headers, caps each line at 500 bytes and says when it was cut (P-005)` |
| 15 | 评审 7:llm-judge、spec、architecture 三对文档按实际保证改写 | `docs: llm-judge, spec and architecture pairs say both homes are replaced, after redaction, and nothing beyond them (P-005)` |
| 16 | 复审 1:MCP 行上限那条测试切在 3 字节字符中间,删掉 rune 回退就红 | `judge: the MCP line-cap test cuts inside a 3-byte character, so deleting the rune walk-back turns it red (P-005)` |
| 17 | 复审 2:只丢行、一行没截的 MCP 摘录单独测 `LLM-000` | `judge: an MCP excerpt that drops lines without capping one is disclosed by a test of its own, so losing that half of the note turns it red (P-005)` |
| 18 | 复审 2:lead 键按恰好相等认,`command.x` 一类顶层键排在其余键里 | `judge: a top-level MCP key that only starts with command, args or url is pinned to rank with the rest, so matching lead keys by prefix turns a test red (P-005)` |
| 19 | 复审 3:截断补齐扩到编码写法,用户名起点按编码后的偏移算;spec 同步 | `judge: a static snippet the cap cut inside the encoded home is completed like the raw one, so projects/-Users-alic… no longer goes out with the username's head (P-005)` |
| 20 | 复审 4:llm-judge、architecture 两对和 spec 写明"取绝对路径并规整"与"一段式家目录不换编码写法";`newEgress` 注释同步,补一条钉住规整形态的测试 | `docs, judge: llm-judge, architecture and spec say a home is replaced made absolute and cleaned, not as given, and a one-segment home such as /root keeps its encoded form (P-005)` |
| 21 | 本文件、索引 | `proposals: P-005 (P-005)` |

## 未决问题

1. **`DB_PASS` 不在 `Redact` 的凭据键名表里**(`credKeys` 只有 `password|passwd|…`,`e2e_test.go` 里那条老测试的注释原话就是
   "`pass` is not in the redaction key list")。按 `key=value` 渲染之后 `DB_PASS=hunter2` 仍然原样出去——渲染本身救不了它。怎么办?
   **建议**:只在判官的 MCP 摘录里,键名(最后一段)含 `pass` `pwd` `secret` `token` `key` `auth` `cred` `private` `cookie`,或某一段恰好是 `pw` 时,
   值换成 `<REDACTED>`,然后整段照常过 `Redact`。**不**把 `pass` 加进 `credKeys`:那张表跑在散文和代码上,`bypass=`、`compass:` 会被抹,
   而且它动的是静态输出(不做什么第一条)。MCP 配置里键名是结构不是散文,宽一点的表在这里没有误伤散文的问题;误伤的代价是模型少看一个值。
   triage 里的静态 snippet 不套这张表(那是静态视图,见不能说什么第四条)。
   **已决(2026-10-08)**:按建议。
2. **Downloads 那一路用哪个家目录?** 环境扫描的约定是 `filepath.Dir(root)`,而 Downloads 候选的 root 是候选本身,上一级是 `~/Downloads`。
   **建议**:`scanInbox` 用 `os.UserHomeDir()`(它本来就用它展开 `~/`);环境扫描保持 `filepath.Dir(root)`。
   **已决(2026-10-08)**:按建议。(评审 1 之后 `Run` 总是换 OS 用户的家目录,Downloads 的单独覆写随 W11 删掉,结论不变)
3. **静态 snippet 进 triage 前已经被 `Redact` 吃掉了家目录的前半截**(实测 `<REDACTED>.d/alicemarker/…`:带数字的临时路径一段被熵规则抹掉,
   在 `.` 处断开,用户名那半截留下),判官这边看不到完整的家目录。
   **建议**:精确补两种相邻形态——`<REDACTED>` 紧跟家目录在某个断点字符(不在熵字符类里的字符,如 `.`)之后的后缀,换成 `~`;
   家目录到某个断点字符为止的前缀紧跟 `<REDACTED>`,换成 `~/<REDACTED>`。不做模糊匹配。
   **已决(2026-10-08)**:按建议。
4. **`detect.ConfigStrings` 导出的理由("让判官发同一份视图")不再成立,留着还是降为包内?**
   **建议**:降为 `configStrings`。判官改用 `ConfigLines`,两者共用同一个读条目的函数,`TestConfigLines_SameLeavesAsConfigStrings` 钉住叶子一致。
   **已决(2026-10-08)**:按建议。
5. **数组怎么渲染:每个元素一行 `args=-y`,还是拼成一行?**
   **建议**:每个元素一行。和静态视图同一颗粒度(一条字符串叶子一行),模型引一个元素也能落地。
   **已决(2026-10-08)**:按建议。
6. **一段式家目录(`/root`)的编码形态 `-root` 也换吗?**
   **建议**:不换。`-root` 太像一个命令行选项(`tool -root-dir x`),换了会改坏被审的命令;原样路径 `/root/…` 照换。
   **已决(2026-10-08)**:按建议。
7. **(实现中追加)静态 snippet 的 200 字节截断也会把家目录切成半截。** 对着 e2e fixture 看实际请求体时发现:一条 `HOOK-001`
   的 snippet 结尾是 `-d @/…/home…`——`detect` 的 `clip` 在 200 字节处切断、补一个 `…`,切点落在哪里不看内容;长 hook command 上
   这个切点完全可能落在用户名中间(`/Users/alic…`)。未决 3 只补了熵规则的两种切法。
   **建议**:同样精确地补:只看**以 `…` 结尾**的文本(截断标记只会出现在 snippet 末尾),家目录的某个前缀(或熵规则吃头之后的某段尾巴)
   紧跟 `…` 且**已经伸进用户名那一段**时换成 `~…`;只到上级目录为止的 `/Users/…` 不算,它不指向任何人,换了是在猜。不越过「不做什么」,
   不改任何用户可见的契约(报告字节不变),是未决 3 同一件事的第三种切法。
   **已决(2026-10-08)**:按建议。
8. **真机扫描(记录,不是问题)**:`detect` 有改动(`ConfigLines`、`configEntry`、`configStrings`),按移植约定在本仓库
   `make build && ./bin/aguard scan --root ~/.claude --quiet` 前后各跑一次(只读,不带 `--llm`),只记摘要行,数字在「完成」。
9. **(评审修复中追加)两个家目录嵌套时换哪个?** 评审 1 要求总是同时换 OS 用户家目录和 `--root` 上一级;`CLAUDE_CONFIG_DIR=~/.config/claude`
   时后者是前者的子目录,两个都按"最长优先"换,`~/.config/claude/x` 就成了 `~/claude/x`——判官会把它读成另一个位置。
   **建议**:按顺序处理(OS 用户的在前),后一个家目录的某种写法落在前一个之内就丢掉那种写法,外层的替换已经覆盖它;**只在这个方向**:
   被扫环境的家目录**包含**用户家目录时(`--root /Users/.claude`)两个都留,否则 `/Users/alice/x` 会成 `~/alice/x`。
   `TestEgress_LaterHomeInsideAnEarlierIsDropped` 钉住两个方向。**按建议实现;移植时按"未决按建议"处理,待人在本 PR 上确认。**
10. **(评审修复中追加)评审 6 的字面修法和评审 3 冲突。** 评审 6 说"`repairHalves` 只用于静态 snippet,原始内容的构造器不做";
    但评审 3 把原始内容改成先 `Redact` 再替换之后,原始内容也会出现熵规则吃头/吃尾的形态(e2e fixture 的临时家目录 `<REDACTED>.d/alicemarker`),
    不补就把用户名发出去——e2e 会红,而且红得对。**建议**:把 `repairHalves` 拆成两半,熵规则那两种切法(凡是先过 `Redact` 的文本都会有)
    所有路径都补;200 字节截断那一种(只有 detect 的 `clip` 会产生)只给 `egress.snippet`,即 triage 与串通摘要。这是评审 6 的意图
    (不在没被截过的文本上按截断去猜)在评审 3 之后的正确形态。**按建议实现;移植时按"未决按建议"处理,待人在本 PR 上确认。**
11. **(评审修复中追加)MCP 的 500 字节上限按值还是按行?** 评审 4 写的是"每个值"。键名同样是配置作者可控的,一个 6 KB 的键名照样能占满预算。
    **建议**:按行(`key=value` 整行)截,值和键一起管住;rune 边界,标记 ` … (N bytes omitted)`。**按建议实现;移植时按"未决按建议"处理,待人在本 PR 上确认。**
12. **(评审修复中追加)lead 键怎么认?** `ConfigLines` 用 `.` 拼路径,顶层键 `command.x` 和 `command` 下的嵌套键分不开;按前缀认的话,
    一串 `command.a…` 顶层填充键会排进 command 组、把 env 挤出去。**建议**:`command`、`args`、`url` 是字符串或字符串数组,只认键名**恰好**相等;
    `env`、`headers` 是对象,认 `env.` / `headers.` 前缀——而 `ConfigLines` 的排序让真正的 `env` 对象的行总排在 `env.<任何>` 顶层键之前。
    不改 `detect.ConfigLines`(那样要动 `detect`、要跑真机)。**按建议实现;移植时按"未决按建议"处理,待人在本 PR 上确认。**
    复审 2 起由 `TestPlan_MCPLeadKeysMatchExactly` 钉住:command、args、url 改成按前缀认,它就红。
13. **(移植时追加,记录,不是问题)与并行的 P-001 / P-003 的交叠。** P-001(`p/001-judge-usage-in-json`)也改 `cmd/aguard/main.go` 的 `runJudge` 函数体、`internal/judge/run.go` 和 llm-judge 对子,
    本条给它加了一个 `home` 参数;P-003(`p/003-zero-dial-test`)给 `openai.go` 的 `NewHTTP` 加了一个测试接缝,并用 `scanOpts{…}` 具名字段调
    `scanEnv` / `scanInbox` / `checkTarget`。本条不碰 `NewHTTP`、只给 `scanOpts` 加一个具名字段,和 P-003 没有语义冲突。
    谁后合谁 rebase:`runJudge` 那一处按两边的意图合;若 P-003 的零外连源码检查对 `NewHTTP` 里客户端字面量的形状有要求,由后合的一方适配。
