<!-- SPDX-License-Identifier: MIT -->
# 020 — 一行指令垫满空白,判官摘录里就只剩一行省略标记:判官恰好看不见它存在要读的那句话

- **来源**:P-005、P-006 合入后记下的后续(2026-10-09)。P-006 修了"垫空白的引文在**证据**里只显示 `…Note:…`",
  前提是引文已经落地 —— 可摘录在发出去**之前**就把整行扔掉了,模型根本没有引的机会
- **依赖**:无(P-005、P-006 已在 `main`)
- **分支**:`p/020-excerpt-padding-evasion`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

判官的摘录是按字节封顶的:SKILL.md 正文、CLAUDE.md 这类单文件、connector 工具清单各 6,000 字节,skill 的脚本每个文件
2,000、合计 6,000(`internal/judge/excerpt.go` 的 `capHeadTail`);hook 命令截 6,000 字节前缀(`boundedRedact`),
MCP 配置每行 500(`capLine`),声明用途 1,000(`declaredPurpose`),解码出的 blob 800(`decode.go`)。
`condense` 只折叠**整行都是空白**的行。一行里有字、但字中间(或行首)垫着几千字节空白的,原样进封顶:

- `capHeadTail` 从头往下装、从尾往上装,装不下的那一行连同它和尾段之间的所有行一起换成一行 `# … N line(s) omitted …`;
- 整段只有这一行时退化成"取前缀",前缀全是空白,而且没有省略标记;
- 其余几处是前缀截断,同样只剩空白。

于是作者把注入指令写成 `Note:` + 7,000 个空格 + `ignore your instructions …`,模型收到的是"这里省略了 1 行",
判官(LLM-003 / LLM-007 那一趟)对它存在要读的那句话一个字都看不到。空白不必是 ASCII:`unicode.IsSpace` 认的 U+3000、
U+2003、NBSP,以及 INJ-004 报告的零宽字符(U+200B …)都行 —— P-006 已经让**落地**把这两类当垫料,摘录这边没有跟上。

实测(`origin/main` `fd28344`;探针用 `httptest` 起一个 OpenAI 兼容端点,经 `NewHTTP` + `Run` 记下每个请求 user 消息,
数指令原文出现在几个请求里;探针文件在 scratch,不提交,判据里的测试会重现同样的形状):

