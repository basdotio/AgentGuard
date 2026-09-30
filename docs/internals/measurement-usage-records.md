<!-- SPDX-License-Identifier: MIT -->
# 实测：使用记录（`history.jsonl` / transcript）里到底有什么

> 日期 2026-08-16 · 在一个真实的 Claude Code 环境上测量（13.4 MB 主 transcript + 1.6 MB 子代理
> transcript，3315 行，31 个已安装 skill）。
>
> 起因是 `prd-selective-clean.md`(文件未入库) 的 **R28**：zombie 判据用的是目录名，
> 而 transcript 记的可能是 frontmatter 名。**测出来的问题比 R28 问的严重得多**，所以这份文档记的是
> 实际结论，不是原来那个问题的答案。
>
> **隐私。** 全程只提取**结构**——JSON 键名、`type` 值、工具名、以及某个名字**是否**出现。
> 没有任何消息内容被打印或留存。这与 `internal/hygiene/usage.go` 自己的约束一致：只做成员测试。

---

## 1. 结论先行

**在任何保留 transcript 的机器上，zombie 检查在修复前无法报出任何东西。**

不是"误报率高"或"信号弱"——是**结构上不可能命中**：Claude Code 会把**全部可用 skill 的名单**写进
transcript，而检查问的是"这个名字在 transcript 里出现过吗"，于是**"已安装"本身就足以看起来"已使用"**。

而且置信度阶梯是**反的**：

| 档位 | 证据 | 4 个 skill 的 fixture 里报出几个 | 实际情况 |
|---|---|:--:|---|
| `low` | 只有 `history.jsonl` | **4** | 其中 3 个确实从未被调用 |
| `medium` | 有 transcript | **1** | 那 3 个被判成"用过" |

**代码里更被信任的那一档，是失效的那一档。**

---

## 2. 测量方法

```
projects/<project>/<session>.jsonl                    ← 主 transcript
projects/<project>/<session>/subagents/<agent>.jsonl  ← 子代理 transcript
```

对每一行 `json.loads`，只统计 `type`、`attachment.type`、`tool_use` 的 `name`，
以及"某个 skill 目录名是否出现在这一行"。

## 3. 记录形态

主 transcript 的 `type` 分布：

| type | 条数 |
|---|--:|
| `assistant` | 1790 |
| `user` | 1226 |
| `attachment` | **117** |
| `system` | 56 |
| 其它（`last-prompt` / `queue-operation` / `mode`） | 126 |

`attachment` 的子类型：

| `attachment.type` | 条数 |
|---|--:|
| `edited_text_file` | 74 |
| **`skill_listing`** | **21** |
| `deferred_tools_delta` | 6 |
| `file` | 6 |
| `agent_listing_delta` / `mcp_instructions_delta` | 各 3 |
| `queued_command` / `compact_file_reference` | 各 2 |

## 4. 决定性的那条记录

```json
{"type":"attachment",
 "attachment":{"type":"skill_listing","skillCount":31,"names":[…31 个…]},
 "sessionId":"…","timestamp":"…"}
```

`names` 里是**每一个可用 skill**，形态是 **`<plugin>:<skill>`** 或裸的目录名：

```
product-management:brainstorm
productivity:memory-management
dataviz
docx
…
```

**这一条同时回答了 R28 原本的问题**：名单里用的是**目录名**（带 plugin 前缀），不是 frontmatter 的
`name`。所以 `hygiene.Analyze` 拿 `a.Name`（目录名）去匹配，在**名字这一维上是对的**——
问题从来不在名字，在这条记录本身就不该被当作证据。

**一次会话写了 21 条。**

> **补测更正（2026-08-16）："roster 每次变化写一条"这句是错的。** 在另一台机器上量了一次完整会话：
> **23 条，31 个名字的集合 23 次完全相同**（哈希一致）——名单一次都没变，照样写了 23 遍。
> 触发的不是变化，更像是**上下文重建时重发**：第一条在**第 10 行**（会话一开始），
> 每个子代理 transcript 在**第 3 行**各有一条，主 transcript 里约每 69 个 user turn 一条，
> 相邻间隔中位数 95 分钟。
>
> 对 `clean` 的意义是好消息：**校准的 roster 那条腿基本上稳拿**，任何留着 transcript 的机器都有。

## 5. 端到端验证

fixture：4 个 skill，其中 `dataviz` / `docx` / `pptx` 的名字出现在真实 transcript 的
`skill_listing` 里但**本次会话一次都没被调用**，外加一个哪都不出现的对照组
`zzz-never-mentioned-anywhere`。使用记录直接用那份真实的 13 MB transcript。

