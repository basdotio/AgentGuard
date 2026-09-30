<!-- SPDX-License-Identifier: MIT -->
# 019 — 软链安装的 skill 只能报告，不能隔离（R31b 否决）

- **类别**：功能范围（已否决）+ 已修复的报告缺陷
- **严重程度**：中（报告缺陷部分）
- **状态**：**部分修复** —— 报告缺陷已修复；隔离能力**不规划**（复合状态：一半修了，另一半是决定不做，理由见 §3）。

> 这条 issue 有两半。第一半是一个**当时就错的**缺陷，已修。第二半是
> `prd-selective-clean.md`(文件未入库) 的 **R31b**，纸面上很有道理，
> 套上"隔离物仍要计分"那条不变量之后过不去。**将来又想让软链装的 skill 可隔离时，先读 §3。**

## 1. 现象（实测）

造两个软链安装的 skill：

```
skills/x -> ../shared/x                  真身在 root 内，但不在 skills/ 下
skills/y -> ../../agents-elsewhere/y     真身在 root 外（主流安装方式）
```

修复前：

```
Cleanup: 2 item(s), 1 executable          ← x 被算成"可执行"
  · Z-c3765e73 [zombie/A1] x               ← 没有任何 blocker
  · Z-5c0b79f8 [zombie/A1] y
      blocked: target-outside-root

--apply --dry-run:
skip x: not an installed skill (shared/x); only skills/ is quarantined   ← 到这才拒
```

**计划说能动，执行才拒。** 根因是两道检查问的不是同一个问题：计划期只问
`withinDir(root, path)`，执行期的 `quarantinable` 还要求 root 相对路径第一段是 `skills`。

`Blockers` 这个字段存在的**全部理由**就是"动不了要在还能改主意的时候看得见"。这里它没做到，
而且失败方向最差：清单把一件做不到的事标成可做的。

## 2. 已做的修复

`quarantinable` 改为返回 `(blocker, why)`，并以 `clean.QuarantineRefusal` 导出；
`hygiene.moveBlocker` **调用它**，而不是另写一份判断。

**刻意不复制条件**（CLAUDE.md 的老规矩：漂移要在结构上不可表达）。计划期的答案现在**就是**
执行期的答案，连句子都一样：

```
· Z-c3765e73 [zombie/A1] x
    … Cannot be moved: not an installed skill (shared/x); only skills/ is quarantined.
    blocked: target-not-under-skills
```

blocker id 给脚本，句子给人。只印 id 等于把解释藏在"跑一遍那条注定失败的命令"后面。

新增的 blocker 常量都在 `model`：`target-not-under-skills`、`target-is-security-config`、
`already-quarantined`、`target-unlocatable`。钉住它的测试是
`TestZombie_SymlinkOutOfSkillsIsBlockedInThePlan`（在旧行为上验证过会红）。

## 3. 为什么不做"可隔离"——两条路都不通

### 3.1 R31b 字面版：软链 + 真身一起搬

PRD 原文是"入口为软链时，manifest 须同时记录软链本身，apply 时把软链一并移入 trash"。

**真身不归 `clean` 管。** `skills/x -> shared/x` 的真身在 `shared/`，
`skills/x -> plugins/foo/skills/x` 的真身在插件树里，`skills/y -> ~/.agents/skills/y` 的真身
根本不在 root 内。搬走它等于从一个 `clean` 不拥有的面里挖内容 —— 而"只动 `skills/`"是这个命令
最外层的边界。root 外那种（**主流安装方式**）连搬都搬不了。

### 3.2 更好的变体：只搬软链——**它撞上"隔离物仍要计分"**

只把 `skills/x` 这个软链移进 trash，真身不动。听起来完全正确：加载路径没了，agent 就加载不到了，
而且 root 外的安装方式也照样处理得了。

**它过不了 `collectQuarantine` 那条不变量。** 那个收集器存在的理由写在它自己的注释里：

> 隔离一个已知恶意 skill 曾把环境从 69/100 变成干净的 100/100，而文件就躺在配置目录里——
> 这让 `clean --apply` 成了把红色 `--fail-on high` 变绿的最短路径。

只搬软链之后：

- trash 里那个条目是**软链**。`collectQuarantine` 用 `os.Stat` 跟进去 —— 而**相对**软链
  （`../shared/x`）换了一层深度之后指向就变了或者直接悬空，`os.Stat` 失败 → 静默 `continue`
  → **不计分**。
- 真身留在 `shared/`，那是个顶层目录，`unowned.go` **有意不读**（依据同 `ExcludeFromHash`：
  没人引用的树是惰性的）。

**净效果：`clean --apply` 让分数变好。** 正是那条不变量防的事，而且这次是静默的。

**"把 trash 里的软链改写成绝对路径"能修计分，但要付两笔账**：

1. 那不再是 `os.Rename`，而是"新建一个 + 删掉原来那个" —— **`clean` 永不删除**是头条不变量。
2. 原来那条软链的**相对写法丢了**。undo 放回去的是一条绝对软链，跟操作者原来写的不是一个东西。
   一个自称"完全可逆"的命令，不能悄悄改写它放回去的东西。

### 3.3 还撞上 undo 的检查 1b

```go
case !withinTrash(trash, r.To) || isSymlink(r.To):
    // 一条被追加的行 + trash 里一个指向别处的软链 = 把 undo 变成任意文件移动
```

**trash 里的东西是软链就拒绝恢复。** 所以"隔离一个软链"产出的正是 undo 拒绝放回的东西。
要放开就得让 undo 分辨"本来就是软链的条目"和"路径被换成了软链" —— 而 manifest 是数据不是权威，
这个分辨只能从文件系统重新推导，推导得到的信息恰好不足以区分这两者。

## 4. 结论

对软链安装的 skill，`clean` **兑现不了自己的承诺**：

> 内容仍在你的配置目录里、仍被扫描、仍计分；删除是你自己的动作。

指针搬得走，内容搬不走；而只搬指针会让内容脱离计分。所以 **report-only 是对的**，
要修的是"报告要诚实"—— 那部分已经落地（§2）。

## 5. 什么会改变这个结论

只有一件：**trash 能携带出处，并且 `collect` 能在不信任 manifest 的前提下验证它。**
那样一条被隔离的软链就能被计分到它原来的真身上，3.2 的洞才补得上。
今天没有这样的机制，发明一个的代价远大于"软链装的 skill 不能一键隔离"这个不便。

## 6. 关联

- `prd-selective-clean.md`(文件未入库) R31 / R31b / V12
- [`clean-internals.md`](../docs/internals/clean-internals.md) §3.2（两个谓词）、§11（不变量表）
- 实现：[`internal/clean/paths.go`](../internal/clean/paths.go) `quarantinable` /
  `QuarantineRefusal`、[`internal/hygiene/hygiene.go`](../internal/hygiene/hygiene.go) `moveBlocker`
- 同类"实测否决"的先例：[`018`](018-clean-autoapply-rejected.md)