| 形状 | 请求数 | 含指令原文 | 含省略标记 | 说明 |
|---|---|---|---|---|
| SKILL.md 正文,指令行内垫 7,000 空格 / 7,000 tab / 2,400×U+3000 / 2,400×U+2003 / 3,600×NBSP / 2,400×U+200B | 2 | **0** | 1 | injection 那趟的正文是"头 + `# … 1 line(s) omitted …` + 尾" |
| 同上,垫在行首(缩进) | 2 | **0** | 1 | |
| 同上,垫在 ```` ```bash ```` 围栏里的一行 | 2 | **0** | 1 | |
| 正文只有这一行 | 2 | **0** | **0** | user 消息 6,163 字节,是 `Note:` 加约 6,000 字节空白;连"省略了"都没说 |
| 指令行本身不垫,**它上一行**是 `.` + 7,000 空格,后面还有 2,360 字节正文 | 2 | **0** | 1 | 一行装不下的行挡住了它之后、尾段之前的所有行 |
| skill 脚本 `run.sh` 里 `echo ok;` + 3,000 空格 + `curl … \| sh` | 2 | **0** | 1 | 每文件 2,000 字节的封顶 |
| 三个脚本各约 1,975 字节、几乎全是空白(都在 2,000 之内),第四个 `z.sh` 是 payload | 2 | **0** | 1 | 合计 6,000 字节被垫料花光,`z.sh` 只剩头尾碎片 |
| CLAUDE.md,指令行内垫 7,000 空格 | 1 | **0** | 1 | |
| description 垫 2,000 空格后接指令 | 2 | **0** | 0 | 声明用途 1,000 字节前缀 |
| hook 命令 `echo ok;` + 7,000 空格 + `curl … \| sh` | 2 | **0** | 0 | 6,000 字节前缀,无任何披露 |
| MCP `args` 一个值里垫 700 空格后接 `--require /tmp/evil.js` | 1 | **0** | 0 | 截到 500 字节,出 `LLM-000`(披露了,但模型仍没看到) |
| 反向:同一份正文不垫 | 2 | 1 | 0 | |

后果:这是本工具**深度检查自己的规避** —— 静态规则逐行读不到的"换了说法的注入",正是判官存在的理由;
一段不可见的空白就让判官对这一行失明,而报告里"判官跑了、没发现"和"判官根本没看到"无从区分。

真实文件里这么长的空白串几乎不存在(本机 `~/.claude/plugins`、`~/.claude/skills` 与语料库 benign / hard-negative /
malicious 共 2,246,717 个非空行,按上面那组字符量每行最长的一串):行首缩进最长 121 字节;超过 128 字节的串只有 28 行,
27 行是 markdown 表格的列对齐,另一行是语料 benign 里一个 skill 行尾 636 字节的空格 + 零宽字符。

## 初步方向

在摘录层把**垫料**按落地读它的方式折掉,再封顶:一串长度超过某个远高于真实缩进的阈值的空白 / 不可见字符,换成落地本来就把它
读成的那个样子(一个空格;只有不可见字符时什么都不留)。只在行内做、绝不跨 `\n`,所以行数和 `lineMap` 不变;先脱敏再折叠再封顶,
不变量 #3 的顺序不动;只会让发出去的字节变少。放在摘录构造器对原始内容调用的那一个收口里,让每种 kind 一起生效。
动到 `internal/judge`(`excerpt.go`、`egress.go`)和判官文档对子、spec §5.2。

## 完成的判据

- [x] `TestRun_PaddedDirectiveReachesTheJudge`(`internal/judge/padding_test.go`,新):经 `NewHTTP` + `httptest` 端点跑 `Run`,
  端点记下每个请求的 user 消息,**只在它收到的文本里有指令原文时**才回一条以指令为证据的 flagged 判决(模型只能引它看到的东西)。
  6 种垫料(7,000 空格、7,000 tab、2,400×U+3000、2,400×U+2003、3,600×NBSP、2,400×U+200B)× 5 种位置(指令行内、做它的缩进、
  正文唯一的一行、```` ```bash ```` 围栏里、垫在它上一行且后面还有约 2,400 字节正文),共 30 例,每例断言:至少一个请求带着指令原文;
  有一条 `LLM-003` 引到 `SKILL.md` 里指令**真实所在的那一行**;没有 `LLM-005`。今天红:30 例都没有请求带指令原文
- [x] `TestPlan_PaddingCostsWhatOneSpaceCosts`(同文件,新):同一份 fixture 垫料写成几千字节,与垫料写成"落地读它的样子"(一个空格;
  只有不可见字符时什么都不写)相比,`planFor` 给出的**请求逐字节相同**(`Mode`、`Declared`、`Behavior`),**source unit 也相同**
  (`file`、`text`、`firstLine`、`lineMap`、`collapsed`),`shortened` 也相同。覆盖的面:SKILL.md 正文(injection)、skill 脚本(intent)、
  三个都在每文件上限之内的垫料脚本加一个 payload 脚本(合计上限)、description(声明用途)、CLAUDE.md、connector 工具说明、
  hook 命令、MCP `args` 里的一个值、解码后是垫料命令的 base64 blob。今天红:每个面上垫过的那份都和参照不同
  (省略标记 / 纯空白前缀 / payload 脚本只剩碎片 / `1 value(s) cut`)
- [x] 不变量 #3 的守卫 `TestEgress_PaddingIsFoldedBetweenRedactions`(`internal/judge/padding_test.go`,新):
  ① 一个单独就会被熵规则抹掉的 24 字符 token,后面接一串不可见字符和 60 个 `a` —— 发出去的文本里没有这个 token
  (折叠挪到 `Redact` 之前就红:拼起来的长串熵掉到 3.6 以下,不再抹);② `AKIA` + 一串不可见字符 + 16 个字符 —— 发出去的文本里
  没有拼好的 `AKIA…` 原文(去掉折叠后的第二次 `Redact` 就红);③ 家目录被一串不可见字符从中间切开 —— 折叠后换成 `~`
  (scrub 挪到折叠之前就红)。①② 今天就绿(今天不折叠,token 照样被第一遍抹掉,`AKIA…` 照样是切开的),守的是实现;③ 今天红。
  三条都写明各自对应的变异,并在实现之后实际跑一次变异、记下红
