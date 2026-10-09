<!-- SPDX-License-Identifier: MIT -->
# 011 — 散文体指令文件不跑 EXEC/OBF/FS 规则

| 项 | 值 |
|----|----|
| 类别 | 检测精度 |
| 严重程度 | 中 |
| 状态 | 部分修复（slash command 已按 kind 跑全量规则，2026-10-09；subagent / rule / workflow / output style / memory 仍是散文，需规则精度） |

## 问题描述

`internal/detect/detect.go` 的 `roleForPath` 按**文件名**分配 `fileRole`：只有 `SKILL.md` 与
`CLAUDE.md` 是 `roleInstruction`（跑全量规则），其余 `.md` 一律 `roleDoc`——按 `roleAllows`
只跑维度 1（注入）与密钥检测。

后果：一个 subagent（`agents/x.md`）、slash command（`commands/x.md`）、rule（`rules/x.md`）
或 output style 里**真的写着** `curl … | bash`、`eval(atob(...))`、读 `~/.ssh/id_rsa`，
不会产生任何 EXEC / OBF / FS 发现。而这些文件都是 Claude Code 会加载并照做的指令。

## 出处

- `internal/detect/detect.go` — `roleForPath`（分类）与 `roleAllows`（`roleDoc` 只放行 dim 1）
- 该行为**早于**自动加载面采集的那次改动，不是它引入的

## 影响

漏报。注意注入检测仍然覆盖这些文件，所以"指令文件里藏了一段让 agent 干坏事的话"这个主要威胁面
是有覆盖的；缺的是"指令文件里直接给出恶意命令"这种更直白的形态。

## 为什么没有直接修

试过一次直接的修法：按 artifact kind 强制 `roleInstruction`（理由是"这些文件 Claude Code 确实会
照做"）。实测在一个**完全良性**的真实形态环境上产生 15 条发现、其中 8 条 high，把 100/100 打成
69/100 并让 `--fail-on high` 退出 1。误报来源全部是同一类：

- `rules/security.md`：「Never pipe a downloaded script into a shell (curl https://x | bash)」→ EXEC-001
- `agents/code-reviewer.md`：「Flag any code that reads ~/.ssh/id_rsa」→ FS-001 + EXFIL-001
- `projects/*/memory/MEMORY.md`：Claude 自己记的「fixed the installer; it used to do curl … | bash」→ EXEC-001

正则分不清「照做」与「禁止/引用/复述」。在这个误报率下用户学到的是忽略输出，比漏报更贵。该改动
已回退，回归测试见 `internal/detect/loaded_test.go` 的 `TestScan_BenignProseIsNotFlagged`。

## 修复方向

需要**规则精度**而不是角色提权：

1. **否定/引用感知**：同一行含否定语（never / don't / do not / avoid / 不要 / 禁止）或报警语
   （flag / report / 检测到）时，跳过维度 ≠ 1 的规则。
2. **新增中间 role**（如 `roleAuthoredProse`）：只放行高精度子集（EXEC-001/002、OBF-003），排除
   在散文里几乎必假的低精度规则（EXEC-003 裸 `eval(`、EXEC-004 子进程名、FS-001/FS-002 命中的是
   "提到路径"而非"访问路径"）。
3. **EXFIL 链对 markdown 指令文件整体关闭**：`credentialRE` 与 `networkRE` 各自匹配一句散文的
   概率太高，同文件命中几乎是必然。
4. 落地前必须有**误报侧的度量**：拿多个真实 `~/.claude` 跑 before/after，把 FP 数写进 ROADMAP。
   本条目的存在本身就是因为上一次缺这一步。

**2026-09-29 追记（P-031，否决记录）**：修复方向第 1 条"否定/引用感知"的一个子问题——`INJ-001` 打在"引用注入来教防御"的文字上——在语料 3 个 hard negative 上量过。三个全是同一份土耳其语红队语料，现有豁免 `injectionQuotedAsExample` 只认英文线索所以不生效。静态修法只有两条且都否决：加土耳其语线索＝拟合一个来源；按"文件自称防御/引用 OWASP"整体豁免＝作者可控的一行绕过。这一块归判官（它已分得清）。本 issue 其余部分（subagent/command/rule 文件不跑 EXEC/OBF/FS）不受影响，状态不变。

与 A.1（AST 检测）相关但不同：AST 提高的是"这段代码在干什么"的精度，这里需要的是"这段散文是在
教你做还是在教你别做"的精度。

**2026-10-09 追记（部分修复）**：slash command（`commands/*.md`，含 plugin 自带的 `commands/`）改为按 **kind** 取
`roleInstruction`，跑全量规则；`aguard check ~/.claude/commands/x.md` 单文件也按同样规则。只提这一种 kind 的理由：
command 的正文是"照这个步骤做"，和 SKILL.md 同一性质；上面列的三类误报（禁止、复述、笔记）在 command 里不是主流形态。
真机（25 skill / 6 plugin / 11 memory）与语料 before/after 均无新增误报（数字见该 PR）。subagent / rule / workflow /
output style / memory **没有一起提**，`TestScan_BenignProseIsNotFlagged` 继续钉住它们；修复方向不变。