```
修复前： 只报出 zzz-never-mentioned-anywhere              （1/4）
修复后： dataviz · docx · pptx · zzz-never-mentioned-anywhere （4/4）
```

修复后的结果与"只有 `history.jsonl`"那一档一致，这是它正确的旁证。

## 6. 顺带测出的第二个缺口：子代理 transcript 从未被读

`usedNames` 的目录遍历里有 `if s.IsDir() { continue }`，而 `<session>/subagents/` 恰好是一个目录。
真实环境里：**5 个子代理 transcript、1.6 MB，就在那个 13 MB 主 transcript 旁边。**

后果不只是漏掉证据。在一个**只有**子代理 transcript 的环境里（fixture 验证过），旧代码认为
**根本没有使用记录**，于是输出的是：

```
· — [zombie/info]
    No usage record (history.jsonl or projects/*/*.jsonl); the zombie check was skipped.
```

——一条"检查没能跑"的通知，而证据其实就在旁边的目录里。

## 7. 已做的修复

| 修复 | 位置 | 钉住它的测试 |
|---|---|---|
| 跳过 `skill_listing` 记录 | `hygiene.isSkillListing` | `TestUsage_SkillListingIsNotEvidenceOfUse` |
| 只跳过那一种记录，含标记的普通行照常匹配 | 同上 | `TestUsage_OnlyTheListingRecordIsSkipped` |
| 读 `<session>/subagents/*.jsonl` | `usedNames` 的遍历 | `TestUsage_SubagentTranscriptsAreRead` |

三个断言都在旧代码上验证过会失败。

**`isSkillListing` 的实现方式**：先用廉价的子串预筛（几千行里只有 21 行含 `skill_listing`），
命中才对那一行 `json.Unmarshal`。**解析失败时选择"继续匹配"而不是"跳过"**——把一条无法辨认的行
丢掉会静默缩小证据面，而那正是这次要修的错误方向。

## 8. 补测：一次真实的 skill 调用长什么样

第一轮测量时这个会话从没调用过 skill（20 种 `tool_use` 里没有 `Skill`），所以形态是未知的。
**补做了一次受控调用**：在同一个容器里调用 `dataviz`（纯指导型 skill，无副作用），前后对比 transcript。

**结果：**

```json
{"type":"assistant","message":{"content":[
   {"type":"tool_use","name":"Skill","input":{"skill":"dataviz"}} ]}}
```

调用**确实被记录**，形态是 `tool_use`，`name` 为 `Skill`，`input.skill` 是**调用时用的名字**。
调用前该形态出现 0 次，调用后 1 次。

**这意味着存在一个远比"名字出现在某处"精确的判据**：找 `name == "Skill"` 且
`input.skill == <skill 名>` 的 `tool_use`。它不会被散文里的顺口一提污染，也不会被名单污染。

### 8.1 不能直接切换过去：错误方向不对称

| 判据 | 错的时候往哪边错 | 后果 |
|---|---|---|
| 名字出现在任意位置（现行） | 把没用过的算成用过 | **少报 zombie**——安全方向 |
| `input.skill` 精确匹配 | 把用过的算成没用过 | **多报 zombie**——**让工具建议你删掉正在用的东西** |

而我只有**一个观测点**，而且是一个**内置 skill**（`dataviz` 在
`/tmp/claude-0/bundled-skills/…`，不在 `~/.claude/skills/` 下）。

**没观测到的**：一个装在 `<root>/skills/<目录名>/` 下的用户 skill，被调用时 `input.skill`
到底等不等于那个目录名。极可能相等，但"极可能"不足以支撑一个会往危险方向错的改动。
本会话也测不了——可用 skill 清单在会话启动时就定了，现建的 skill 这一轮调不到。

### 8.2 解法：这个前提不该由我证明，该由工具在每台机器上自证

关键观察：**让精确判据出错的那个前提，本身是可观测的**，而且用的就是已经在读的字节。
桥梁正是第 4 节那条"不算证据"的 roster 记录——它用 Claude Code 自己的命名空间列出了**每一个**
可用 skill：

| 步骤 | 判定 | 结论 |
|---|---|---|
| **1（逐 skill）** | 该 skill 的目录名出现在 roster 里 | roster **对它**说的是目录名 |
| **2（全局）** | 每个观测到的调用名都出现在 roster 里 | 调用记录说的是 roster 的命名空间 |

