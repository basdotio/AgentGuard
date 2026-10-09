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

## 完成的判据

- [ ] `TestApproveRefusesWhatHasNoContentHash`(`cmd/aguard/gate_approve_test.go`,新),两种今天就能走到的目标各一行:
  `settings.json` 坏掉的 `.claude` 目录(`PARSE-000`),和一个 `chmod 000` 的单文件。每行断言:`approvePath` 返回错误,
  错误里有 `<kind> "<name>" has no content hash`、`nothing was approved` 和指向 `--json` 的提示(`PARSE-000` 那行还要说出
  `did not parse`,另一行说 `the scanner could not compute one`);错误**不是** `*failExit`(于是 `main` 走运行错误,退出码 2);
  输出里没有 `approved`;批准库文件**没有被创建**。今天红:返回 nil,打印 `approved … hash `,并新建了一个空的批准库
- [ ] `TestApproveRefusalLeavesTheStoreAlone`(同文件,新):批准库里已有一条批准时,拒绝之后它**还是同一个文件**
  (`os.SameFile`)、字节不变、那条批准还在。今天红:`Save` 用临时文件 + rename 重写了它
- [ ] `TestApproveNeverFallsBackToAnotherArtifact`(同文件,新):坏 `settings.json` + 一个 subagent + 一个 command 的 root
  → 拒绝,批准库不被创建。拒绝不能退到"第一个有哈希的 artifact"
- [ ] `TestCleanLoadWithNoHashIsNotClaimedTrusted`(`internal/gate/unhashed_test.go`,新):`PreToolUse` 干净分支遇到空哈希
  (`PARSE-000` 的 hook、没有任何 note 的 instruction 各一行)→ 不返回决定(照常放行)、消息里没有 `trusted`、说
  `not remembered, it has no content hash: <原因>`、不报 store 变更、批准库为空。今天红:说 `trusted from now on for content (none)`
  并报 store 变更
- [ ] 反向断言 `TestApproveStillRecordsWhatHasAHash`(`cmd/aguard/gate_approve_test.go`,新,今天就绿):干净的 skill → 返回 nil,打印
  `approved skill "<name>" (…, clean)`,批准库里恰好一条,key 等于 `collect.TreeHash` 算出的哈希;
  带凭据外泄链的 skill → 记为 `accepted-risk`,并打印那句 "you accepted a risk" 的提醒。修完不改一字仍绿
- [ ] 反向断言 `TestCleanLoadWithAHashIsStillRemembered`(`internal/gate/unhashed_test.go`,新,今天就绿):同一个 artifact 带哈希
  → 记为 `clean`、说 `trusted from now on for content <短哈希>`、报 store 变更
- [ ] 反向断言:`internal/gate` 已有的测试文件一字不改仍绿(含 `TestEmptyHashIsNeverApproved`);闸门不变量 #2 不动 ——
  写批准的调用点仍是原来那三处(`approvePath`、`handlePre`、`handlePost`),没有哪一处接收外部给的哈希
- [ ] `make verify` 绿;`go version` 不切换工具链

## 不做什么

- **不改批准库**(`internal/gate/approvals.go`):`Approve` 仍静默丢掉空 key、`Approved("")` 仍恒 false
- **闸门 hook 路径只改 `handlePre` 的干净分支**:哈希为空时不说 trusted、不报 store 变更;`handlePost` / `SessionStart` 不动
- **不让 `approve` 一次批多个 artifact**,也不在最差 artifact 没有哈希时改批别的 artifact(未决问题 2)
- **不给没有哈希的东西造哈希**:`collect` / `detect` 一行不动;`PARSE-000` 的 artifact 和读不了的文件仍是 `""`。
  给 hook / MCP / permission 算哈希是 P-009 的事
- **不改 `approve` 成功时的输出**(`approved <kind> "<name>" (…)` 那三行和 accepted-risk 的提醒)
- `go.mod` / `go.sum` 不动,不加依赖

## 不能说什么

