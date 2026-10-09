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
P-015 用 dec64ca 构建的 `aguard scan --json --inbox off` 确认过:空行和 BOM 两份被命名为 `(path-scoped)`。

同一个函数也给 `SKILL.md`(以及子 agent、斜杠命令)读 `name`/`description`。Claude Code 对 `---` 前有 BOM 或空行的
`SKILL.md` 认不认 frontmatter,没有量过:如果它不认,aguard 拿来算 context_bloat、重复描述、判官"声明的用途"的那段
描述,就不是 agent 实际看到的那段。

## 初步方向

先照 P-015 的办法量 Claude Code 2.1.107:规则文件和 `SKILL.md` 各量 `---` 前"没有 / 空行 / BOM / 注释"几种形状
(隔离的 `CLAUDE_CONFIG_DIR`,不碰用户配置)。再让 `parse` 判断 frontmatter 的方式跟实测一致,只在 Claude Code 会遵守
`paths:` 时才标 `(path-scoped)`;`SKILL.md` 只有实测和 aguard 不一致时才改,且只改到一致为止。哈希只看字节,不受影响。
