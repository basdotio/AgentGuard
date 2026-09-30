<!-- SPDX-License-Identifier: MIT -->
# 012 — clean 的覆盖范围与剩余缺口

| 项 | 值 |
|----|----|
| 类别 | 功能范围 |
| 严重程度 | 中 |
| 状态 | 部分修复 |

## 问题描述

`clean` 目前**只处理 skill**（`internal/hygiene/hygiene.go` 的
`if a.Kind != model.KindSkill { continue }`）。scan 已经采集并计分的其它面——rules、workflows、
output-styles、auto memory、导入链、项目级 MCP——一个都不在 clean 的视野里。

PRD 里设计的**逐项选择**当时只落到了数据层：`CleanItem` 有稳定 ID 且会打印出来，但 CLI 还没有

> **2026-09-14 勘误**：这一句已经过时，而且和 `issues/020` 互相矛盾。`clean --ask` 已经存在
> （`cmd/aguard/main.go:730`，配五条互斥约束），它经历了完整的"撤回 → 重做"周期，过程记在
> `issues/020`。**020 记录了 012 的修复，却没人回来改 012** —— 这正是 `README.md` 里那条
> 同步约定对非"检测绕过"类失效的后果。本条其余 9 项缺口仍然成立，所以状态是"部分修复"而不是关闭。
`--apply-items <ids>`，`--apply` 仍是"移走全部僵尸"的全量操作，也没有交互勾选。

## 已经补上的（不再是缺口）

- 隔离项重新计分：`.aguard-trash` 作为 `KindQuarantined` 采集并扫描，`clean --apply` 不再能把
  红色分数洗白（`TestScan_QuarantinedContentStillScores`）
- manifest（写前日志、0600/0700 权限）、`--undo <batch|last>`、O_EXCL 排他锁
- undo 的三重授权：root 内封闭、安全配置永不作为恢复目标、隔离内容哈希一致性
  （`TestUndo_RefusesPoisonedManifest`、`TestUndo_RefusesSwappedContent`）
- 退出码 3 表示"部分跳过"，被阻塞项逐条点名
- zombie 数据源改为 history.jsonl + session transcript，按证据强度给出 low/medium 置信度

## 仍然缺的

1. ~~**逐项选择的 CLI**~~ —— **已完成**，见上方勘误与 `issues/020`。
2. **非 skill 面的清理**：rules/workflows/output-styles/memory 的重复、膨胀、失效检测。
3. **启动上下文预算**：把每次会话自动加载的项按 token 成本列账（纯读）。
4. **C 级内容瘦身**：`context_bloat` 有量化、有 ID，但没有执行器（blocker
   `content-edit-unimplemented`）；落地前需要"只删不加 + 结构完整性断言"。
5. **`.aguard-trash` 自身没有保留策略**——它是我们发明的目录，Claude Code 的
   `cleanupPeriodDays` 不会碰它，所以我们自己成了一个新的增长源。
6. **祖先 `.claude` 的 project skills**：实测发现 project 作用域会**向上**找祖先 `.claude`
   （cwd 在 `<x>/trashlab/proj`，解析出的 project skills 路径是 `<x>/.claude/skills`）。
   `--root <project>/.claude` 的审计会漏掉那次会话真正会加载的祖先 skill。
7. **`skills/synced/` 是 sync 拥有的**：实测看到它被自动解压重填。隔离那里的内容会被下次同步
   还原，所以应当**拒绝**而不是尝试。
8. **运行中的会话会实时看到变更**：实测 Claude Code 监视 `skills/`、`commands/`、`agents/`。
   我们的锁只保护两个 aguard 之间，不保护 aguard 与正在运行的 agent 之间。
9. **output-style 悬空引用**：被 settings 选中的 style 一旦被隔离，settings 里就剩一个悬空
   引用。缺少配置完整性检查。

## 已实测（原第 6 条已关闭）

见 `docs/internals/measurement-startup-loading.md`（Claude Code 2.1.229，隔离 CLAUDE_CONFIG_DIR，抓取
出站请求体 + strace + `--debug`）。

- **A 级隔离的安全前提成立**：`<config-root>/.aguard-trash/` 下的 skill、rule、subagent、
  output-style、CLAUDE.md、MEMORY.md 六个探针，**一个都没有被打开，一个都没有进 system prompt**。
- **但安全来自"位置"，不来自"点开头"**：`rules/`、`agents/`、`commands/` 是**递归**扫描且
  **不跳过隐藏目录**——`rules/.aguard-trash/x.md` 照样进 system prompt，不带点的
  `rules/aguard-trash/x.md` 也一样。所以 `--root` 指到这些目录里，隔离就会失效，而报告仍然说
  "已隔离"。已加硬拒绝（`collect.QuarantineUnsafe` + `TestApply_RefusesRootInsideALoadedTree`），
  dry-run 也拒绝。
- **`skills/.dotskill/SKILL.md` 是一个活的 skill**（打开 9 次，description 进了 system prompt）。
  debug 日志只说点开头目录"不会被当作 plugin"——那是另一条规则，看着像保护其实不是。所以
  collector 必须继续走隐藏目录（`TestCollectSkills_DotPrefixedDirIsStillLoaded`）。
- **workflows 不是启动加载的**：目录被枚举（供 `/<name>` 补全），文件体到调用时才读。之前把它
  算进报告的 `Auto-loaded:` 行高估了环境的常驻成本，现已单列 `On-demand:`。
- **auto memory 未复现**：`projects/<p>/memory/MEMORY.md` 完全没被访问。所以下面"唯一的真空是
  auto memory"这句**尚未实测**，不能当作已确认引用。可能需要某个设置、需要该项目已有历史，
  或者只在交互模式生效。

## 与第一方能力的边界（重要）

Claude Code 自己已经管理运行时数据的增长：`cleanupPeriodDays`（默认 30 天）在启动时按龄删除
transcripts、file-history、plans、debug、各类 cache、tasks、shell-snapshots、backups 等；
`claude project purge` 按项目删除状态，带 `--dry-run` 计划、逐项确认、`--all`、`-i`。

因此 aguard **不做**运行时数据的保留策略：重做只会更差，并引入第二个删除来源。唯一的真空是
**auto memory**——它不在自动老化清单里，只有显式 purge 才删。**注意：它"每会话加载"这一点在
2.1.229 的 `-p` 模式下没有复现**（见上），所以"真空"目前只成立在"不会被自动老化"这一半上；
"每会话都花 context"那一半待重测。无论如何对它的处理都应当是"只报不删"。