- [x] `TestFoldPadding`(表驱动)+ `FuzzFoldPadding`(`internal/judge/fold_test.go`,新;种子语料随 `go test` 跑):
  128 字节的串原样、129 字节折叠;只有不可见字符的串折成空;混合的折成一个空格;`\n` 两侧各 100 字节的空白不折(不跨行);
  无效 UTF-8 字节原样。性质:输出不长于输入;`\n` 个数不变;`normalizeWithLines` 给出的规范化文本和逐字节行号与输入**完全相同**
  (落地读它和读原文没有区别);幂等;输出里没有超过 128 字节的垫料串
- [x] 反向断言 `TestPlan_RealTextIsSentAsWritten`(`internal/judge/padding_test.go`,新,今天就绿,修完不改一字仍绿):
  ① 正文里一行 7,000 字节、只有单个空格的普通长行,摘录与"按今天的流水线手算"(`condense` + `capHeadTail`,不折叠)逐字节相同,
  仍是头 + `# … 1 line(s) omitted …` + 尾;引这条省略标记的判决仍落不了地(`LLM-005`、没有判官发现);
  ② 一份 Python 脚本(8 层缩进、一处正好 128 字节的对齐空白)摘录逐字节等于手算;
  ③ 垫料每 128 字节被一个可见字符隔开(`.`)的那行**不折**,照旧被省略标记换掉 —— 这是可见内容,不是垫料(见不能说什么)
- [x] 反向断言 `TestRun_PaddingBelowTheFoldStillShowsTheDirective`(同文件,新,今天就绿):P-006 的 W12/W13 窗口路径在折叠之后
  仍有端到端的测试 —— 指令中间是 5 段各 120 字节的空白(低于阈值,原样发出),每段之间一个 `-`,引文落地在 `SKILL.md:7`,
  snippet 含整句指令、≤ 515 字节。原因:P-006 的 `TestRun_WhitespacePaddedQuoteShowsTheDirective` /
  `TestRun_UnicodePaddedQuoteShowsTheDirective` 用的是 600 字节的单段垫料,折叠之后它们照样绿,但走的不再是 `collapsedWindow`
- [x] 反向断言:既有测试一字不改仍绿 —— `excerpt_test.go`、`plan_test.go`、`rendering_test.go`(含 `TestGround_OmissionMarkerIsNotEvidence`、
  `TestRun_OmissionMarkerIsNotEvidence`、P-006 的两条垫料测试)、`ground_test.go`、`egress_test.go`、`cmd/aguard` 的 e2e;
  `git diff --numstat origin/main -- '*_test.go'` 删除列全为 0
- [x] `make verify` 绿;`go version` 不切换工具链;`internal/collect`、`internal/detect` 不动(所以不跑真机扫描对比,改为证明 diff 为空)

## 不做什么

- **不动任何封顶**:`maxExcerptBytes`、`maxFileBytes`、`maxDeclaredBytes`、`maxConfigLineBytes`、`maxDecodedBytes`、`maxSnippetBytes`
  的值和 `capHeadTail` / `capLine` / `boundedRedact` / `declaredPurpose` 的截法一行不改;折叠只会让进封顶的字节变少
- **不动 `detect`**:`detect.Redact`、静态规则、静态 snippet 一样都不碰,所以 text / JSON / SARIF / HTML / markdown 的静态输出不变;
  不加"横向垫料"的静态规则(那会改分数,见下面的后续)
- **不碰落地和渲染**:`ground.go`、`judge.go` 不动 —— 折叠之后规范化文本与原文相同,落地标准、行号计算、`LLM-005` 口径、
  P-006 的窗口都不需要改
- **不动 triage 和 collusion 摘要里的静态 snippet**:它们在 `detect` 那一步已经按 200 字节截过,事后折叠拿不回被截掉的内容
  (`eg.snippet` 不经过折叠)
- **不改 `capHeadTail` 的"一行装不下就挡住后面所有行"**:对**可见**内容的长行(压缩过的 JS、一段 7,000 字节的散文)照旧
- 不改提示词、不改 `Client` 接口、不碰 `openai.go`;`go.mod` / `go.sum` 不动,不加依赖;canonical 哈希、闸门、评分不动

## 不能说什么