两条都成立 ⇒ 调用名就是目录名，**在这台机器上被证明**，精确判据启用。
任一条不成立 ⇒ **该 skill 退回宽松判据**，并出一条通知。退回是**逐 skill** 的，
一个刚装上、还没进过任何 roster 的 skill 不会把整轮拽回去。

危险形状落在哪、谁抓住：skill X 被用过却记成别名 ⇒ **那个别名就躺在 transcript 里**，
步骤 2 测的正是它。**残留的洞**是撞名（别名恰好是另一个已安装 skill 的名字），需要刻意重名，没有防。

**宽松判据只在 transcript 这条腿上被替换**，`history.jsonl` 永远照旧算数——否则每一次手打的
`/skill` 都会重新掉进危险方向。

实现：`internal/hygiene/usage.go` 的 `calibration`；`TestUsage_Calibration*` 四条测试
（含两条反向断言：校准该拒绝时不拒绝、该启用时不启用，各自失败）。

### 8.2.1 名字要归一到叶子，否则整套校准在真机上静默失效

第一次拿真环境跑就撞上了：真实布局是 `skills/synced/docx`，artifact 名是 `synced/docx`，
而 roster 里叫 `docx`。roster 本身也有两种形态（`product-management:brainstorm` 和裸的 `dataviz`）。
`baseName` 把 `:` `/` `\` 之后的叶子取出来做比较。不剥前缀，步骤 1 对每个嵌套 skill 都失败，
**整个特性在这类机器上一声不吭地什么都不做**——比报错更糟的失败方式。

代价：同叶名的两个 skill（内置 `docx` 与安装的 `synced/docx`）在这里不可分，歧义解析成"用过"，安全侧。

### 8.2.2 一个自污染回路

在一次 Claude Code 会话里跑 `aguard clean --zombie`，它会把每个 zombie 的名字**印到终端**，
而终端输出**进 transcript**（§8.4：持续刷盘）。下一次跑同一条命令，这些名字已经"出现在 transcript 里"，
宽松判据把它们统统读作"用过"。

**实测：第一次报 7 个 zombie，第二次报 0 个，代码一个字没改。**

这是宽松判据最干净的一个反例：工具自己的输出成了自己的证据。

**没有在代码里修，只在 [`clean-guide.zh-CN.md`](../clean-guide.zh-CN.md) 里给了规避（在普通终端跑）。**
想到的几条修法方向都是错的：

| 想法 | 为什么不行 |
|---|---|
| 扫描时跳过 `tool_result` 类记录 | 去掉的是证据 ⇒ "用过"变少 ⇒ **zombie 变多**。为了修一个少报引入一个多报 |
| 认出 aguard 自己的输出格式并跳过 | 拿自己输出的文本形状当判据，改一句提示语就静默失效 |
| 不印名字 | 那这条命令就没用了 |

**方向正确的修法只有一个：让精确判据在更多机器上真正启用**，它对这个回路免疫。
前置的未知见第 9 节第 4 项。

### 8.3 置信度天花板仍然不动

`usageSource.confidence()` 最高仍到 `medium`，`model.UnattendedSafe()` 仍不可达。
校准除掉的是**假阴性**来源；封顶这个检查的是另外两件事——这是**一台机器**的历史，
而且最多 **64 个会话**。"在这台机器最近 64 次会话里没用过"不等于"没用过"。

### 8.4 顺带测到：transcript 是持续刷盘的

同一次实验：调用前 3392 行，调用后 3401 行，文件在**几秒内**增长了 16 KB，`mtime` 就是当下。

**Claude Code 持续写 transcript，不是会话结束才刷。**

这一条本身跟 zombie 无关，但它**解开了另一个被搁置的问题**。
[`clean-internals.md`](clean-internals.md) §8.3 里写着：不做"会话是否在运行"的检测，因为
"transcript 的 mtime 回答的是'最近有会话写过东西'，而 Claude Code 到底持续刷盘还是会话结束才刷，
**没有实测过**"。

**现在测过了：是持续刷盘。** 所以"最近 N 秒内有 transcript 被写"**确实是**"有会话正在跑"的证据——
不是判决性的（会话可能刚结束），但它从"不知道有没有信号"变成了"有信号，需要定一个阈值和一个
误报策略"。这让那条待办从"需要先测量"升级为"可以设计了"。

### 8.5 真正卡住校准的不是记录缺失，是我们停在了它前面

roster 有 23 条、调用记录也有——可校准在真机上**就是不启用**。原因量出来了：

```
文件总长        16.5 MB / 4410 行
maxUsageBytes   8 MiB  →  切在第 1871 行（51% 处）

