<!-- SPDX-License-Identifier: MIT -->
# 020 — `clean --ask` 审查后撤回并重做

- **类别**：安全缺陷 + 功能撤回
- **严重程度**：高（5 项能移动/隐藏操作者没要求的东西）
- **状态**：**已修复**（`--ask` 撤回后重做，`internal/clean/ask.go`）。11 项全部关闭 —— 4 项修在别处，
  7 项由重做的架构消除，见 §7。方向键是**下一轮**的纯输入层。

> 这条 issue 的用途是：**重做选择器之前先读完**。下面每一条都是真机复现过的，
> 不是纸面推演。

## 0. 背景

`clean --ask` 用方向键逐对回答重复 skill。第一版落地后经两轮审查撤回，
按 §7 重做。命令行那条路（`--resolve <id> --keep <name>` / `--keep-both`）**全程保留**
——两轮独立审查都单独确认它是干净的。

**§1–§6 记的是撤回时的状态，逐条标记原样保留**（"❌未修"读作"当时未修"），
§7 说明重做怎么把它们关掉的。保留原始记录是为了让重做的理由可被审计。

## 1. 能移动或隐藏操作者没要求的东西（5 项）

### 1.1 别名对会毁掉被点名保留的那一个 ✅已修

`skills/alias -> skills/real` 产生一个 `duplicate_fn` 对，**两个 locator 指向同一个目录**：

```
targets:  ['alias', 'real']
locators: ['skills/real', 'skills/real']
blockers: ['side-selection-required']
```

`--keep alias` 算出 `drop=real`，而 `real` 正是 `alias` 指向的东西。`quarantinable` 过
（在 `skills/` 下），**内容哈希复核也过——因为本来就是同一棵树**。工具报成功，被点名保留的
skill 消失。

**跟交互无关，命令行那条路一样中招**，所以先修了：`hygiene.samePath` 判定两侧解析后是否同一目录，
是则挂 `model.BlockerSameTarget`，`Executable()` 不再放行。解析失败也算"相同"——
"这两个是不是同一个东西"答不上来时，拒绝执行是安全侧。

钉住它的测试：`TestResolve_RefusesAPairThatIsOneDirectory`。

### 1.2 走查全程使用问之前的那一份扫描 ❌未修（随 `--ask` 一起撤回）

hygiene 是 `i<j` 全配对，三个相似 skill 产生 **三对**：`(a,b) (a,c) (b,c)`。
`ResolveInteractive` 把 `res` 抓一次就不再刷新，`pathByName` 也来自那份旧快照，
而且**没有任何地方复核"被保留的那一侧还在不在"**。

复现：三次回车之后 `skills/` 可以空掉，`Skipped=0`，退出码 0，
其中一屏还印着 `Resolving …: keeping b`——而 b 已经在 trash 里。

**没有任何一次按键要求过这个结果。** 重做时必须：每对提问前重新看磁盘，
或把已被移走的一侧涉及的后续对直接剔除。

### 1.3 方向键会误触发"两个都留" ❌未修

`decode` 对未知转义**固定吞 3 字节**。而 `Shift+Down` 是 `ESC [ 1 ; 2 B`（六字节，
`Ctrl+Down`/`Alt+Down` 同族）：吞掉前 3 个，`;` `2` 当未知丢掉，剩下的 **`B` 命中 keep-both**，
于是一次移动光标的按键往 `.aguardignore` 写了一条抑制。

实测：`pick(…, []byte{0x1b,'[','1',';','2','B'})` → `{both:true}`。

代码注释写的是"swallow it rather than acting on a guess"——**它就是在按猜测行动**。
重做时必须完整解析 CSI（`ESC [` 之后读到 `@`–`~` 的终结字节为止），而不是数字节。

### 1.4 raw 模式其实有默认边 ❌未修

`sel` 初值 0，任何一个游离的 `\r` 就是确认。粘贴一段多行文本、X10 鼠标报告里坐标恰好是 `0x0d`，
都能变成"确认第一项"。实测：`pick(…, []byte("hello world\n"))` → `{keep:"a"}`。

`pickLine` 是对的（必须敲 `1`/`2`），raw 那条路不是。**重做时初始必须无选中**，
第一次方向键才产生选中。

### 1.5 `pickLine` 把 EOF 前的半行当确认 ❌未修

`if err != nil && line == ""` 只在**空**读时中止。输入 `1` 然后 Ctrl-D：
`pickLine` 返回 `{keep:"a"}`。设计写的是"EOF 永远是中止"。

## 2. 对操作者撒谎（3 项）

### 2.1 部分完成的运行报成"什么都没做" ✅已修

移动成功之后 `appendRecord` 失败 → `return out, err` → `main` 丢掉 `Result` 直接返错 →
退出码 **2**，而帮助里 2 的定义是 "refused, nothing changed"。目录已经搬了，
`Batch … Undo with:` 那行在循环之后才印，所以**连 undo 的把手都没给**。

