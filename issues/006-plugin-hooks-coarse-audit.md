<!-- SPDX-License-Identifier: MIT -->
# 006 — plugin 内置 hooks 未按 (event, command) 细审

- **类别**：覆盖缺口
- **严重程度**：中
- **状态**：**已修复**（`collectPluginHooks`，复用同一个 per-command 构造器）

## 问题描述

已安装 plugin 被当作**一棵树**整体扫描：它捆绑的 skills / commands / hooks 文本
**都会被读到**，规则也会命中。但 plugin 内的 hook **不会**像 `settings.json` 里的
hook 那样被按 (event, matcher, command) 拆开审计。

差别在于：

- `settings.json` 的 hook → 每条命令是一个独立产物，finding 能点名
  `PreToolUse[Bash]#1`，并且 `HOOK-001`（hook 命令里的 shell 串联/替换）这类
  **只对 hook 生效**的规则会跑。
- plugin 内的 hook → 只是树里的一段文本。定位粒度粗，hook 专属规则不生效。

设计规范（spec §4）要求做更细的拆分。

## 出处

- README "Coverage caveats" 第一条
- ROADMAP "Known limitations" 第二条
- 已实现的对照：[internal/collect/hooks.go](../internal/collect/hooks.go)、
  [internal/detect/hooks.go](../internal/detect/hooks.go)（per-command 产物 + `HOOK-001`）
- plugin 收集：[internal/collect/plugins.go](../internal/collect/plugins.go)

## 影响

plugin 是"一次安装、批量引入外部内容"的渠道，风险面比单个 skill 大。
hook 又是最危险的表面之一（静默执行 shell）。两者交叉处却是最粗的审计粒度。

## 修复方向

复用已有的 per-command hook 产物构造逻辑：在 plugin 树收集阶段识别其 hook 声明
（plugin 的 hooks 配置文件），走 `collect/hooks.go` 同一条路径产出 (event, matcher,
command) 产物，这样 `HOOK-001`、hook 脚本跟读、`LLM-008` hook 能力检查都自动生效。

关键是**不要重写一套**：让 plugin hook 与 settings hook 共用同一个产物构造器，
否则两边规则会随时间漂移。

## 关联

- [010](010-hook-script-follow-depth.md)（hook 脚本跟读深度）

## 修复记录

`internal/collect/plugins.go` 的 `collectPluginHooks`：读 plugin 自带的 hook 声明，走
**`collectHooks` 同一条路径**产出 (event, matcher, command) 产物。

**共用构造器是重点而不是省事**：两套构造器一定会漂移，而没人看的那套就是 plugin 那套。
共用之后 `HOOK-001`、hook 脚本跟读、judge 的 hook 能力检查全部自动生效，finding 也从
"这个 plugin 里某处"变成 `hook:PreToolUse[Bash]#1 (plugin acme@mk)`。

实测确认（之前 plugin 内的 hook 完全不跑 hook-only 规则）：

```
🔴 high    [EXEC-001] plugin:acme@mk (1.0)
🔴 high    [EXEC-001] hook:PreToolUse[Bash]#1 (plugin acme@mk)
🟠 medium  [HOOK-001] hook:PreToolUse[Bash]#1 (plugin acme@mk)   ← 新增
```

### 三个实现判断

1. **两种布局都接受**：`hooks/hooks.json`、`hooks.json`、`.claude/settings.json`（嵌在
   `hooks` 键下）。只试一种会让用另一种的 plugin **静默不被审计**——正是本条要关的失败模式。
2. **不可解析的 hook 文件要通报**（`PARSE-000`）而不是跳过。"plugin 声明了 hook 但我们读不了"
   是覆盖缺口；静默会让它看起来像"这个 plugin 没有 hook"。
3. **hook 命令会被报两次是刻意的**——一次在按文本扫描的 plugin 树里，一次作为独立产物。报告按
   (产物, 规则) 聚合，所以那是两行、说的是不同粒度的两件事，第二行才是可操作的。要消掉第一行
   就得在扫树时排除 hook 文件，那会让**树扫描依赖 hook 收集成功**——一次解析失败就从"报两次"
   变成"完全不报"。**往重复的方向错是安全的那一侧。**

### 仍未做的（本条只覆盖 hooks）

plugin 自带的 **skills / MCP 配置**仍然只作为树里的文本被读，没有按自己的 kind（commands 自 2026-10-09 起在树里按指令角色跑全量规则，但仍不是独立产物；MCP 配置自 `collectPluginMCP` 起已是逐 server 产物）
重新收集。spec §4 要求那个更细的归因。hooks 先做是因为它是最危险的表面（静默执行 shell），
而且 per-command 构造器已经现成。
