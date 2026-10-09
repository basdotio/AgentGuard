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
