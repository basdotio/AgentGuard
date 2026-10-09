<!-- SPDX-License-Identifier: MIT -->
# 013 — settings.json 解析失败时,终端和 markdown 报告说 "looks safe":artifact 自己的 dim-0 note 从不渲染

- **来源**:collect 把 `PARSE-000` 挂在 artifact 上而不是扫描级 note,终端、markdown、HTML 只渲染扫描级 note,
  于是一个解析不了的 `settings.json` 被报成 "looks safe … Nothing was found to check"(不变量 #5:任何遗漏都不许静默)。
  移植自旧仓 agent-guard 的 P-054(私有仓)
- **依赖**:无
- **分支**:`p/013-artifact-notes-rendered`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

一个 `settings.json` 写坏了的 `.claude`(内容 `{"hooks": {"PreToolUse": [ broken`),用 `main`(`dec64ca`,v0.18.0)
构建的二进制实测:

```
$ aguard check …/broken/.claude
AgentGuard scan · root=…/broken/.claude
Risk score 100/100 (Low)

Summary
  Your Claude Code setup looks safe. No findings.
  Nothing was found to check under this root.


✅ No risk findings (static).

Scan details
  …
  Inventory: skills=0 mcp=0 hooks=0 permissions=0 subagents=0 commands=0 plugins=0 connectors=0
$ echo $?
0
```

`--verbose` 与默认输出一字不差;`--md -` 同样是 "looks safe" + "Nothing was found to check",没有 "Not checked" 一节;
`scan --root … --html`(`check` 没有 `--html`)同样,页面里 `PARSE-000` 出现 0 次。而 `check --json` 里那条说明就在:

```
artifacts: [("hook", "settings.json", hash "", score 100, findings [("PARSE-000", dimension 0, "Parse failed, artifact not fully covered")])]
notes: []
```

`check --sarif` 也带着它(`PARSE-000`,`properties.artifact = "hook:settings.json"`)。

原因:`collect.withParseError`(`internal/collect/collect.go:580`)把 `PARSE-000` 挂在**它造出来的那个 artifact 上**,不放进
扫描级 `notes`。三个给人读的渲染器只从 `ScanResult.Notes` 取 dimension-0 说明(`text.go:167` 的 `writeNotes(w, r.Notes, …)`、
`markdown.go:79` 与 `html.go:264` 的 `splitNotes(r.Notes)`),而 `report.Aggregate`(`aggregate.go:86`)跳过所有 dimension-0
的发现 —— 于是 artifact 自己的 note **哪里都不印**。SARIF 印了(它遍历每个 artifact 的 findings),JSON 印了(原样序列化),
给人看的三个都没有。

同一份配置里装着 hooks、permissions、env,是这个工具最在意的那块面;它没被读,报告却说 "looks safe"、"Nothing was found to
check"(找到了,只是没读成)。这正是不变量 #5("任何遗漏都不许静默")要防的事:说明产出了,读者看不到。README 里
"`--json` / `--html` / `--md` always carry everything"(`README.md:186`)对 html 和 md 也不成立。

`withParseError` 有三个调用点:`settings.json` / `settings.local.json`(hook 类 artifact,`collect.go:540`)、MCP 配置
(`~/.claude.json` 等,mcp 类,`collect.go:476`)、`plugins/installed_plugins.json`(plugin 类,`plugins.go:144`)。
`main` 上实测三种都是同一个缺口:坏的 `~/.claude.json`(`{"mcpServers": {`)和坏的 `installed_plugins.json` 同样报
"looks safe … Nothing was found to check",JSON 里各有一个带 `PARSE-000` 的 artifact、`notes` 为空。

退出码 0 本身不是这次要改的:dimension-0 note 从不参与 `--fail-on`(`score.Deterministic`),这是不变量 #4 的一部分。

## 初步方向

只改给人读的三个渲染器(`internal/report` 的 text / markdown / html):artifact 自己的 dimension-0 note 和扫描级 note 进同一条
"Not checked" 通道(同一行折叠、同一段 `--verbose`、同一个 `<details>`、同一个 HTML 区块)。数据不挪 —— JSON / SARIF
已经带着它,字节不变。Summary 里的两句("looks safe"、"Nothing was found to check")要知道覆盖不全,措辞沿用现有的
"coverage is incomplete"。分数、退出码、`--fail-on` 都不动。