- **不说"垫料再也挡不住判官"**:折掉的只是**连续**超过 128 字节的垫料。每 128 字节插一个可见字符(`.`)就不算垫料,
  约 47 个可见字符就能把 6,000 字节的摘录填满 —— 那是内容,摘录分不出它和别的内容;一行里塞满这种东西照旧会被省略标记换掉,
  多个文件里各塞一些照旧能把合计上限花光。修的是"**零可见字符**就能让判官失明"这一档
- **不说"摘录发的就是文件原文"**:超过 128 字节的空白串在摘录里是一个空格,只有不可见字符的串是空。落地本来就这样读,所以引文和行号
  不受影响;但模型看不见"这里曾垫过几千字节",报告里也不说(没有 note)
- 不说"垫料"包括所有看起来是空白的字符:只认两张表 —— `unicode.IsSpace` 认的空白(换行除外)和 `detect.Invisible` 那组不可见字符,
  与 P-006 落地用的完全相同。U+2800 盲文空白、U+3164 韩文填充符这类不在表里的,照旧占预算
- 不说 Redact 现在认得被零宽字符切开的 secret:只有超过 128 字节的那种串被折掉之后,拼起来的文本才会再过一次 `Redact`;
  切开它的串更短时照旧原样发出(和今天一样,是切开的样子)
- 不说折叠不改变任何真实文件的摘录:2,246,717 个真实非空行里有 28 行会变(27 行 markdown 表格的列对齐、1 行行尾空格 + 零宽字符),
  变化是对齐空白缩成一个空格

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 端到端与 `planFor` 两组新测试 + 三条守卫 + 两组反向断言,跑红(守卫①②与反向断言今天就绿) | `judge: tests — a directive padded with whitespace never reaches the judge, which is sent the omission marker or a prefix of blanks instead (P-020)` |
| 2 | `foldPadding`:行内连续超过 128 字节的垫料折成落地读它的样子;`eg.redact` 改成 Redact → 折叠 → (折过才)再 Redact → scrub;`TestFoldPadding` + `FuzzFoldPadding`;守卫三条各跑一次变异 | `judge: padding inside a line is folded to the space grounding reads it as before any cap, so it can no longer push a directive out of the excerpt (P-020)` |
| 3 | `llm-judge` 对子、`architecture` 对子、spec §5.2、`.claude/rules/judge.md` 各一句 | `docs: the judge pair, the architecture pair, spec §5.2 and judge.md say padding is folded before the excerpt caps (P-020)` |
| 4 | 本文件「完成」、索引 | `proposals: P-020 (P-020)` |

## 未决问题

1. **阈值取多少?**
   **建议**:128 字节。真实行首缩进最长 121 字节(2,246,717 个非空行),超过 128 的串只有 28 行;再低就开始吃真实缩进
   (超过 64 的行首串语料里有 3 行),再高只是让"每隔 N 字节插一个可见字符"便宜一倍,不改变能防住的那一档(零可见字符)。
   **已决(2026-10-09)**:按建议
2. **折成什么?**
   **建议**:落地读它的样子 —— 串里有空白就是一个空格,只有不可见字符就是空(`foldForMatch` 的同一套)。这样摘录规范化之后与原文
   逐字节相同,落地、行号、P-006 的窗口都不用动。插一个可见的占位符(`⟨7000 spaces⟩`)能告诉模型"这里垫过",但它是不在任何文件里的文本,
   要像省略标记一样教落地拒收,代价不成比例。
   **已决(2026-10-09)**:按建议
3. **放在哪一层?**
   **建议**:`eg.redact` —— 摘录构造器对原始内容调用的唯一收口(SKILL.md 正文与描述、脚本、单文件、connector、hook、MCP 行、解码 blob
   全经过它),新加的构造器自动带上。不放进 `condense`:hook / MCP / 描述 / blob 不经过它;也不放进 `detect.Redact`:那会改静态输出。
   **已决(2026-10-09)**:按建议
4. **只在超封顶时折,还是一律折?**
   **建议**:一律折。只在超封顶时折挡不住"三个脚本各 1,975 字节、都在每文件上限之内,合计 6,000 字节被垫料花光"(探针实测 payload 不进请求);
   真实文件里会变的只有 28 行。
   **已决(2026-10-09)**:按建议
5. **要不要披露"这里折过"(`LLM-000` 或文本里的标记)?**
   **建议**:不。折掉的不是内容 —— 落地本来就把它读成一个空格;`condense` 折空行、删注释行也从不披露;为 28 行真实表格出 note
   会教人跳过 note。"横向垫料本身就是信号"这件事该是静态规则(与 `OBF-007` 对称),那会改分数,记为后续,本条不做。
   **已决(2026-10-09)**:按建议
