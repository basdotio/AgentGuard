<!-- SPDX-License-Identifier: MIT -->
# 022 — macOS 上 CLI 装不上插件：插件名与 marketplace 名只差大小写（上游 bug）

- **类别**：分发缺陷（根因在 Claude Code，触发条件在本仓库的命名）
- **严重程度**：高 —— 默认 macOS 上，README 写的那条安装路径（`/plugin install agentguard@AgentGuard`）走不通
- **状态**：**未修复** —— 修法实测可行（§4），但要改用户可见的插件名，还没人拍板。上游 bug 报告**尚未提交**。

> 2026-09-30 在 v0.16.0 发版后的真机换装中发现。**要改插件名、marketplace 名或仓库名之前先读 §3 和 §4**：
> 三个名字互相卡着，能动的只有一个。

## 1. 现象（实测）

Claude Code 2.1.107，macOS APFS（默认**不区分大小写**：`cache/agentguard` 与 `cache/AgentGuard` 是同一个 inode）。
在隔离的 `CLAUDE_CONFIG_DIR` 里，用本仓库 `.claude-plugin/marketplace.json` + `plugin/` 的本地副本当 marketplace：

```
$ claude plugin marketplace add <本地副本>
✔ Successfully added marketplace: AgentGuard
$ claude plugin install agentguard@AgentGuard
✘ Failed to install plugin "agentguard@AgentGuard": EINVAL: invalid argument,
  rename '<cfg>/plugins/cache/agentguard' -> '<cfg>/plugins/cache/AgentGuard/agentguard/0.16.0'
```

- `installed_plugins.json` **没有生成**，插件没有登记 —— 这不是误报，是**真装不上**。
- 留下一个装了一半的 `cache/agentguard/`：插件文件平铺在顶层，外加一个空的 `agentguard/` 子目录。
- 再装一次，结果完全一样。

维护者自己的机器上同一条命令也报这个错，但插件**最后是登记上了的**（`agentguard@AgentGuard` 0.16.0，
安装目录与仓库 `plugin/` 的 canonical 哈希一致）。当时有另外三个 Claude 会话在跑（`.in_use` 里三个 pid），
推测是它们走别的路径补装的 —— **未查实**。不要把"我这台能用"当成"这条路能用"。

## 2. 根因（读 Claude Code 2.1.107 内嵌的 JS 得出）

安装分两步：

1. 把插件放进暂存目录 `cache/<plugin.json 的 name>`（非 `[a-zA-Z0-9-_]` 字符替换成 `-`）；
   **这个目录已存在就先 `rm -rf`**。
2. 挪到版本化位置 `cache/<marketplace>/<插件>/<版本>`。代码**专门处理了**"版本化位置在暂存目录里面"的情况 ——
   先 rename 到旁边的临时目录再挪进去 —— `everything-claude-code@everything-claude-code`（插件名与 marketplace 名
   **完全相同**）就是靠这段装上的。**但这个判断是区分大小写的字符串 `startsWith`**：
   `cache/AgentGuard/agentguard/0.16.0` 不以 `cache/agentguard/` 开头，判断落空，直接 rename，
   在不区分大小写的卷上就是"把目录挪进它自己里面" → `EINVAL`。

第 1 步那个 `rm -rf` 更糟：在不区分大小写的卷上，`cache/agentguard` **就是** `cache/AgentGuard`，
所以每次安装/更新都会先把整个 marketplace 缓存目录删掉，包括正在被会话使用的已装版本。
这一点是**读代码推出来的，没有单独实测"已装版本被删"**。后果推论：一台靠别的路径装上了的机器，
下次自动更新到新版本时，会先删掉现有安装、再在同一处失败。

## 3. 为什么不改 marketplace 名

桌面版在 Customize 里加 marketplace 时，**按仓库名**记 marketplace 名；点 Update 时拿这个名字让内置 CLI
刷新，而 CLI 按 `marketplace.json` 声明的名字注册。两个名字不一致，更新就是 `NOT_REGISTERED`
（见 `.claude/rules/plugin.md`，2026-09-04 真机确认）。所以 marketplace 名必须等于仓库名 `AgentGuard`。
改仓库名代价远大于改插件名。**能动的只有插件名。**

