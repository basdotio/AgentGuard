<!-- SPDX-License-Identifier: MIT -->
# 024 — frontmatter 前面有空行或 BOM 的规则文件,报告标成 (path-scoped),Claude Code 却每次会话都加载它

- **来源**:P-015 未决 5(`docs/proposals/complete/015-rules-frontmatter-first.md`)。2026-10-09 人批准另开一份,先量 `SKILL.md`
- **依赖**:无
- **分支**:`p/024-frontmatter-leading-bytes`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

`internal/parse/frontmatter.go` 的 `splitFrontmatter` 在找开头的 `---` 之前,先去掉 UTF-8 BOM 和开头的空白(空格、制表符、
空行)。`collectRules` 用它判断一份 `rules/**/*.md` 有没有 `paths:`,有就在 artifact 名后面加 ` (path-scoped)`。

P-015 在 Claude Code 2.1.107 上量过:`---` 之前有一个空行或一个 BOM 的规则文件,`paths:` 被忽略,每次会话开头都加载。
于是这两种形状 aguard 标成 `(path-scoped)`,而 agent 实际每次会话都读它。这个名字回答的正是"它占多少上下文"——
`collectRules` 的注释写着"每次会话都加载"和"打开匹配文件时才加载"值得读者不同的反应——报告在这一点上说少了。

同一个函数也给 `SKILL.md`(以及子 agent、斜杠命令)读 `name`/`description`。Claude Code 对 `---` 前有 BOM 或空行的
`SKILL.md` 认不认 frontmatter,P-015 没有量:如果它不认,aguard 拿来算 context_bloat、重复描述、判官"声明的用途"的那段
描述,就不是 agent 实际看到的那段。

### 实测(Claude Code 2.1.107,本机,2026-10-09)

方法沿用 P-015,外加一个只听回环地址的抓包服务器。隔离的 `CLAUDE_CONFIG_DIR`(用户配置不碰),`env -i` 起一个干净环境,
在一个现搭的项目目录里跑 `claude -p "hi"`:

- **规则**:用户级 settings 里注册 `InstructionsLoaded` hook,记下每次加载的 `file_path` 和 `load_reason`。
  规则同时放在用户级 `<config>/rules/` 和项目级 `.claude/rules/`,每份的 `paths:` 都指向不存在的目录。
- **`SKILL.md`、斜杠命令、子 agent**:`ANTHROPIC_BASE_URL` 指向 `127.0.0.1` 上的抓包服务器(`ANTHROPIC_API_KEY` 是一个
  占位串,不是任何真实凭据;服务器记下请求体后回 400,CLI 随即退出)。CLI 发给模型的那份请求里,第一条用户消息带着
  skill 清单(`- <名字>: <描述>`),`Agent` 工具的说明带着子 agent 清单——这就是模型看到的东西,不经过模型。

**前导字节**(frontmatter 本身合法;规则两级结果相同):

| `---` 之前有什么 | 规则:`session_start` 加载? | `SKILL.md`:清单里的描述 | 命令:清单里的描述 | 子 agent:进清单? | aguard fd28344 |
|---|---|---|---|---|---|
| 没有,`---` 在第 1 字节(LF) | 否 | frontmatter 的 description | frontmatter 的 description | 是 | 读 frontmatter,规则标 `(path-scoped)` —— 一致 |
| 没有,CRLF 换行 | 否 | frontmatter 的 description | —(未量) | —(未量) | 一致 |
| 一个空行 | **是** | `---` | `---` | **否** | 读 frontmatter,标 `(path-scoped)` —— **不一致** |
| UTF-8 BOM | **是** | `---` | `---` | **否** | 同上 —— **不一致** |
| 一行空格 | **是** | `---` | —(未量) | —(未量) | 同上 —— **不一致** |
| 一行 HTML 注释 | 是 | 那行注释 | 那行注释 | 否 | 不读 frontmatter —— 一致 |
| 一行 `# 标题` | 是 | `Title`(标题文字) | —(未量) | —(未量) | 不读 frontmatter —— 一致 |

skill 在七种形状下都进清单,名字一律是目录名;frontmatter 不被认时,Claude Code 拿正文第一行非空文本当描述。

**`paths:` 的值**(frontmatter 在第 1 字节,用户级规则):

| `paths:` | `session_start` 加载? | aguard fd28344 |
|---|---|---|
| 列表 `- "no-such-d/**"`、标量串、`"a/**, b/**"`、嵌套列表、`"no-such-{e,f}/**"` | 否 | `(path-scoped)` —— 一致 |
| `paths:`(空值) | 是 | 不标 —— 一致 |
| `[]`、`""`、`- ""`、`- "**"`、`- "**/**"`、`"/**"`、`"**, **"`、`"{**,**}"`、`5`、映射 | **是** | `(path-scoped)` —— **不一致** |
| 不加引号的 `**/no-such-g/*.ts`(YAML 解析不了) | 否 | 不标 —— 方向相反(报告多算了上下文),见未决 6 |