已修：`undoHandle` 在错误终止前，只要这次运行**已经移动过东西**，就先把 batch id 和
`--undo` 命令印出来。**错误可以结束一次运行，不能追认它已经做过的事没发生。**

### 2.2 同一秒内的多次 resolve 共用一个 batch ❌未修

`batchID` 只到秒，`n` 在 resolve 里恒为 1。`--ask` 一秒答完的几对全落进同一个 batch，
而每移动一次就印一遍 `--undo <同一个 id>`——照着印的命令做会把不该恢复的一起恢复。

重做时：一对一个 batch，或让 batch id 带上项 ID。

### 2.3 一对失败中断整个走查 ❌未修

`duplicatePairs` 只按 kind/ID/长度过滤，**不看 `Executable()`**。带其它 blocker 的对照样显示、
照样收按键，然后 `Resolve` 返错、`ResolveInteractive` 立即返回。前面几对已经落盘，
而 `main` 拿到 error → 退出码 2 → `partialGate` 不跑 → `Skipped>0` 永远变不成退出码 3。

## 3. 其它 ✅已修

### 3.1 `--apply` / `--undo` 静默吞掉 `--keep` / `--keep-both`

`MarkFlagsMutuallyExclusive` 漏了 keep↔apply、keep↔undo、keep-both↔apply、keep-both↔undo。
实测 `clean --apply --keep bogus` 跑完 apply、对 `--keep` 一个字不提、退出 0。

这正是 `main.go` 里那段注释说"已经为 apply/undo 修过"的同一类问题，
**漏在了两个承载操作者决定的 flag 上**。四组都补上了。

### 3.2 `--keep-both` 会毁掉末行无换行的基线

`.aguardignore` 末行没有 `\n` 时直接追加，两行融成
`EXEC-001D-1a2b3c4d  # keep both: a, b`：操作者原来的抑制规则被毁，
**新 ID 也没记上**（融合后的 token 过不了 `itemIDRE`），而 stdout 两件事都报成功。

已修：追加前检查末字节，必要时补一个换行。
钉住它的测试：`TestKeepBoth_DoesNotFuseWithAnUnterminatedLastLine`。

### 3.3 选择器不过 `report.sanitize` ❌随撤回一并消失

违反不变量 #7。skill 名是攻击者可选的目录名；`pairLines()` 假设一目标一行，
而带 `\r\n` 和 `❯` 的名字能伪造光标位置并打乱重绘偏移。
**重做时选择器渲染必须走 `report.sanitize`**。

## 4. 我自己的测试出了什么问题

值得单列，因为这套测试当时是绿的：

- **`TestDecode_KeyTable` 里 `"unknown escape is swallowed"` 钉的是 bug**——
  它断言的正是产生 §1.3 的固定 3 字节框架，表里没有任何超过 3 字节的用例，
  也没有 2 字节的 ESC 前缀用例。**读起来像转义处理的覆盖，实际是给缺陷发了合格证。**
- **没有任何测试走过一对以上。** `dupSetup` 结构上造不出多对 fixture
  （传三个名字会变成一个三目标项，被 `duplicatePairs` 过滤掉）。
  于是 §1.2、§2.2 完全没被钉，而"没有一个按键能一次回答多对"这条**头号设计承诺**也没被钉。
- **`pickLine` 一条测试都没有**——整个 fail-closed 第 (b) 层未被执行过，§1.5 就住在那儿。
- **没有断言 `restore()` 会被执行**（`enterRaw` 在每个测试里都被替换成 `func(){}, true`）。
- **没有断言"没有默认边"**——改掉 `sel` 初值不会红。

教训写进 CLAUDE.md：**终端交互的测试必须把"没人回答"和"回答了别的"当成两种不同的输入来测**，
而不是只测"回答了正确的"。

## 5. 依赖

`golang.org/x/term` 是为 `--ask` 加的，随它一起移除，直接依赖回到 **两个**。
重做选择器时要重新论证一次（见 CLAUDE.md 的依赖条款，包括**必须看它的 `go` 指令**）。

## 6. 关联

- 撤回的实现：`internal/clean/select.go`、`internal/clean/termraw.go`（已删除）
- 重做的实现：[`internal/clean/ask.go`](../internal/clean/ask.go)
- 保留部分：[`internal/clean/resolve.go`](../internal/clean/resolve.go)、
  [`internal/clean/keepboth.go`](../internal/clean/keepboth.go)
- [`019`](019-symlink-installed-skills-report-only.md)（同一轮里另一个"计划与执行不同尺"的例子）


## 7. 重做（`internal/clean/ask.go`）

撤回时记的 7 条不是 7 个 bug，是 **3 个根因**。重做按根因来，不按症状来。

### 7.1 根因一：选择器自己在执行 → §1.2 §2.2 §2.3，以及退出码契约

