<!-- SPDX-License-Identifier: MIT -->
# 009 — hook、MCP、permission 没有哈希,闸门和信誉库对它们恒"不认识"

- **来源**:hook、MCP 服务器和权限列表的 artifact 哈希为空,闸门的 SessionStart 和信誉库按哈希识别内容,对这三类恒判为未知;
  同一份配置在两台机器上也没有共同的身份。移植自旧仓 agent-guard 的 P-051(私有仓)
- **依赖**:无
- **分支**:`p/009-content-hash-three-kinds`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

`ArtifactReport.Hash` 是信誉库和闸门批准共用的 key(`.claude/rules/hash.md`)。skill、plugin、单文件、connector 都有;
**hook、MCP server、permission 三类从来没有** —— 采集时就写死了空串:

| 位置 | 写进 Hash 的 |
|---|---|
| `internal/collect/hooks.go:64`(每条 hook,settings.json 和插件自带的都走这里) | `""` |
| `internal/collect/collect.go:485`(每个 MCP server,`~/.claude.json`、项目 `.mcp.json`、插件 `.mcp.json`) | `""` |
| `internal/collect/collect.go:549`、`:553`(`permissions` allow/deny,和 `settings env` 块) | `""` |

而每个消费方都把 `""` 读成"没人审过":

- `gate.Store.Approved("")` 恒 false(`internal/gate/approvals.go:129-135`,`TestEmptyHashIsNeverApproved` 钉着),
  `Store.Approve` 遇到 `""` 直接返回、什么都不存(`approvals.go:167-175`);
- `reputation.DB.Match("")` 恒 false(`internal/reputation/reputation.go:111-117`);
- 闸门 `SessionStart` 按 `Approved(a.Hash)` 跳过已批准的(`internal/gate/hook.go:381`),这三类永远跳不过;
- `aguard hash <root>` 对这三类打印空哈希(`cmd/aguard/main.go:827`)。

后果:

- **这三类是闸门管不住的那一半**(从会话第一轮就活着,没有加载事件),却也是唯一一类**连"认识"都做不到**的:
  同一份配置、同一条 hook,每台机器、每次会话都是"新的";信誉库里也无法为它们录入任何一条(`reputation.New` 丢掉空 key 的条目)。
- **`aguard approve <root>` 在最差 artifact 是 hook/MCP/permission 时打印 `approved … hash ` 然后什么都没存** ——
  `Approve` 把空 key 静默丢掉了。

用 `main`(`dec64ca`)构建的二进制对一个临时 root 手跑(2026-10-09):一条 `PreToolUse[Bash]` hook 跑 `sh ~/.claude/hooks/pre.sh`,
脚本里是 `curl … | bash`:

```
aguard hash <root>          →   "  hook:PreToolUse[Bash]#1"(哈希一栏是空的)
aguard approve <root>       →   approved hook "PreToolUse[Bash]#1" (75/100, accepted-risk)
                                  hash                                  ← 空
                                exit 0;.aguard-approvals.json 里 "approvals": {}
SessionStart                →   照样列出 hook PreToolUse[Bash]#1 75/100 EXEC-001
```

真机(本机 `~/.claude`,2026-10-09,同一个 `main` 二进制):hook 29 个、MCP 27 个、permission 2 个,**58 个 Hash 全是空串**;
其余 117 个 artifact 都有哈希。

## 初步方向

在 detect 阶段(`Redact` 和脚本跟进都在那里;collect 不能 import detect)给这三类算一个**按内容的**哈希:
带 kind 前缀做域分离,不含 `OwnerRoot` 和任何本机绝对路径,MCP/permission 用排序键的规范 JSON,
secret 值先脱敏(哈希会印进 JSON、存进 approvals,不能是凭据的摘要);hook 的哈希包含它跟进的脚本内容。
`TreeHash`/`FileHash` 一字不动(信誉条目和已存批准全靠它们)。在 `analyze()` 里先于信誉、闸门、`approve` 填好;`aguard hash` 同步。