**源码对照**(`strings` 读出的内嵌 JS):规则、skill、命令、子 agent、output style、memory 都经同一个函数,用
`/^---\s*\n([\s\S]*?)---\s*\n?/`(不带 `m` 标志)去匹配按 utf-8 读进来、**保留 BOM** 的文本;规则加载器再把 `paths`
按"深度 0 的逗号切开、去首尾空白、花括号展开、去掉结尾的 `/**`、丢掉空串",剩下的为空或全是 `**` 就当没有 `paths`。
上面两张表与这段代码逐条一致。

**aguard fd28344 在同样的目录上**:`aguard scan --json --inbox off` 把空行、BOM、空格三份规则和上表十种 `paths` 值都命名为
`(path-scoped)`。两个 BOM 开头、描述相同且超长的 skill 各出一条 `context_bloat`,两者之间还出一条 `duplicate_fn` ——
而 Claude Code 给它们列的描述都是 `---`。

## 初步方向

让 `parse` 判断 frontmatter 的方式跟实测一致:`---` 必须是文件的头三个字节;`(path-scoped)` 只在规则的 `paths:` 照
Claude Code 的整理办法还剩至少一条不是 `**` 的 glob 时才加。实测 `SKILL.md`、命令、子 agent 同样不认前导字节后面的
frontmatter,所以共用的解析器一起改,这几类 artifact 的 frontmatter 读法随之和 Claude Code 一致。哈希只看字节,不受影响。

## 完成的判据

fixture 都在 `t.TempDir()` 现搭,与现有测试同一写法。

- [x] `TestPathScoped_MatchesClaudeCode`(`internal/parse/frontmatter_test.go`,新):上面两张表里每一种规则形状一行
  (前导字节 7 行 + `paths` 值 16 行),期望值就是实测结果。在 fd28344 上红,红的**恰好是**表里标"不一致"的 13 行
- [x] 反向断言(同一测试):`---` 在第 1 字节的 LF、CRLF 两行,以及列表、标量串、逗号串、嵌套列表、真实花括号五种值,
  修前修后都是 `true`;HTML 注释、`# 标题`、空值三行修前修后都是 `false`
- [x] `TestCollectRules_PathScopedOnlyWhenClaudeCodeHonoursIt`(`internal/collect/loaded_test.go`,新):经 `CollectAll`,
  空行开头、BOM 开头、`paths: []` 的规则名字不带 `(path-scoped)`;第 1 行开始的那份仍带(反向)。原有
  `TestCollectRules_RecursiveAndPathScoped` 不改、照绿
- [x] `TestReadSkill_FrontmatterMustStartAtTheFirstByte`(`internal/parse/frontmatter_test.go`,新):BOM、空行、一行空格
  开头的 `SKILL.md` 读出 `Name == ""`、`Description == ""`,`Body` 含 frontmatter 那几行(Claude Code 调用 skill 时
  正文里就有它们),`BodyLine` 指向 `---` 那一行;反向:LF、CRLF 在第 1 字节的照旧读出 name 和 description
  (原有 `TestReadSkill` 不改、照绿)
- [x] `TestSplitFrontmatter` 的 `leading blank + bom` 一行改为期望"没有 frontmatter":旧期望钉住的正是这个出入
- [x] `TestContextBloat_OnlyForADescriptionClaudeCodeLists`(`internal/hygiene/hygiene_test.go`,新):BOM 开头、描述超长
  的 skill 不出 `context_bloat`;同样内容 `---` 在第 1 字节的照出(反向)
- [x] `TestPathScoped_BraceExpansionIsBounded`(新,带 deadline):40 组 `{,}` 在 2 秒内返回 `false`(每个展开都是空串),
  40 组 `{x,y}` 返回 `true`;回归的表现是挂死而不是断言失败,所以测试自己计时
- [x] 哈希不变:`TestHashGolden` 绿,`git diff --stat origin/main -- internal/collect/hash.go` 为空
- [x] 实测目录复扫:本分支构建的 `aguard scan --json --inbox off` 在上面三个实测目录上的 `(path-scoped)` 标签与实测表逐条一致
  (不加引号那一行除外,未决 6);两个 BOM skill 不再出 `context_bloat` 和 `duplicate_fn`,两个第 1 字节的照出
- [x] 真机:`make build && ./bin/aguard scan --root ~/.claude --quiet --json` 修前修后对比,差异逐条解释
- [x] `make verify` 绿;`go version` 不切换工具链,`go.mod` 第二行仍是 `go 1.23.5`,不加依赖

## 不做什么