## 4. 修法实测（同一台机器、同一个卷，隔离配置）

| 组合 | 首次安装 | 再装一次 |
|---|---|---|
| A 现状：marketplace `AgentGuard`，插件 `agentguard` | ❌ `EINVAL`，未登记 | ❌ |
| B 插件改名 `aguard` | ✅ 装在 `cache/AgentGuard/aguard/0.16.0` | ✅ |
| C 插件改名 `AgentGuard`（与 marketplace 名完全相同） | ✅ 走第 2 步的特判分支 | ✅ |
| D 只改 `plugin.json` 的 name（`agentguard-plugin`），marketplace 条目仍叫 `agentguard` | ✅ ID 仍是 `agentguard@AgentGuard` | — |

**D 看起来零迁移，其实不能用。** 命名空间（`agentguard:aguard-scan` 冒号前那半）取的是 **`plugin.json` 的
name**（Claude Code 加载插件时 `{name: manifest.name, ...}`），所以 D 照样改掉了用户看到的前缀；而闸门
`gate.ResolveSkill` 拿命名空间去查 `collect.PluginPaths`，后者的 key 是**插件 ID 里 `@` 前那半**。
D 让这两个名字分开，每个带命名空间的 skill 在闸门里都会变成 `GATE-000`。要改就两处一起改成同一个名字。

**C 能用但最不稳**：它依赖那段特判分支，而且第 1 步的 `rm -rf` 删的正好是整个 marketplace 缓存目录
（`everything-claude-code` 每次更新也是这样），正在跑的会话会在更新瞬间丢掉插件文件。

**B 是推荐方向**，名字的候选：

- `aguard`：与二进制同名，一眼不会和 `AgentGuard` 混；命令变成 `/aguard:aguard-scan`，skill 变成 `aguard:agentguard-audit`。
- `agent-guard`：保留品牌，但 `agent-guard@AgentGuard` 像打错字。
- **不要用 `guard`**：旧的 `basdotio/guard` marketplace 就叫 `guard`，还没删掉它的机器上 `cache/guard` 正是那个
  marketplace 的缓存目录，第 1 步会把它整个删掉 —— 同一个 bug 换个名字再踩一次。

## 5. 改名的代价（决定之前要看清）

- 插件 ID 和命名空间都变。已装 `agentguard@AgentGuard` 的人（桌面版用户、区分大小写文件系统上的 CLI 用户、
  靠别的路径装上了的 macOS 用户）在 marketplace 里再也找不到旧名字，要重装一次。
- `aguard version` 的 `updateHint`（`cmd/aguard/version.go`）目前只按 `pluginBundleName` 查一个名字；改名后要
  **同时认旧名**，否则旧安装连提示都收不到。而它现在给出的切换命令 `claude plugin install agentguard@AgentGuard`，
  在默认 macOS 上正好撞这个 bug。
- 要跟着改：`plugin/.claude-plugin/plugin.json`、`.claude-plugin/marketplace.json` 的条目名、`pluginBundleName`、
  `TestMarketplaceEntryVersionMatchesPlugin`、README（双语）、`plugin/skills/agentguard-audit/references/install.md`、
  `.claude/rules/plugin.md`、spec 里写到插件 ID 的地方。版本号要升到 0.17.0，CHANGELOG 写迁移步骤。

## 6. 什么会让这条不必修

上游把第 2 步的包含判断改成按真实路径比较（`realpath` 后比较，或在不区分大小写的卷上忽略大小写），
并且第 1 步不再按名字盲删。那样现有名字就能装，`agentguard@AgentGuard` 不用动。
**上游修好之前，改名是本仓库唯一能单方面做的事。**

## 7. 关联

- 上游：anthropics/claude-code（bug 报告未提交；2026-09-30 按 "EINVAL rename plugins cache" 搜过，没有现成 issue）
- 桌面版 marketplace 名的约束：[`.claude/rules/plugin.md`](../.claude/rules/plugin.md)
- 升级提示：`cmd/aguard/version.go` `updateHint`；闸门解析：`internal/gate/resolve.go` `ResolveSkill`
