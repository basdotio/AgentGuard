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