- **不动 canonical 哈希**:`internal/collect/hash.go` 一个字节不改。哈希只看字节,这个改动不碰字节
- **不改任何检测规则、严重度、分数公式**:`internal/detect`、`internal/score` 不动。`parse` 不在计分路径上
  (只被采集的标签、hygiene、判官用),所以分数和 `--fail-on` 的答案不变
- **不模拟 Claude Code 的描述回退**:没有 frontmatter 时它拿正文第一行当描述,aguard 的 `Description` 照旧是空(未决 3)
- **不对齐闭合分隔符和开头那一行的尾巴**:Claude Code 在第一个出现的 `---` 处结束 frontmatter(哪怕在一行中间),开头允许
  `---` 后跟空白再换行;aguard 在第一个以 `---` 开头的行结束。这些形状没量,不在本条(未决 6)
- **不移植 Claude Code 的 YAML 修复**:它解析失败时会给 `key: value` 行的值补引号再解析一次,于是不加引号的
  `paths: **/x` 和带 `: ` 的 description 它读得出、aguard 读不出。方向是 aguard 读少了,不是本条的问题(未决 6)
- **不新增 note 或发现**来报"这份 frontmatter 被 Claude Code 忽略"(未决 5)
- **不改 `cmd/aguard/claude_rules_test.go`**:那是 P-015 给本仓自己的规则文件做的检查,它已经报这几种前导形状

## 不能说什么

- 不说"所有版本的 Claude Code 都这样":只量了本机 2.1.107
- 不说被标 `(path-scoped)` 的规则"会在打开匹配文件时加载":量的只是它们**不**以 `session_start` 加载(与 P-015 同)
- 不说 output style、memory、workflow 的 frontmatter 也量过:实测只覆盖规则、`SKILL.md`、斜杠命令、子 agent;
  前两类经同一个函数是读源码得出的,没有行为测量
- 不说 BOM 开头的 `SKILL.md`"不会被加载":它照样进清单、照样能调用,只是 frontmatter 被忽略;不进清单的是子 agent
- 不说分数变了:这个改动不碰计分路径,真机扫描的分数前后相同

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 测试:前导字节、`paths` 值、`SKILL.md` 读法、context_bloat、花括号上限;跑红 | `parse, collect, hygiene: tests — a rule whose frontmatter does not start at the first byte, or whose paths keep no glob Claude Code uses, is labelled path-scoped, and a skill description after a BOM is read (P-024)` |
| 2 | `splitFrontmatter`:`---` 必须是头三个字节 | `parse: frontmatter is read only when the file starts with ---, as Claude Code reads it, so a blank line or BOM above it no longer makes a rule path-scoped or a skill description count (P-024)` |
| 3 | `PathScoped`:照 Claude Code 整理 `paths` 的值 | `parse: a rule is path-scoped only when its paths keep a glob Claude Code uses; empty, ** and non-string values load every session (P-024)` |
| 4 | 规格 §4 采集表那一行、`pipeline.md` 一条防护 | `docs: spec and pipeline rules say a rule is path-scoped only on the frontmatter and globs Claude Code honours (P-024)` |
| 5 | 本文件、索引 | `proposals: P-024 (P-024)` |

## 未决问题

1. **`paths:` 的值(`[]`、`""`、`**`、数字、映射……)算不算本条?**
   **建议**:算。它和前导字节是同一个问题——报告说按路径加载,agent 每次会话都读它——任务的判据也写成"只在 Claude Code
   会遵守 `paths:` 时才标",而这十种值都实测过。另开一份只会让同一个函数改两次。
   **已决(2026-10-09)**:按建议。
2. **花括号展开要不要照搬?**
   **建议**:照搬语义,但不照搬枚举:深度优先,碰到第一个有效 glob 就返回;展开总数设上限(1024),超了按"每次会话都加载"
   答。照搬枚举会让 `{a,b}` 重复 40 次的一行把扫描器拖进 2^40 次展开——被扫对象不能决定扫描器停不停(`detect.md` 里
   FIFO、超大文件同一条道理)。超限时往"每次都加载"那边答,最坏是报告多算了上下文,不会再说少。正常写法在第一个展开上就返回。
   **已决(2026-10-09)**:按建议。
3. **要不要模拟 Claude Code 的描述回退(没有 frontmatter 时拿正文第一行当描述)?**
   **建议**:不。aguard 对没有 frontmatter 的文件一直给空描述,本条只让"frontmatter 被忽略"的文件和"没有 frontmatter"的
   文件同等对待。回退文本最多 100 字符,不会触发 context_bloat;要模拟就改了 HTML 注释、标题开头那些今天已经一致的文件的读法。
   **已决(2026-10-09)**:按建议。