- 不说"没有哈希的东西现在可以批准了":修的是**拒绝得出声**,不是让它们可批准
- 不说"以前那些 approved 现在失效了":它们**从来没有存进去过**(批准库拒收空 key),闸门一直照问
- 不说"P-009 合入之后这个问题就没了":`PARSE-000` 的 artifact 和读不了的文件在 P-009 之后仍然没有哈希
- 不说 `aguard check`(终端 / markdown)会显示配置没解析:它不显示 artifact 自己的 dim-0 note,解析失败的配置在那里读作
  `looks safe`(`main` 上实测:坏 `settings.json` 的 `.claude` 目录,终端报告 `Your Claude Code setup looks safe. No findings.`,
  而 `--json` 里有 `PARSE-000` 和 `COV-000`)—— 那是另一个缺口,人已批准单开(P-013)。所以拒绝信息自己写全文件和
  `PARSE-000`,提示指向 `check --json`

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 两条拒绝测试 + 一条反向断言,跑红 | `cmd: tests — approve prints "approved" for a target with no content hash, stores nothing and exits 0 (P-011)` |
| 2 | `gate.Verdict` 带上"为什么没有哈希";`approvePath` 遇到空哈希拒绝、说明原因、不碰批准库;`approve` 的帮助说一句 | `gate, cmd: approve refuses a target with no content hash, says why, and leaves the store alone (P-011)` |
| 3 | install-gate 对子、spec §17、`.claude/rules/gate.md` 各一句 | `docs: install-gate, spec §17 and gate.md say approve refuses what has no content hash (P-011)` |
| 4 | 闸门干净分支在空哈希时也宣称 trusted —— 红测试 + 反向断言 | `gate: tests — a clean load with no content hash is announced as trusted and rewrites the store (P-011)` |
| 5 | 修:不说 trusted、不报 store 变更;未解析的原因写出文件和 `PARSE-000` | `gate: a clean load with no content hash says it was not remembered and why, and leaves the store alone (P-011)` |
| 6 | 拒绝里的 `check` 提示改指 `check --json`;加"不退到别的 artifact"的测试 | `cmd: approve's refusal points at check --json, which does list what was not read, and never falls back to another artifact (P-011)` |
| 7 | gate.md 补干净分支这一句 | `rules: gate.md says the PreToolUse clean branch, like approve, never claims trust over an empty hash (P-011)` |
| 8 | 本文件、索引 | `proposals: P-011 (P-011)` |

## 未决问题

1. **拒绝用哪个退出码?**
   **建议**:2(运行错误,返回普通 `error`)。1 在本工具里的意思是"有发现达到阈值",这里不是发现的问题,是"做不到";
   与同一个函数里 `nothing could be collected from … — refusing to approve` 走的是同一条路。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
2. **一次调用涉及多个 artifact 时怎么办?**
   **建议**:这个情形在 `approvePath` 里不存在 —— 它只批准 `gate.Summarize` 选出的**那一个**最差 artifact,哪怕目标是一个
   路由到 root 采集器的目录。所以"批有哈希的、点名跳过的"不适用;最差 artifact 没有哈希就**整个调用拒绝**,不退到
   下一个有哈希的 artifact —— 那等于批准一份打印出来的判决没有描述的内容。让 `approve` 一次批多个是另一个契约,不在本条。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
3. **原因写什么?**
   **建议**:只写 artifact 自己说得出的:带 `PARSE-000`(`SrcParseError`)→ 说它没解析、没读全;
   其他 → `the scanner could not compute one`,并给一条能看到"哪里没读"的命令。
   **不按 kind 猜**:P-009 之后 kind 不再决定有没有哈希,按 kind 写的原因会在它合入那天变成假话。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。**评审更正(2026-10-09,旧仓)**:提示原先指向 `aguard check <target>`,
   而终端报告对解析失败的配置打印 `looks safe`(见「不能说什么」,本仓 `main` 上复测相同);所以 `PARSE-000` 的原因
   写出**文件和 note 本身**(`"<file>" did not parse, so it was not fully read [PARSE-000: <title>]`),提示改指 `check --json`(W5、W6)。
4. **原因放在哪里算?**
   **建议**:`gate.Verdict` 加一个字段(`Unhashed`),在 `Summarize` 里填 —— 只有它知道选中的是哪个 artifact;
   在 `cmd` 里再找一遍"最差"等于复制一份定义,而本仓库对"同一个判定只有一份"是有明文要求的。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
5. **闸门 hook 路径要不要一起改?** `handlePre` 的干净分支在哈希为空时同样会 `Approve`(被丢掉)并说
   `trusted from now on for content (none)`。
   **建议**:不在本条。那条路的目标由 `ResolveSkill` 给出,总是目录,正常情况下走 `TreeHash`,而 `TreeHash` 不会返回 `""`;
   只有"skill 目录本身长得像 root"(例如里面有 `plugins/installed_plugins.json`)才会路由到 root 采集器、可能选出
   空哈希的 artifact。后果是一句不实的提示 + 下次照问(朝"多问"失败),不是放行。
   **已决(2026-10-09)**:按建议(旧仓)。**评审更正(2026-10-09,旧仓)**:评审实测复现了,而这和本条是同一个契约
   ("不对空哈希宣称信任"),所以并入本条(W4–W7),不另开。本仓 `main` 上复测相同(见「问题」最后一段)。
6. **与 P-009(未合)谁先合?**
   **建议**:先后都行;冲突只会在索引行。**后合的那一个**在 rebase 时要把 P-009 写进 `.claude/rules/gate.md` 的半句
   "`PARSE-000` 的 artifact 仍是 `""`,`approve` 对它们仍会空打印一句 `approved`(待修,不在 P-009 里)"改成"`approve` 拒绝它们(P-011)"
   —— 两条各自合入时那句都是真的,一起合入后它就是假的。实测见「完成」。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