旧版每收到一个答案就立刻 `Resolve`，全程用问之前那份扫描。现在是**收集 → 对账 → 一次执行**：

```
按键流 ──► askAll（纯函数，不碰文件系统）──► []answer
                                              │
                                     reconcile（纯函数）
                                              │
                                    一次 quarantine()  ← 和 --apply 同一条流水线
```

一次执行带来：**一个 batch id**、**一个 `Result`**（`partialGate` 又能算退出码 3 了）、
**逐项拒绝照常上报而不中断整轮**。

`reconcile` 处理走查真正的语义危险——**对是重叠的**（a,b,c 产生 `(a,b) (a,c) (b,c)`），
所以同一个名字可能在一个答案里被保留、在另一个答案里被丢弃。规则：**保留胜过丢弃**，
并且**把矛盾说出来**而不是悄悄择一。实测（答 `1 / 2 / 1`，三个答案互相矛盾）：

```
keeping b: you kept it in one pair and dropped it in another, and a keep wins.
keeping c: you kept it in one pair and dropped it in another, and a keep wins.
No side was chosen for quarantine.
幸存：a b c
```

旧版在同样输入下能把 `skills/` 清空并报成功。

重复丢弃（两对都丢 c）合并成一次移动。

### 7.2 根因二：手写终端协议 → §1.3 §1.4 §1.5

**整个消失**：这一版读**整行**。没有转义序列可以拆错，没有 raw mode，
**天然没有默认选中**——不敲数字就什么都没选中。EOF 在**任何位置**都是中止，
包括半行（`1` 然后 Ctrl-D 是"没有回答"，不是"回答了 1"）。

### 7.3 根因三：渲染不过 sanitize → §3.3

一切输出走 `report.Sanitize`（**导出了那一份实现**，不是抄一份——抄一份就是让两边漂移）。

### 7.4 终端检测降级为"礼貌提示"

旧版把 tty 检测做成了安全边界。现在**正确性不依赖它**：管道会读到 EOF，EOF 是中止，
无法识别的行是跳过，所以非交互运行本来就动不了任何东西。
**需要"必须判对"的检查，比不需要它的设计更差。**

### 7.5 测试：这次逐条变异验证过

| 变异 | 被谁抓住 |
|---|---|
| 丢弃可以带走被保留的名字 | `TestReconcile_AKeepBeatsADropOfTheSameName` + 端到端那条 |
| 半行 EOF 算数 | `TestAsk_PartialLineAtEOFIsAbort` |
| 空行确认默认边 | `TestAsk_NoDefaultSide` |
| 名字不过 sanitize | `TestAsk_NamesAreSanitisedBeforePrinting` |
| 无法回答的对照样提问 | `TestAskablePairs_SkipsWhatCannotBeSettled` |

**并且 fixture 修好了**：`multiPair` 能造出 N 个 skill × 每对一项，
所以 §4 里"结构上造不出多对场景"那条不再成立——头号承诺
（`TestAsk_OneLineAnswersExactlyOnePair`）现在有断言。

### 7.6 方向键（第二步，已做）

按承诺加成**纯 `按键→行` 的输入层**（[`rawline.go`](../internal/clean/rawline.go)）：
它唯一的产物是"人本来也能敲出来的那个字符串"（`1` `2` `b` `s` `q` 或空），交给**同一个 `askOne`**。
下游分不出答案是敲出来的还是按方向键选出来的，所以**它改不了答案导致什么**。

三条性质是结构性的，不靠小心：

| 性质 | 为什么是结构性的 |
|---|---|
| **没有默认边** | 光标初值 `-1`。没按过方向键就回车 → 返回空串 → `askOne` 当跳过，和空行完全一样 |
| **状态行不含任何攻击者可控文本** | 菜单由 `askOne` 印过（走 `Sanitize`），这一层只重写**一行**、**只显示数字**。没有名字可以伪造光标，也没有多行回绕可以错位 |
| **转义序列整条消费** | CSI 读到 `0x40`–`0x7E` 的终结字节为止；SS3（`ESC O A/B`，终端进 application keypad 后就是它）也认；认不出的整条丢弃，**绝不留尾巴** |

§1.3 那条缺陷的变异体验证过：把解析换回"固定吞 3 字节"，`ESC [ 1 ; 2 B` 的尾巴立刻漏成字面量
（`;`），测试当场红。另外四个变异（默认选中、不认 SS3、零长读空转、状态行印名字）也各自被抓。

**修饰过的方向键（Shift/Ctrl/Alt+方向）当成普通方向键**——这是终端用户的预期，而且它只能移动，
**只有回车才提交**。有专门一条断言："同样的序列后面不跟回车，必须什么都答不出来"，
证明尾字节没有变成独立按键。

依赖因此回到三个，`x/term` **钉在 v0.28.0**（配 `x/sys v0.29.0`），理由和踩过的坑见 CLAUDE.md。