4. **子 agent、斜杠命令用的是同一个解析器,一起改吗?**
   **建议**:一起改。实测 Claude Code 对它们同样不认(命令的描述变成 `---`,子 agent 直接不进清单);判官对它们"声明的用途"
   随之变成 `(not declared)`,与 agent 看到的一致。为它们留一个宽松的旧解析器,等于在实测之后保留一处已知的不一致。
   **已决(2026-10-09)**:按建议。
5. **要不要对"frontmatter 被 Claude Code 忽略"出一条 note 或发现?**(例如 BOM 开头的 `SKILL.md`,它的
   `disable-model-invocation`、`allowed-tools` 都不生效;子 agent 整个不加载)
   **建议**:不在本条。那是一条新规则,维度、严重度、在真实安装上的命中数都要单独定和量;本条只让已有的标签和读法说实话。
   **已决(2026-10-09)**:按建议。
6. **YAML 修复、闭合分隔符这两处出入呢?**
   **建议**:不在本条,记为后续。前者方向相反(aguard 读少了),后者没量过;两者都和"前导字节"无关。
   **已决(2026-10-09)**:按建议。

## 完成

```
合入:PR #36(2026-10-09;sha 用 git log --grep P-024 找)
发布:待发
证据:实测(本机 Claude Code 2.1.107,隔离 CLAUDE_CONFIG_DIR,claude -p 在回环抓包服务器回 400 后退出):规则 InstructionsLoaded 两级各 7 种前导形状 + 用户级 17 种 paths 值;skill 清单 7 种前导形状、命令与子 agent 各 4 种。结果即「问题」一节两张表
证据:W1 在 fd28344 上红,原因与判据一致:TestPathScoped_MatchesClaudeCode 23 行里红 13 行,恰好是表里"不一致"的 3 种前导形状 + 10 种 paths 值,均为 "PathScoped = true, want false";TestPathScoped_BraceExpansionIsBounded 的 "every expansion empty" 红(不展开,答 true);TestReadSkill_FrontmatterMustStartAtTheFirstByte 三种前导各红(读出 probe/probe description,Body 只有 "\n# Body\n",BodyLine 4/5/5);TestSplitFrontmatter 的 leading blank + bom 红;TestCollectRules_PathScopedOnlyWhenClaudeCodeHonoursIt 红 3 条(blank、bom、nothing 都带 (path-scoped));TestContextBloat_OnlyForADescriptionClaudeCodeLists 红(targets = [listed ignored])→ W2 后前导字节相关全绿,W3 后全绿
证据:反向断言:TestPathScoped_MatchesClaudeCode 的 LF/CRLF 第 1 字节、列表、标量串、逗号串、嵌套列表、真实花括号 7 行修前修后都是 true,HTML 注释、标题、空值 3 行修前修后都是 false;TestCollectRules_PathScopedOnlyWhenClaudeCodeHonoursIt 里 line1 修前修后都带 (path-scoped);TestReadSkill_FrontmatterMustStartAtTheFirstByte 的 first byte LF/CRLF 与原有 TestReadSkill、TestContextBloat、TestCollectRules_RecursiveAndPathScoped 不改照绿
证据:变异确认测试会咬(跑完还原,未提交):去掉展开上限 → BraceExpansionIsBounded 2 秒超时 "brace expansion is unbounded";不做花括号展开 → "braces that expand to **" 与 "every expansion empty" 红;不去结尾 /** → "**/**"、"/**" 红;不按逗号切 → "** twice in one string" 红
证据:实测目录复扫(aguard scan --json --inbox off,31 份规则):fd28344 构建标签与实测一致 14/31 → 本分支 30/31,剩下那份是不加引号的 **/no-such-g/*.ts(YAML 修复,方向相反,未决 6);两个 BOM 开头的超长描述 skill:fd28344 出 context_bloat ×2 + duplicate_fn ×1 → 本分支 0,两个第 1 字节的照出 context_bloat ×2 + duplicate_fn ×1
证据:真机 make build && ./bin/aguard scan --root ~/.claude --quiet:前后都无输出、退出码 0;同一命令加 --json 前后对比,除 scanned_at / tool_version 外逐键相同:overall 69 · overall_effective 69 · artifacts 180 · rules 24 · path-scoped 15 · 计分发现 high 162 / medium 451 / low 193 · notes 10 · hygiene context_bloat 6 / duplicate_fn 11 · inbox 4。没有差异的原因:~/.claude 下没有一份 .md 的 --- 前有 BOM 或空白(逐文件查了头部,0 份),15 份 path-scoped 规则的 paths 都留得下有效 glob
证据:哈希不变:TestHashGolden PASS;不做什么 —— git diff --stat origin/main -- internal/collect/hash.go internal/detect internal/score go.mod go.sum cmd/aguard/claude_rules_test.go 为空
证据:make verify: all gates passed;go version go1.23.5(无工具链切换),go.mod 第二行 go 1.23.5,无新依赖
```

