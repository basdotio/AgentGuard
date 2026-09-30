<!-- SPDX-License-Identifier: MIT -->
# 013 — clean 写盘路径的对抗审计（10 项，全部已修）

| 项 | 值 |
|----|----|
| 类别 | 安全缺陷 |
| 严重程度 | 严重（其中 1 项 CRITICAL，3 项 HIGH） |
| 状态 | 已修复，全部有回归测试 |

## 根因（一条，不是十条）

十个发现共用一个模式：**判断是在"写出来的路径"上做的，而不是"解析后的路径"上做的**。
一个符号链接或一个相对路径就让写出来的形式说谎。修复统一为三条规则，写在
`internal/clean/paths.go` 的文件头注释里：

1. **先解析再判断** —— `filepath.Abs` 然后 `EvalSymlinks`；目标路径还不存在时解析到最近的存在祖先。
2. **按文件系统的方式比较路径段** —— macOS 大小写不敏感，Windows 忽略尾部点和空格，而这个二进制要交叉编译到两者。
3. **名字是标签，永远不是地址** —— 叫 `synced/websearch` 的 skill 不能左右它的隔离副本落在哪。

第二条结构性修复：**Apply 的许可集在构造上是 Undo 可恢复集的子集**。两者曾各判各的，
缺口正好是"绝不能移动"的那棵树。

## 发现与修复

| # | 严重度 | 缺陷 | 修复 | 回归测试 |
|---|---|---|---|---|
| F1 | CRITICAL | `Undo` 只校验行的 `from`，`to`（rename 的**来源**）仅做 root 封闭检查 → 一行追加的 manifest 就能把 `settings.json` / 整个 `hooks/` 移走并把内容种到 `rules/` 下，报告 `restored`、exit 0。`--undo last` 也被劫持（`restorable` 取文件里最后出现的批次） | 来源必须在隔离目录内且不是符号链接；`protected` 同时校验两端 | `TestUndo_RefusesSourceOutsideTrash` |
| F1b | CRITICAL | `hash:""` 静默关闭身份校验，攻击者只要不写这个字段 | 空哈希直接拒绝；Apply 也拒绝写出无哈希的记录 | `TestUndo_RefusesRowWithoutHash` |
| F2 | HIGH | `QuarantineUnsafe` 判断未解析的原始 `--root`，三种绕过：`--root .`（`Clean(".")=="."`，没有段可匹配）、root 是指向 `rules/` 的符号链接、`Rules`/`rules.` 大小写与尾部标点 | Abs + EvalSymlinks 后再切段，段名折叠大小写与尾部 `. ` | `TestQuarantineUnsafe`（collect）+ E2E |
| F3 | HIGH | `.aguard-trash` 是符号链接时，`MkdirAll`/`Rename` 都跟着走 → 隔离物离开被扫描的树，`collectQuarantine` 静默丢弃它，分数 50→100、`--fail-on high` 1→0，而输出还印着"仍在配置根内，所以仍然计分" | `safeTrash` 拒绝符号链接、校验解析后仍在 root 内且不在加载树内；`collectQuarantine` 丢弃时发 high 级 `COV-000` | `TestApply_RefusesSymlinkedTrash` |
| F4 | HIGH | `Apply` 完全没有 `protected` 检查 → `skills/x → <root>/hooks` 的安装符号链接让 apply 搬走整个 hooks 树，而 undo 拒绝放回（不可逆） | `quarantinable()`：同一个 `protected` + 来源必须在 `skills/` 下 | `TestApply_RefusesSourceOutsideSkills` |
| F5 | MEDIUM | `reportPending` 在"无目标"的提前返回**之后**才调用，而崩溃后 `skills/` 里正好没东西 → 唯一需要这条警告的时刻它被抑制；`Undo` 也不报，回答"没有待恢复的批次"、exit 0 | 提前到返回之前；`Undo` 点名半成品并返回 exit 3 | `TestUndo_SurfacesInterruptedMove` |
| F6 | MEDIUM | `synced/<skill>` 的斜杠名直接 join 到隔离路径 → 父目录没人建，rename 永远 ENOENT，且发生在写完意图行之后；每次运行多一条假的"上次被中断"警告 | `trashName()` 压成单个路径段；rename 失败写 `failed` 行关闭意图 | `TestApply_FlattensNameWithSeparator`、`TestApply_FailedMoveDoesNotLookLikeACrash` |
| F7 | MEDIUM | `protected` 匹配未解析路径而封闭检查解析了 → `<root>/hk → hooks` 两边都过，undo 装上了任意 pre-tool-use hook | `protected` 先解析再判段 | `TestProtected_ResolvesSymlinkedAlias` |
| F8 | LOW | `os.OpenFile` 跟随符号链接 → 符号链接的 manifest 让每次 apply 往 root 外的文件追加行 | `openManifest` 拒绝非普通文件 | `TestManifest_RefusesNonRegularFile` |
| F9 | LOW | `--apply --undo` 同时给出时静默丢掉 apply | `MarkFlagsMutuallyExclusive` | CLI E2E |
| F10 | LOW | apply 时的"内容变更"检查比较的是同一个值的两个副本（`hygiene.Analyze` 从同一个 artifact 填 locator 哈希），分支在真实路径上永不触发；真正的窗口 scan→rename 无人看守 | 从磁盘重新计算哈希再比 | `TestApply_SkipsWhenContentChanged`（现在测的是真实路径） |

另外补上：`dry-run` 校验隔离地址但**不创建**它（`TestApply_DryRunCreatesNoTrashDir`）；
`pathByName` 查不到名字时不再静默丢弃（会点名并计入 Skipped）。

## 审计过但没打穿的（保留记录，避免重复投入）

`--undo` 的 root 逃逸（绝对路径、`../`、活/悬空符号链接、`to` 在 root 外，六种变体全被拒）；
覆盖已存在文件（`os.Lstat(r.From)` 守卫在所有变体下都成立）；构造受保护的 basename；
并发 apply（O_EXCL 锁在 8 个 skill 下无交错、无丢失、无 `.1` 冲突）；
`--dry-run` 纯净性；正常隔离洗白分数（50→50，四条 finding 全保留）；
非空但错误的哈希（正确拒绝）。

## 尚未验证的一项

F2 的大小写绕过在**函数层面**已证明（返回值从 `""` 变为命中），但"`Rules` 与 `rules` 是同一个目录"
这一步需要大小写不敏感的文件系统，审计环境（Linux）无法构造。修复对两种情况都成立，
但**在 macOS 上的端到端复现仍待补**。
