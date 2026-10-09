<!-- SPDX-License-Identifier: MIT -->
# 014 — hook 越界提示的证据里,箭头后面的解析路径没经过脱敏

- **来源**:`HOOK-002` 的 snippet 由 `redactClip(ref) + " → " + resolved` 拼成,解析出的路径没经过脱敏,前半段被脱掉的东西
  会在箭头后原样出现;`SUP-006` 的 `Why` 里注册表地址同理。移植自旧仓 agent-guard 的 P-056(私有仓)
- **依赖**:无
- **分支**:`p/014-hook-outside-snippet-redacted`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

hook command 引用的脚本解析到 HOME 外时,`HOOK-002` 的证据片段这样拼(`internal/detect/hooks.go:361`):

```go
Snippet: clip(redactClip(ref) + " → " + resolved)
```

箭头前面的 `ref` 是从 hook command 里抄出来的,过了 `redactClip`;箭头后面的 `resolved` 是同一串字节展开
`~`/`$HOME`/`$CLAUDE_PROJECT_DIR`、或拼上 home 之后的结果,**一个字节都没脱敏**。绝对路径的引用 `resolved` 就等于 `ref`。
所以脱敏器在前半段抹掉的东西,后半段原样印出来——不变量 #3 说的"`detect.Redact` 是产出 snippet 的唯一途径"在这一处不成立。

复现(本仓 `main` dec64ca 构建的二进制,`HOME` 指向 fixture 里的 home,两条 hook:`sh <HOME 外>/opt/ghp_<36 位>/hook.sh`
和 `sh <HOME 外>/outside/guard.sh`,`scan --json --inbox off`):

- 带 token 的那条 `HOOK-002` 的 snippet:`<REDACTED><REDACTED>/hook.sh → /tmp/aguard-scratch/p014fx/opt/ghp_<36 位>/hook.sh`
  —— token 在箭头后原样出现
- 同一个引用在 `COV-000` note 里:`<REDACTED><REDACTED>/hook.sh` —— 同一串字节,这里是脱敏的
- 另一条普通路径的 `HOOK-002`:`<REDACTED>.sh → /tmp/aguard-scratch/p014fx/outside/guard.sh` —— 普通长路径也会被熵检测
  整段吃掉前半段,而后半段照印

这条 snippet 进 JSON、HTML、markdown(`check --md -` 是写来贴 PR 评论的)、SARIF(上传到代码扫描)和终端报告。
判官那条路两处都会对证据再脱敏一次(`judge/run.go` 的 `triageItems`、`judge/excerpt.go`),所以泄漏面是报告,不是模型端点。

**同类排查**(在 `internal/detect`、`internal/collect`、`internal/permcheck` 里找"同一段字节一处脱敏、另一处原样"):
`SUP-006`(包源被改)的 `Why` 把从文件行里取出的目标拼进句子(`internal/detect/shape.go:194-204`),而同一行在 snippet 里是脱敏的。
目标解析不出主机时(端口是 `${PORT}` 这类 `url.Parse` 拒绝的写法、userinfo 后面没有主机、`${VAR:-…}` 的默认值坏了),
拼进去的是原样的目标串。同一个 fixture 里一个 skill 脚本写 `npm config set registry https://ci:ghp_<36 位>@npm.corp:${PORT}/`:
snippet 是 `npm config set registry https://ci:<REDACTED>@npm.corp:${PORT}/`,`Why` 里是完整的 token。
整份 JSON 里 token 出现 2 次(`HOOK-002` 的箭头后一次、`SUP-006` 的 `Why` 一次)。

后果:用户把一个 secret 放进了路径或 URL(目录名、registry 地址里的凭证),工具在一处替他抹掉、在同一条发现的另一处替他印出来;
报告越是被转贴(PR 评论、SARIF 上传),这一处越不该是例外。

## 初步方向

`HOOK-002`:先把整串 `ref → resolved` 拼好,再整体过一次 `redactClip`(先脱敏后截断的顺序不变);普通路径的 snippet 与今天逐字相同。
`SUP-006`:拼进 `Why` 的目标先脱敏。`detect.Redact` 本身不动。其余同类形状的地方逐一列出,形状不同的不在本条修。
