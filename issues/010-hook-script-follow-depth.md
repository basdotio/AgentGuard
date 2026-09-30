<!-- SPDX-License-Identifier: MIT -->
# 010 — hook/权限引用脚本只跟读一层

- **类别**：覆盖缺口
- **严重程度**：低
- **状态**：设计取舍（记录在案，非待修 bug）

## 问题描述

两处引用会被跟读并扫描：

- hook 命令指向本地脚本（`sh "$CLAUDE_PROJECT_DIR/.claude/hooks/pre.sh"`）；
- 权限授予点名本地脚本（`Bash(./scripts/deploy.sh *)`）——授权的窄度只等于脚本的行为。

但跟读**只有一层**：被跟读的脚本如果再调用第三个脚本，那一层不再跟进。
并且跟读**只在 HOME 之内**，越界的引用一律不读、报为 `COV-000` 覆盖缺口。

## 为什么这算取舍而非缺陷

- **HOME 边界是安全不变量**，不是限制：跨边界读文件会把扫描器本身变成一个
  任意文件读取原语。对抗测试对此有硬断言
  （`TestAdversarial_NoBoundaryEscape`，[cmd/aguard/adversarial_test.go:403](../cmd/aguard/adversarial_test.go)）。
  **这一半永远不应该"修"。**
- **深度为 1** 才是可讨论的：无限跟读会引入环、爆炸式读取与性能问题，
  且离"实际执行整条链"越近，越容易被诱导读到不该读的地方。

## 出处

- README "Coverage caveats"：`A hook script is followed one level and only inside your home directory.`
- ROADMAP "Known limitations" 第三条
- 实现：[internal/collect/hooks.go](../internal/collect/hooks.go)、
  [internal/collect/pathsafe.go](../internal/collect/pathsafe.go)、
  [internal/detect/pathsafe.go](../internal/detect/pathsafe.go)

## 真机发现：曾经有一条**假的**覆盖警告（已修），以及一个仍然存在的归因缺口

真机扫描（2026-08-13）的 60 条 warning 里 54 条是"hook 脚本未跟读"。抽查一条之后
发现**它是假的**：note 说 `$_R/scripts/smart-install.js` 没被扫描，而同一次扫描
在 `plugin:claude-mem` 名下报了 `EXEC-001 ×4`、`EXEC-009 ×4`，evidence 路径正是
`plugins/cache/thedotmack/claude-mem/10.5.2/scripts/smart-install.js`——**同一个文件**。

**根因**：plugin hook 通过 plugin 根指向脚本，而且几乎从不用扫描器能解析的写法：

```
_R="${CLAUDE_PLUGIN_ROOT}"; [ -z "$_R" ] && _R="$HOME/.../plugin"; node "$_R/scripts/x.js"
```

展开 `$_R` 需要解释 shell（而且上一句的 `[ -z ]` 兜底让 `_R` 有两个可能值，
选一个就是猜）。但"`scripts/x.js` 在这个 hook 所属的那棵 plugin 树里存不存在"
是个**文件系统事实**，不是猜。所以 `model.Hook` 加了 `OwnerRoot`，
`resolveInOwnerRoot` 用**最长后缀**在那棵树里查（后缀至少两段，否则光凭 basename
就能匹配到树里任何同名文件——那就又变成猜了；每个候选都过 `inBoundary`）。

**关键的设计选择：查到了也不重新读。** 先量过再定的：plugin 树本身就是一个产物、
被整棵扫过，在 hook 名下再读一遍只会重复计数——5 个 hook 都路由到同一个
`runner.js` 时，同样三行代码从 **9 条 finding 变成 29 条**，`EXEC-001` 从 1 变 6。
那正是 [008](008-monorepo-attribution.md) 拒掉的虚高，换了个门进来。

所以改成一条**新标题**的 note（`Hook script attributed to its plugin, not to the
hook`），单独成桶，说清楚：内容读过了，在 plugin 名下；**缺的是归因**。

**仍然存在的缺口就是这个归因。**"这个 plugin 里有个脚本 `curl | bash`"和
"这个 hook 每次匹配的工具调用都静默跑那个脚本"是两句不同的话，只有第二句能告
诉你频率。note 里点出了文件名，让人能手动接上，但工具没把它接上。

修这个不能靠"在 hook 下重读一遍"（上面量过了）。可行的方向是让 finding 带一组
**引用者**，报告层展示"这条 finding 所在的文件被以下 hook 调用"——即改展示，
不改计数。这会动 `model.Finding`，需要单独设计。

## 修复方向（若认为值得）

1. **有界加深**：把深度做成常量（如 2–3 层）+ 访问集合去环 + 总字节上限，
   保持 HOME 边界不变。收益是"脚本套脚本"这种最朴素的间接层被打穿。
2. 无论深度如何，**未跟进的引用必须继续产出 `COV-000`**——缺口可见是本项目的铁律。
3. 不建议做成可配置项：跟读深度影响 `overall`，配置化会破坏跨机器的可复现性
   （`overall` 需与 `--fail-on`、未来的链上认证保持一致）。

## 关联

- [006](006-plugin-hooks-coarse-audit.md)