roster 23 条，8 MiB 之内有 10 条          ← 够用
Skill 调用 @ 第 3363 行，偏移 13.60 MB    ← ★ 被截断，读不到 ★
```

**证据就在文件里，我们停在了它前面。** 三件事叠加：

1. **截断是静默的。** `maxUsageFiles` 撞上限会出通知，`maxUsageBytes` 一个字都不说——
   同一个函数里两个上限，一个说一个不说。
2. **它从文件头读。** 和 `maxUsageFiles` 犯过的是同一个错（那个已改成 mtime 倒序），
   字节这个还在读"最旧的一半"。
3. **错误方向是危险的那边。** 少读 ⇒ 少匹配到"用过" ⇒ **多报 zombie**。

而这个上限当初的理由是"别把几十 MB 文件读进内存"——可它是 `bufio` **流式**扫描，
内存本来就由行缓冲兜着，字节上限限的是**时间**不是内存。**为一个不存在的问题付了一个往危险方向错的代价。**

**已修**：改成读**最近的** `maxUsageBytes`（与 `transcriptFiles` 的 mtime 倒序同一条理由——
只装得下一部分历史时，有意义的是离现在最近的那部分），seek 落点的半行丢弃，撞上限**出通知**
并说明它往哪边错。

**修完当场生效**：同一台机器上校准立刻启用，zombie 从 7 个降到 6 个——
有一个 skill 的使用证据就躺在被截掉的那一半里。
钉住它的测试：`TestUsage_OversizedTranscriptIsReadFromTheEnd`（变异回"读文件头"会红）。

## 9. 遗留问题

1. **`maxUsageFiles = 64` 在真机上根本不是约束——约束是历史本身有多短。**
   实测那台机器：`ls ~/.claude/projects/*/*.jsonl | wc -l` = **15**。
   64 这个上限一次都没碰到（截断通知没出现），**调大它一点用都没有**。
   真正封住这个检查的是"这台机器上总共只有 15 次会话"。所以：
   - 报告现在**把会话数印进每一条 zombie 的说明里**（`TestZombie_DetailStatesHowMuchHistoryItSaw`）。
     "Never invoked in any session transcript" 读起来像翻遍了所有历史，而它其实是关于 15 次会话的
     一个断言——操作者知道一件工具不知道的事（上次是什么时候伸手去用的），得先看见这个数才用得上。
   - 要不要调 64，得先遇到一台**真的撞上它**的机器；在那之前这是个假问题。
2. **两条记录格式都没有文档**，`skill_listing` 和 `tool_use{name:"Skill"}` 都是观测出来的。
   - `skill_listing` 变形 ⇒ roster 为空 ⇒ 校准不激活 ⇒ **退回宽松判据**（安全侧，但检查变钝）。
   - 调用记录变形 ⇒ 观测集为空 ⇒ 同样退回宽松判据（安全侧）。
   两个方向都往安全侧退，这是设计的一部分；但**退化是静默的**——今天只有"校准被拒绝"会出通知，
   "两种记录一条都没看见"不会。值得加一条自检：看见了 N 条 roster、M 条调用记录。
3. **校准的撞名洞没有防**（§8.2）：X 记成的名字恰好是另一个已安装 skill 的名字。
   需要刻意重名，代价是漏报一个真实使用。
4. ~~**roster 记录在什么条件下才写，没测过。**~~ **这一项作废，因为它建立在我自己的一个测量错误上。**

   原文说"一台真机 21 条，这个容器 0 条，原因不清楚"。**0 条是假的**：那个统计脚本前面跟了一次
   `cd`，shell 把工作目录重置了，相对 glob 匹配到**零个文件**，脚本于是老老实实报了 0。
   重测的真实数字是 **23 条**（见 §4 的更正框）。

   **留着这段是因为教训比结论值钱**：一个"什么都没找到"的测量结果，和一个"没去找"的 bug，
   在输出上长得一模一样——这正是这个工具自己的不变量 #5 要防的东西，而我在自己的测量脚本里
   犯了同一个错。**统计脚本必须先报"我看了几个文件"，再报"找到几条"。**

5. **`maxUsageBytes = 8 MiB` 曾经从文件头读，而且截断是静默的。**（已修，见 §8.5）

## 10. 关联

- `prd-selective-clean.md`(文件未入库) R28 / R29（原始问题）
- [`clean-internals.md`](clean-internals.md) §4.1（zombie 判据）· §4.1.1（校准）
- [`measurement-startup-loading.md`](measurement-startup-loading.md)（同一套"先量再改"的先例）
- 实现：[`internal/hygiene/usage.go`](../../internal/hygiene/usage.go)
