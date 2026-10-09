<!-- SPDX-License-Identifier: MIT -->
# 015 — 许可证注释把十份规则的 frontmatter 挤下第一行:按路径加载失效,范围测试不再查 glob

- **来源**:新发现(2026-10-09)。仓库首个公开提交 `2b4c2a7` 在十份按路径加载的规则文件的 `paths:` frontmatter
  上方加了一行许可证注释;范围测试从此一条 glob 都不查,Claude Code 也不再按路径加载这十份文件
- **依赖**:无
- **分支**:`p/015-rules-frontmatter-first`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

`.claude/rules/` 下十份文件——`detect` `gate` `hash` `judge` `npm` `pipeline` `plugin` `report` `reputation` `score`——
第 1 行是 `<!-- SPDX-License-Identifier: MIT -->`,第 2 行才是 `---`。这一行是 `2b4c2a7`(本仓第一个提交)带进来的,
十份文件在那个提交里就是这个形状。`conventions.md` 和 `invariants.md` 没有 frontmatter,按设计每次会话都加载,不在此列。

**Claude Code 只认第一行开始的 frontmatter。** 它的术语表对 frontmatter 的定义里写着
"The opening `---` must be the file's first line."(code.claude.com/docs/en/glossary#frontmatter);
规则文件的 frontmatter 说明(code.claude.com/docs/en/memory#rules-frontmatter-reference)也说它位于文件顶部的两条
`---` 之间;同页说没有 `paths` 的规则在启动时加载。

**本机实测(Claude Code 2.1.107)**:用 `InstructionsLoaded` hook 记下每次加载的 `file_path` 和 `load_reason`,
在 `git archive origin/main`(dec64ca)解出的副本里跑 `claude -p`。12 份规则文件**全部**以 `session_start` 加载,
包括这十份。(命令在向模型发请求前就退出了——本机该 CLI 未登录——而加载事件在那之前已全部触发,所以这次测量不依赖模型。)
另在一个只放规则文件的空目录里量了几种形状,每份的 `paths:` 都指向不存在的目录:

| 第一行之前有什么 | `session_start` 加载? |
|---|---|
| 一行 HTML 注释(本仓的形状) | 是 —— `paths:` 被忽略 |
| 一个空行 | 是 |
| UTF-8 BOM | 是 |
| 一行 `# 标题` | 是 |
| 没有,`---` 在第 1 行(LF) | 否 |
| 没有,`---` 在第 1 行(CRLF) | 否 |
| 没有 frontmatter | 是(对照) |

所以这十份共 797 行、约 84 KB 的"只在动到对应文件时才读"的防护点,今天在本仓的每一次会话开头都整份进上下文;
而 `CLAUDE.md` 的"何时加载"表和 `docs/README.md` 都说它们按路径加载。

**范围测试同时失明。** `cmd/aguard/claude_rules_test.go` 的 `frontmatterPaths` 在文件不以 `---\n` 开头时返回 `nil`
——"没有 frontmatter、常驻规则"——于是 `TestClaudeRulesAreScopedToExistingPaths` 对这十份文件只查了 200 行上限,
一条 glob 都没查。这个测试的文件头注释写的正是它要防的事:一条匹配不到任何文件的 glob 会让那份规则"never read, silently"。
在 dec64ca 上把 `score.md` 的 `internal/score/**` 改成 `internal/scorex/**` 跑这个测试:**PASS**。

后果有两层:一是上下文成本——每次会话多读约 84 KB,不相关的包的防护点和当前任务混在一起;二是本仓唯一一道
"规则会不会永远读不到"的检查实际上是关着的,以后改名、挪目录让某条 glob 落空,不会有任何东西报出来
(而把注释挪走、让 frontmatter 生效之后,落空的 glob 才真的会让那份规则读不到)。

## 初步方向

范围测试先学会报"frontmatter 不在第一行"(写红);再把十份文件的许可证注释挪到 frontmatter 结束的 `---` 下一行
(保留,不删,行数不变)。只动这十份文件和 `cmd/aguard/claude_rules_test.go`。glob 检查重新生效后如果有落空的,
替换无歧义才在本条修,否则停下来问。

## 完成的判据

fixture 都在 `t.TempDir()` 现搭,与现有测试同一写法。

- [x] `TestClaudeRulesProblemsAreCaught`(`cmd/aguard/claude_rules_test.go`,扩充)钉住:上表里实测会让 Claude Code 忽略
  `paths:` 的四种形状——`---` 之前有一行 HTML 注释、一个空行、一行 `# 标题`、一个 UTF-8 BOM——各报一条
  `frontmatter must start on line 1`,它们的 glob 都能匹配,所以这是各自唯一的问题;精确计数 3 → 7。只加 fixture、不改检查时红
- [x] 反向断言(同一测试):以下都**不**报 ——
  常驻规则开头有 HTML 注释、没有 frontmatter(`invariants.md` 的形状);
  正文里有两条 `---` 分隔线、中间是一句 `Note: …` 这种能被 YAML 读成映射的散文;
  围栏代码块里的 `paths:` 示例(它的 glob 落空,也不能报成落空);
  frontmatter 在第 1 行、许可证注释在闭合 `---` 的下一行(W2 之后十份文件的形状);
  CRLF 换行、在第 1 行开始的 frontmatter(Claude Code 认它,不能报成"不在第一行",未决 4)。
  原有三条(落空的 glob、空 `paths:`、超长)照报
- [x] `TestClaudeRulesAreScopedToExistingPaths`:W1 之后在本仓红,报的**恰好是这十份**,每份一条;W2 之后绿
- [x] glob 检查确实重新生效:W2 之后把 `score.md` 的 `internal/score/**` 临时改成 `internal/scorex/**` → 红,
  `matches no file`;同一改动在 dec64ca 上是绿的(「问题」一节)。变异跑完还原,不提交
- [x] 十份文件每份行数前后相同,每份的改动只是一行换了位置(`sort` 后与 `origin/main` 逐字节相同)
- [x] `CLAUDE.md`「何时加载」一列与每份文件的 `paths:` 逐条一致
- [x] Claude Code 2.1.107 同样的 `InstructionsLoaded` 测量:W2 之后以 `session_start` 加载的规则只剩 `conventions.md`、`invariants.md`
- [x] `make verify` 绿;`go version` 不切换工具链,`go.mod` 第二行仍是 `go 1.23.5`,不加依赖

## 不做什么

- **不动其他任何 `.md` 的许可证注释**:`CLAUDE.md`、`CONTRIBUTING.md`、`invariants.md`、`docs/**` 等的第一行照旧
  ——它们没有 frontmatter,注释在第一行无害。`conventions.md` 本来就没有许可证注释,也不补
- **不改规则内容**:十份文件里除了那一行换位置,一个字节不动
- **不改 `claude_rules_test.go` 之外的 Go 代码**。尤其不改产品里的 `internal/parse/frontmatter.go`(见未决 5)
- 不让范围测试认 CRLF 换行的 frontmatter(未决 4)
- 不给 `CLAUDE.md` 或规则文件加"frontmatter 必须在第一行"的说明:测试的报错信息就是说明(未决 6)
- 不加依赖,`go.mod` 不动

## 不能说什么

- 不说"这十份规则现在会在动到匹配文件时加载":实测的只是它们**不再**以 `session_start` 加载;按路径触发(`path_glob_match`)
  要一次真正读文件的模型会话,本条没量
- 不说"所有版本的 Claude Code 都这样":只量了本机 2.1.107;文档的说法与实测一致
- 不说过去的会话因此漏读了防护点:方向相反——过去是**多读**,十份文件每次都在;修好之后,动到不匹配的文件时它们**不再**出现,
  这是 `CLAUDE.md` 那张表本来的设计
- 不说范围测试以前整个失效:200 行上限一直在查,失明的只是 glob

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 范围测试报"frontmatter 不在第一行";四种错位形状、五条反向断言;跑红 | `cmd: test — the rules scope check reports a paths frontmatter that does not start on line 1, which Claude Code ignores (P-015)` |
| 2 | 十份规则文件的许可证注释挪到闭合 `---` 下一行 | `rules: the licence comment moves below the frontmatter in the ten path-scoped files, so their paths take effect and get checked (P-015)` |
| 3 | 本文件、索引 | `proposals: P-015 (P-015)` |

## 未决问题

1. **检查认哪些形状:只认"注释在上面",还是"任何东西在上面"?**
   **建议**:任何东西。在第 1 行以外找到一对 `---` 夹着、能读成带 `paths` 键的 YAML 映射的块就报;文件以 BOM 开头也报;
   围栏代码块里的不算(那是示例)。实测注释、空行、BOM、标题四种都让 `paths:` 失效,只认注释会漏掉后三种,其中 BOM 肉眼看不见。
   要求"带 `paths` 键"是为了不把正文里的分隔线误报:两条 `---` 之间的散文即使被 YAML 读成映射,也不会恰好有 `paths` 键。
   **已决(2026-10-09)**:按建议。
2. **许可证注释是挪到 frontmatter 下面,还是删掉?**
   **建议**:挪到闭合 `---` 的下一行。删掉会让这十份成为仓库里唯一没有许可证标记的 `.md`;挪动不改行数,
   `detect.md` 仍是 198 行,不碰 200 行上限。
   **已决(2026-10-09)**:按建议。
3. **glob 检查重新生效后,如果有 glob 落空怎么办?**
   **建议**:替换无歧义(例如本仓自己的提交改过名的路径)才在本条修,前后写进「完成」;有歧义就停下来问,不猜。
   **已决(2026-10-09)**:按建议。
4. **范围测试不认 CRLF 的 frontmatter(`frontmatterPaths` 只认 `---\n`),而 Claude Code 2.1.107 认(上表最后一行),要不要一起修?**
   **建议**:不修。这是另一种失明,本仓 `.claude/rules/` 里没有 CRLF 文件(`git ls-files .claude/rules | xargs grep -l $'\r'` 为空);
   新检查对第 1 行开始的 CRLF frontmatter 也不报"不在第一行",不会给出错误的提示(反向断言 `crlf.md` 钉着)。
   **已决(2026-10-09)**:按建议。
5. **产品里的 `parse.PathScoped` 有相近的出入,要不要一起改?**
   `internal/parse/frontmatter.go` 的 `splitFrontmatter` 在找 `---` 之前先去掉 BOM 和开头的空白,所以 `collectRules`
   会把一份第一行是空行或 BOM 的规则命名为 `(path-scoped)`,而 Claude Code 2.1.107 每次会话都加载它(上表)。
   本仓的形状(注释在上面)它判断对了。实测:dec64ca 构建的 `aguard scan --json --inbox off` 扫上表那个目录,
   空行和 BOM 两份被命名为 `(path-scoped)`,注释和标题两份没有。
   **建议**:不在本条改。这是产品行为,而 `splitFrontmatter` 同时给 `SKILL.md` 用,那边 Claude Code 容不容忍前导空白没有量过;
   要改另开一份,先量 `SKILL.md`。
   **已决(2026-10-09)**:按建议。
6. **要不要在 `CLAUDE.md` 或某份规则里写一句"frontmatter 必须在第一行"?**
   **建议**:不写。测试的报错信息自己说明了原因;规则内容按「不做什么」不动,`CLAUDE.md` 的表也不需要改。
   **已决(2026-10-09)**:按建议。

## 完成

```
合入:PR 待开(2026-10-09;sha 合入后用 git log --grep P-015 找)
发布:待发
证据:TestClaudeRulesProblemsAreCaught(cmd/aguard/claude_rules_test.go);只加 fixture、不改检查时红:四种错位形状各一条 missing problem "<name>: frontmatter must start on line 1",want exactly 7 problems, got 3 → 加上 misplacedFrontmatter 后绿
证据:TestClaudeRulesAreScopedToExistingPaths(同文件);W1 后在本仓(dec64ca + W1)红:10 problem(s),恰好 detect gate hash judge npm pipeline plugin report reputation score 各一条 "frontmatter must start on line 1 (Claude Code ignores it anywhere else, so this rule loads every session)" → W2 后绿
证据:反向断言五条(licensed.md、ruled.md、example.md、moved.md、crlf.md 都不报)逐条用变异确认会咬:去掉围栏跳过 → example.md 被报(got 8);任何一对 --- 都算 → ruled.md 被报(got 8);去掉 BOM 分支 → late-bom.md 漏报(got 6);第 1 行开始的也报 → crlf.md 被报(got 8)。变异跑完还原,未提交
证据:glob 检查重新生效:score.md 的 internal/score/** 临时改成 internal/scorex/** —— dec64ca 上 PASS(失明)→ W2 后 FAIL,paths entry "internal/scorex/**" matches no file;还原,未提交。W2 后十份文件共 18 条 glob 全部匹配,没有落空的,未决 3 没有触发
证据:十份文件行数 198 91 25 41 78 151 77 51 69 16 → 不变;每份 diff 1+/1−,sort 后与 origin/main 逐字节相同;detect.md 仍是 198 行
证据:CLAUDE.md「何时加载」与十份文件的 paths: 逐条比对 10/10 一致,CLAUDE.md 未改
证据:Claude Code 2.1.107,InstructionsLoaded hook 记 load_reason,在 git archive 解出的副本里跑 claude -p:dec64ca 上项目级 session_start 加载 13 份(CLAUDE.md + 全部 12 份规则)→ 本分支 3 份(CLAUDE.md、conventions.md、invariants.md)。按路径触发的加载没有量(「不能说什么」)
证据:不做什么 —— git diff --stat origin/main -- '*.md' 去掉这十份规则和 docs/proposals 后为空;-- '*.go' 只有 cmd/aguard/claude_rules_test.go;-- go.mod go.sum CLAUDE.md .claude/rules/conventions.md .claude/rules/invariants.md 为空
证据:make verify: all gates passed;go version go1.23.5(无工具链切换),go.mod 第二行 go 1.23.5,无新依赖
```
