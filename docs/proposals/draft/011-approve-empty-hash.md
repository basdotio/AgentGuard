<!-- SPDX-License-Identifier: MIT -->
# 011 — aguard approve 对没有内容哈希的东西也打印 approved,实际什么都没存

- **来源**:`aguard approve` 在最差 artifact 没有哈希(解析失败的配置、打不开的文件)时照样打印 `approved` 并退出 0,
  批准库其实拒收了空 key;闸门自己的 PreToolUse 干净分支对空哈希也说 trusted。移植自旧仓 agent-guard 的 P-053(私有仓)
- **依赖**:无(与 P-009 独立可合;P-009 让 hook / MCP / permission 有了哈希,但本条要修的空哈希在它之后仍然存在)
- **分支**:`p/011-approve-empty-hash`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

`aguard approve <path>`(`cmd/aguard/gate.go:343` `approvePath`)扫描目标,取 `gate.Summarize` 给出的最差 artifact,
把它的哈希写进批准库,然后打印 `approved …`。

最差 artifact 的哈希为空时,批准库拒收(`internal/gate/approvals.go:167` `Store.Approve` 遇到 `""` 直接返回,
`:129` `Approved("")` 恒 false,`TestEmptyHashIsNeverApproved` 钉着),所以**什么都没存**;`approvePath` 照样 `Save`
(没有批准库时会新建一个空的)、打印 `approved`、退出码 0。用户以为这份内容已被信任,闸门下次照问,
`SessionStart` 照列 —— 而他刚收到的回答是"已批准"。

用 `main`(`dec64ca`,v0.18.0)构建的二进制,`--root` 指向一个空目录,实测三种目标:

```
settings.json 坏掉的 .claude 目录   → approved hook "settings.json" (100/100, clean)
                                       hash
                                     exit 0,新建了 38 字节的批准库:{"version": 1, "approvals": {}}
chmod 000 的单文件 notes.md          → approved instruction "notes.md" (100/100, clean)
                                       hash
                                     exit 0,同样新建空批准库
只有一条 hook 的 .claude 目录         → approved hook "PreToolUse[Bash]#1" (100/100, clean)
                                       hash
                                     exit 0,同样新建空批准库
```

第三种是因为 hook / MCP / permission 这三类在 `main` 上一律没有哈希(P-009 在修)。P-009 合入之后第三种消失,
前两种仍在 —— 解析失败的 artifact(`PARSE-000`)留成 `""`,读不了的文件也算不出哈希。

闸门自己也有同一个缺陷:`handlePre` 的干净分支(`internal/gate/hook.go:263` 起)对空哈希照样 `Approve`(被丢掉)、
说 trusted、报 store 变更。一个 skill 目录里有 `plugins/installed_plugins.json`(于是路由到 root 采集器)和坏掉的
`settings.json`,喂一条 `PreToolUse[Skill]` 事件,`main` 的二进制回答:

```
{"systemMessage":"AgentGuard: hook \"settings.json\" 100/100 (Low) · no finding at or above the threshold · trusted from now on for content (none)"}
```

并在 `--root` 下写出一个空的批准库。

## 初步方向

`approvePath` 在最差 artifact 没有哈希时**明确拒绝**:不打印 `approved`,报一句说明是哪个 artifact、为什么没有哈希、
什么都没批准;走 CLI 的运行错误(退出码 2);不碰批准库。有哈希的目标(正常的 skill / 插件 / 目录 / 文件)行为不变。
闸门的干净分支同理:哈希为空时不说 trusted、不报 store 变更。动到 `cmd/aguard/gate.go`、`internal/gate`,
也许给 `gate.Verdict` 加一个"为什么没有哈希"的字段;文档对子和 spec §17 各加一句。
