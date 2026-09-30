<!-- SPDX-License-Identifier: MIT -->
---
paths:
  - "internal/report/**"
---
## 终端报告的两种模式(`internal/report/text.go`)

`Text` 是默认档,`TextVerbose` 是完整档,两者都走 `writeText`。**它们只差两样东西,而且都不是发现:**
维度 0 的覆盖度说明折成一行 / 展开成一段,以及默认档多一行判定摘要(两档都有)。

- **发现在任何一档都不折叠。** 只有维度 0 的元信息会折。把一条发现藏到参数后面,等于让安全工具的
  **默认**视图成为少报的那一个,而一条从没被展示过的发现,读者不可能想到去追问它的细节。
  `TestText_FindingsSurviveBothModes` 和 `TestWriteReport_DefaultStillShowsTheDangerousThing`
  两头盯着这条(后者用的是真实流水线的输出,不是手搭的 `ScanResult`)。
- **折叠后的那一行必须带着最高严重度**,不是只带条数 —— 否则一条压掉了 critical 的
  `IGN-000` 会被读成低危脚注,那正是不变量 #5 要防的事。`TestText_CollapsedNoteCarriesHighestSeverity`
  盯着它。
- **判定摘要(`writeVerdict`)只能从它下面那张表里推**:条数、严重度、最差那条的规则 ID,全部来自
  已经要打印的 `Group`。**不要在这里引入新的判断或新的阈值** —— 一行会和它下面的列表打架的摘要,
  比没有摘要更糟,因为它是大家真正会读的那半。
- **`--verbose` 是 rootCmd 上的 persistent flag,和 `--quiet` 成对。** 它**只**加宽终端报告:
  `--json` / `--html` 两档内容完全一致,所以一个 CI 任务的输出不会取决于某个人加没加它。
- **报告有两个读者,阅读顺序为不写代码的那个而定**([plain.go](../../internal/report/plain.go) 是两个
  渲染器共用的白话层):分数 → 一句人话结论 → "先看什么"(最多 3 条,带位置)→ 全部发现 →
  白名单压制 → 没检查到的 → 最后才是审计员要的清单和 OWASP 覆盖声明(`writeScanDetails`)。
  以前清单和 OWASP 行排在最前面,普通用户读到第五行还没见到一句结论。
  - **白话层只做派生,不做判断。** `verdictSentence` 是等级档 + 计数的函数,`actions` 就是 medium
    以上的 group 按既有顺序取前三,`dimLabel` 是每个维度一句固定短语(10 句,不是 70 条规则各写一句)。
    没有任何一处能说出下面列表里没有的东西,也没有任何一处能把严重度说轻。
  - **规则 ID 仍在每条发现上,但挪到行尾的方括号里**;行首是"谁 · 什么"(`friendlyArtifact` 把
    `plugin:figma@claude-plugins-official (2.2.107)` 写成 "figma plugin, from claude-plugins-official (2.2.107)")。
    改这里时注意几个测试钉住的锚点:`Risk score N/100 (Level)`、`→ … start with [ID]`、
    `Findings · STATIC` / `Findings · LLM JUDGE`、`✅ No risk findings`、`coverage note(s), highest X [ID]`、
    `advisory: not confirmed`、`×N`。
  - **默认视图缩短证据路径**(`shortPath`:插件缓存前缀折成 `figma › rest`,过长路径留末三段),
    `--verbose`、JSON、SARIF、HTML 的 `title` 悬停保留全路径。定位靠短路径,复制粘贴靠全路径。
  - **markdown(`markdown.go`,`--md`,P-008)是第三个人读渲染器**,同一套派生数据、同一顺序,没有 `--verbose`。
    它多一条别处没有的规则,而且是这份报告存在于代码里而不是模板里的原因:**所有来自被扫目录的字符串只能出现在
    代码跨度里**(`code`/`cell`),包括发现的 Title 和 Why —— 静态规则会把文件名格式化进 Why(`shape.go`),
    判官的 Title/Why 是模型写的。它会被贴进公开的 PR:裸的 `@name` 会 @ 人,`[x](url)` 是链接,`<img>` 是追踪
    像素,`|` 拆表格。`<summary>` 行在 HTML 块里,backtick 不起作用,所以只放规则 ID 且 `html.EscapeString`。
    页尾不能用 `[text](url)` 形式的链接:`TestMarkdown_AttackerTextIsInert` 断言代码跨度外没有 `](`,裸 URL
    GitHub 一样自动成链。要在 markdown 里用到白话层的某一句而那句里嵌着磁盘来的字符串时,**把 plain.go 里那句
    拆成"数据 + 组句"两半**(`worstItem`/`trustName`/`escalations`/`where` 就是这么来的),不要在 markdown 里
    重写推导。
  - HTML 同一套数据:顶部 Summary 卡、"What to look at" 带锚点跳到发现卡、白名单压制单独一区、
    没检查到的折进 `<details>`、清单放最后;跟随系统深浅色。JSON/SARIF 不受任何一条影响。
  - **Scan details 里有 "Where the scan looked"**(`ScanResult.Locations`,2026-09-07):配置目录、用户级 MCP 配置、
    桌面版仓库、下载目录,各标 read / absent / off。清单数字分不出"没装桌面版"和"桌面版仓库读了但是空的",只有
    这张表分得出;`--inbox off` 显示 off 而不是消失,用户关掉的东西也要留痕。`check` 不填它。