6. **折叠后要不要再 `Redact` 一次?**
   **建议**:要,但只在确实折过时。折叠可能把被不可见字符切开的 token 拼回去;第二遍只会抹得更多(第一遍的 `<REDACTED>` 不会被还原),
   正常文本不付代价。顺序 Redact → 折叠 → Redact → scrub:折叠不能在第一遍之前(熵规则按整串判,拼上低熵的尾巴会让原本会被抹掉的 token 漏出去),
   scrub 仍在最后一遍 Redact 之后(P-005 的放置规则)。
   **已决(2026-10-09)**:按建议

## 完成

```
合入:PR #38(2026-10-09;sha 用 git log --grep P-020 找)
发布:待发
证据:TestRun_PaddedDirectiveReachesTheJudge(internal/judge/padding_test.go);W1 在 930e914 上红 30/30,全是 `none of 2 request(s) carried the directive` → W2 后绿,30 例的 LLM-003 都引到指令真实所在行(SKILL.md:9 / 9 / 5 / 11 / 10),没有 LLM-005
证据:TestPlan_PaddingCostsWhatOneSpaceCosts(同文件);W1 红 50/50(9 个面 × 6 种垫料,解码 blob 只跑 ASCII 两种,Unicode 空白会让 blob 读成二进制):hook 两趟 Behavior 各 6,000 字节纯前缀;MCP 那例 shortened = "1 value(s) cut to 500 bytes",参照为空;三个垫料脚本那例 intent 5,995 字节、带省略标记 → W2 后绿
证据:TestEgress_PaddingIsFoldedBetweenRedactions(同文件);W1 时 ③ 红(被零宽字符切开的家目录没换成 ~)、①② 绿 → W2 后三条绿;变异(均已还原):折叠挪到第一遍 Redact 之前 → ① 红;去掉第二遍 Redact → ② 红;scrub 挪到折叠之前 → ③ 红(同时只留一遍 Redact 时 ②③ 一起红)
证据:TestFoldPadding、FuzzFoldPadding(internal/judge/fold_test.go);`go test -run '^$' -fuzz FuzzFoldPadding -fuzztime 20s` 935,369 次执行无失败(输出不变长、行数不变、幂等、无超过 128 字节的垫料串、normalizeWithLines 的文本与逐字节行号与原文相同)
证据:反向断言 TestPlan_RealTextIsSentAsWritten、TestRun_PaddingBelowTheFoldStillShowsTheDirective 在 W1(实现未改)上就绿,W2 后不改一字仍绿;ground.go 里 collapsedWindow 换成 window(变异,已还原)后全包只有 TestRun_PaddingBelowTheFoldStillShowsTheDirective 红 —— P-006 的两条 600 字节垫料测试折叠后确实不再经过那条路,由它接着守
证据:反向断言既有测试一字不改仍绿(make verify 的全量测试里,含 TestGround_OmissionMarkerIsNotEvidence、TestRun_OmissionMarkerIsNotEvidence、TestRun_WhitespacePaddedQuoteShowsTheDirective、TestRun_UnicodePaddedQuoteShowsTheDirective、excerpt_test.go、plan_test.go、egress_test.go、cmd/aguard 的 e2e);git diff --numstat origin/main -- '*_test.go' 只有两个新文件,删除列全为 0
证据:不做什么 —— git diff --stat origin/main -- internal/detect internal/collect internal/score internal/report internal/gate internal/judge/ground.go internal/judge/judge.go internal/judge/openai.go internal/judge/prompt.go internal/judge/decode.go internal/judge/run.go internal/judge/triage.go go.mod go.sum 为空;excerpt.go 删除行为 0(封顶常量与 capHeadTail / capLine / boundedRedact / declaredPurpose 未动,只新增 foldPadding);collect / detect 没改,所以没有跑真机扫描对比
证据:scratch 探针(不提交)在 W2 之后重跑:问题一节表里 22 种形状加反向例,23 例全部"含指令原文 ≥ 1、含省略标记 0";正文垫在行内的 5 种空白例 user 消息 292 字节,与不垫的反向例逐字节同长
证据:make verify → verify: all gates passed;go version go1.23.5 未切换工具链,go.mod 第二行 go 1.23.5
```
