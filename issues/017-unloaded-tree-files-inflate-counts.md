<!-- SPDX-License-Identifier: MIT -->
# 017 — plugin 树里"永不加载"的文件参与打分，把计数按语言副本翻倍

- **类别**：归因 / 覆盖边界
- **严重程度**：中（误报 + 计数虚高，不影响正确性）
- **状态**：未修复

## 怎么发现的

真机扫描（`~/.claude`，2026-08-13）的一组 `EXFIL-001`：`×21 in 21 files`，前三条 evidence 是

```
.../1.8.0/.agents/skills/backend-patterns/SKILL.md:361
.../1.8.0/.github/workflows/monthly-metrics.yml:40
.../1.8.0/docs/ja-JP/skills/backend-patterns/SKILL.md:350
```

`EXFIL-003` 那组四条更直白：`.agents/skills/security-review/SKILL.md`、
`docs/ja-JP/skills/security-review/SKILL.md`、`docs/zh-CN/skills/security-review/SKILL.md`
——**同一份文档的三个语言版本 + 一份原文**，算成 4 条 finding。

这是 [008](008-monorepo-attribution.md) 那次改动（组里报"跨几个文件"）**自己暴露出来的**：
`×21` 单看无法分辨，`in 21 files` 加上三条 evidence 路径之后，一眼就能看出
`docs/ja-JP/` 和 `docs/zh-CN/` 是同一份东西。

## 两类都不该参与打分的文件

**1. 语言副本。** `docs/<locale>/skills/*/SKILL.md` 是文档翻译。Claude Code 只加载
plugin 根下的 `skills/*/SKILL.md`，`docs/**` 里的副本**永远不会进入任何会话**。
一份文档写了 N 种语言，就把同一个事实报 N 次。

**2. plugin 自己的仓库基础设施。** `.github/workflows/monthly-metrics.yml` 读一个
secret 再调一次 API——这是 plugin 仓库的 CI，在**GitHub 上**跑，不在你机器上跑，
也不是 agent 面。`tests/lib/package-manager.test.js`（`FS-004` ×5）、
`tests/lib/session-aliases.test.js`（`OBF-005`）、
`superpowers/tests/brainstorm-server/server.test.js`（`EXFIL-001`）同理。

两类的共同点：**它们在树里，但不在加载面上。**

## 为什么现在会读它们

`collectPlugins` 把整个 plugin 树当**一个产物**交给 detect，detect 读树里所有
文本文件（见 `readTextTree`）。这在 skill 上是对的——skill 的脚本和资源确实
会被用到——但 plugin 树里塞了大量非加载面的东西。

## 修复方向

1. **先量再改，且要量真机。** 这条 issue 的整个证据来自一次真机扫描，容器语料里
   没有多语言 plugin。任何"按路径排除"的规则都必须先在真机上量出它挡掉多少、
   漏掉多少。参考 [011](011-prose-instruction-exec-underscan.md) 那次 100→69 的教训。
2. **候选 A：按加载面裁剪 plugin 树。** 只把 plugin 清单声明的加载点
   （`skills/`、`commands/`、`agents/`、`hooks/`、`.mcp.json`）当产物内容，其余
   （`docs/`、`tests/`、`.github/`、`.cursor/`、`.opencode/`）降级为**通报但不打分**
   ——和 `COV-000` 对第三方树的处理同一个口径。
   **风险**：加载点清单是手维护的，会漂移。参考 `rootOwned` 那次两度漂移的教训，
   必须配一个用真实 plugin 结构构造的 fixture 测试。
3. **候选 B：按内容去重。** —— **注意：这一半已经在跑了。** `readTextTree`
   （`internal/detect/detect.go:590-591`）自 2026-07-23 起（比本 issue 建档还早
   三周）就按**文件内容哈希**去重，注释里写的就是"collapses multi-adapter mirror copies
   like .agents/.cursor/…"。它对**逐字镜像副本**有效，对 `docs/ja-JP/`、`docs/zh-CN/` 这类
   **翻译副本**无效（译文内容不同）。照本条原文动手会重复实现一遍。原描述：
   同一 finding（规则 + 归一化后的行）在多个文件里出现时
   折叠成一条，evidence 列全部路径。这对语言副本很准（译文的代码块逐字相同），
   但**对基础设施文件无效**，且会掩盖"两个不同文件真的各有一个问题"。
4. **不要拆产物。** 见 [008](008-monorepo-attribution.md)：分数是按产物平均的，
   拆完会虚高（实测 86→97）。（P-044 把插件的 skill/命令/子 agent 拆成了各自的产物，但插件和它们是**一个计分单元**，
   所以不虚高；而且子项只采 Claude Code 会加载的那份，`docs/<语言>/`、`.agents/` 副本不采。树本身照旧读全部文件，本条未变。）

## 关联

- [008](008-monorepo-attribution.md)（本条是它的报告改动暴露出来的）
- [006](006-plugin-hooks-coarse-audit.md)（plugin 树按一个产物读，是同一个根因）
- plugin 收集：[internal/collect/plugins.go](../internal/collect/plugins.go)
- 树读取：[internal/detect/detect.go](../internal/detect/detect.go)（`readTextTree`）
