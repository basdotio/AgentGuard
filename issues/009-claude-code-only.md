<!-- SPDX-License-Identifier: MIT -->
# 009 — 仅支持 Claude Code，多平台未抽象

- **类别**：平台限制
- **严重程度**：低（当前定位内不是缺陷；一旦要扩平台就是最大的结构性障碍）
- **状态**：未修复
- **2026-09-14 更新**：不再是"未规划"。配置层（`~/.cursor/mcp.json`、`~/.codex/config.toml`、
  `~/.gemini/settings.json`）与制品层（条件性）已按本条"修复方向 1"的思路（collect 加平台适配层）
  排进后续阶段，见 ROADMAP。

## 问题描述

收集层的布局标记是**Claude 专属且刻意如此**（`skills/`、`settings.json`、
`plugins/installed_plugins.json`、`CLAUDE.md`、`.claude` 根）。没有平台抽象层，
所以要支持 Cursor / Windsurf / 其他 agent 环境，需要改动的不是加一个 collector，
而是整条 collect 路由。

detect / score / report 三层基本与平台无关（它们只吃产物），**collect 是唯一的耦合点**——
这是好消息，说明抽象成本可控。

## 出处

- ROADMAP "Known limitations" 最后一条：`Claude Code only (multi-platform not yet abstracted).`
- 路由注释：[internal/collect/collect.go:52](../internal/collect/collect.go)
  （`Every marker here is CLAUDE-SPECIFIC, and deliberately so.`）

## 影响

产品层面的天花板：AI agent 生态正在多点开花，"agent 杀毒"如果只覆盖一家客户端，
可服务范围受限。但过早抽象会拖慢当前的检测能力建设。

## 修复方向（若立项）

1. 定义 `Platform` 接口：给定一个 root，返回产物列表（skills / MCP 配置 / hooks /
   权限声明 / 指令文件）。让现有 Claude 实现成为它的第一个实例。
2. 按平台差异建映射表，而不是在代码里分支：各家的目录名、配置文件名、hook 机制
   都不同，但**产物种类**（`model.Kind`）高度重合，映射能吃下大部分差异。
3. 规则层需要标注"哪条规则依赖平台语义"——例如 `HOOK-001` 依赖"hook 会静默执行 shell"
   这一 Claude 语义，换平台前要确认前提仍成立。
4. 建议**等 A.1（AST）与 005 落地后再动**：先把单平台的检测深度做扎实，再横向扩展。
