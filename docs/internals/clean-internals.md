<!-- SPDX-License-Identifier: MIT -->
# `clean` 技术手册（实现与审计）

> 这份文档的读者是**要审计这段代码或要改它的人**。
>
> - 想知道怎么用 → [`clean-guide.zh-CN.md`](../clean-guide.zh-CN.md)
> - 想知道当初为什么这么设计 → `../prd-selective-clean.md`(文件未入库)
> - 想知道哪里还不行 → [`issues/012`](../../issues/012-clean-scope-and-remaining-gaps.md)
>   [`013`](../../issues/013-clean-write-path-audit.md)
>   [`015`](../../issues/015-manifest-integrity-not-tamper-proof.md)
>   [`018`](../../issues/018-clean-autoapply-rejected.md)
>
> 引用代码时用**函数名**而非行号（行号会漂）。每条不变量都在 §11 的表里给出"在哪强制 + 哪个测试钉住"。

---

## 1. 一句话概括

`clean` 是 AgentGuard 唯一会写盘的命令。它做两件事：**检测**（`internal/hygiene`，纯读、无 LLM）
和**执行**（`internal/clean`，只有一种动作——把 `skills/` 下的目录移进 `<root>/.aguard-trash/`）。
**它永不删除。** 删除是用户自己的动作。

三条贯穿全局的设计断言：

1. **可逆性决定"能不能写"，"是否需要人做选择"决定"能不能批量"** —— 这是两个独立的轴，
   `Tier` 编码前者，`Confidence` 参与后者的门槛。
2. **manifest 是数据，不是权威。** 它是用户配置目录里的普通文件，任何能写那里的东西都能追加行。
   所以 `Undo` 重新推导每一个安全判断，而不是相信读到的内容。
3. **路径判断必须在解析后做。** `internal/clean/paths.go` 里每一个函数都对应一个被实测验证过的攻击，
   共同根因都是"对写下来的路径而不是解析后的路径做判断"。

---

## 2. 数据流

```
collect.CollectAll(root)                     纯读，产出 []ArtifactReport
        │
        ├──► detect.Run                      风险发现（与 clean 无关，但共享 artifact）
        │
        └──► hygiene.Analyze(root, arts, Options{Zombie})
                    │                        产出 []model.CleanItem（每项一个决策）
                    │                        assignIDs 赋稳定 ID
                    ▼
             ScanResult.Hygiene
                    │
        ┌───────────┴────────────┐
        ▼                        ▼
  report.Hygiene            clean.Apply(w, root, res, dryRun)
  （只显示）                       │ 门：CleanItem.Executable()
                                  │ 只处理 Kind=="zombie"
                                  ▼
                          manifest.jsonl（写前记录 → rename → 确认）
                                  │
                                  ▼
                          clean.Undo(w, root, batch, dryRun)
                                  五重再推导 + 恢复前预览
```

**关键耦合点：** `Apply` 的输入是一份完整的 `ScanResult`，不是独立重扫。CLI 里
`cmd/aguard/main.go` 的 clean 分支先跑 `scanEnv`，再把结果交给 `Apply`。这意味着
**扫描与移动之间存在时间窗口**，`resolveTarget` 里的 content-hash 复核就是为这个窗口存在的。

---

## 3. 数据结构契约

### 3.1 `model.CleanItem`

| 字段 | 含义与约束 |
|---|---|
| `ID` | 对同一目标集合跨运行稳定，单次清单内唯一。**寻址某个决策的唯一句柄。** 无目标的通知项为空 |
| `Kind` | `zombie` / `context_bloat` / `duplicate_fn` / `stale_ref` |
| `Tier` | `A1` `A2` `B` `C` —— 见 §3.2 |
| `Actionable` | 这一项**指向一个具体动作**。**不代表现在能跑**（见 `Blockers`） |
| `Confidence` | `low` / `medium` / `high` |
| `Action` | `move` / `config-remove` / `line-delete` / 空 |
| `Targets` | 展示名。报告可以只认这个字段 |
| `Locators` | 结构化定位：root 相对路径 + 名字 + 规范哈希（+ `Entry` 用于文件内定位） |
| `ReclaimTokens` | **单项**估算，不是总和。求和由报告层做 |
| `Blockers` | 现在动不了的每一个原因。**被拦下的项仍然要报告**，只是不提供 |

### 3.2 两个谓词，管的不是同一件事

`internal/model/model.go`：

```go
func (c CleanItem) Executable() bool {
    return c.Actionable && c.Action != ActionNone && len(c.Blockers) == 0
}

func (c CleanItem) UnattendedSafe() bool {
    return c.Executable() && c.Tier == TierAuto && c.Confidence == ConfHigh
}
```

**⚠️ 审计要点：这两个谓词管的不是同一件事。**

| | 谁在用 | 管什么 |
|---|---|---|
| `Executable()` | **`clean.Apply` 的门**（`clean.go` 注释：the single gate） | 这一项**现在能不能动** |
| `UnattendedSafe()` | 只有报告层（`report.Hygiene`） | 一个**没有点名这一项**的运行能不能拿它 |

**所以 `--apply` 会移动低置信度的 zombie，而且这是对的**：`--apply` 要求操作者点名类别
（`--zombie`）又点名动作（`--apply`），按定义就不是无人值守。拿一个回答"无人值守"的谓词去守
`--apply` 的门，是在用 A 问题的答案回答 B 问题。

### 3.3 `UnattendedSafe()` 今天不可达 —— 是"不可达"，不是"暂时为空"

两个条件被**两个不相交的集合**满足：

| 条件 | 谁产出 | 另一个条件成立吗 |
|---|---|---|
| `Tier == TierAuto` | 只有 zombie | 置信度来自 `usageSource.confidence()`，**没有返回 high 的分支**（一台机器的历史证明不了"没被用过"） |
| `Confidence == ConfHigh` | 只有 `context_bloat` | 它是 `TierContent`，而且挂着 `content-edit-unimplemented`，连 `Executable()` 都过不了 |

所以任何 fixture 都造不出满足它的项。这一点由 **`hygiene.TestConfidenceCeiling_KeepsUnattendedUnreachable`**
证明：它穷举 `usageSource` 的**全部四个输入状态**（两个 bool），因此是证明而不是采样。
另有 `TestNoDetectionIsUnattendedSafeYet` 从产出侧采样，两个都留着——**它们因不同原因失败**：
前者抓"天花板被抬高"，后者抓"新检测被接成 TierAuto+ConfHigh"。

**它为什么会长成这样**：`UnattendedSafe` 原名 `Batchable`，是给
`prd-selective-clean.md`(文件未入库) §3.2 设计的 `--apply-all` 写的门。
那条命令**从未被实现**（今天只有 bool 形状的 `--apply`）。**谓词比它的消费者先出生，然后消费者没来。**
改名是为了让它说出自己管的是哪个场景，而不是听起来像在管现有的 `--apply`。

**报告的那半行已经删掉。** 它过去恒定渲染成 `0 item(s) qualify for an unattended batch`——
那个零看起来像一个可能变化的测量值，还暗示存在一条你可以去跑的无人批量命令。§12"零必须可见"
在这里不适用：它防的歧义是关于**环境**的，而这个零是关于**工具自身**的。
**常量属于文档和测试，不属于每次运行的输出。**

### 3.4 `clean.Record`（manifest 的一行）

```
batch · item · kind · state · name · from · to · hash · unix · tool_version · prev
```

- `state`：`intended` → `done` → （可选）`undone`；`failed` 用于关闭一个**没有发生**的移动
- `from` / `to`：**绝对路径，记录下来而不是重建**。旧版按命名约定拼路径，对软链安装的 skill
  和第二次同名隔离（落在 `<name>.1`）都是错的
- `hash`：移动那一刻的内容哈希。**空哈希会被拒绝写入**，因为空值会静默关掉 undo 的身份校验
- `prev`：前一行**原始字节**的 SHA-256，构成哈希链

---

## 4. 检测侧：`internal/hygiene`

四类检测，全部确定性、无 LLM、纯读。每类**一项一个决策**（不是一类一项）——聚合形状读得懂但没法
逐项执行，而且环境里多装一个无关 skill 就会让项的身份漂移。

### 4.1 `zombie`（装了但没用过）

**默认关闭**，需要 `--zombie`。

判据来自两个磁盘记录，它们**不等价**（`usage.go` 的文件头注释是这段的权威说明）：

| 记录 | 是什么 | 能看见 | 看不见 |
|---|---|---|---|
| `history.jsonl` | 你敲过的每一条提示词 | 手打的 `/skill` 调用 | **任何工具/skill 调用**。Claude 自己路由的调用完全无痕 |
| `projects/<p>/<s>.jsonl` | 完整会话 transcript | skill 调用（形态已实测，见下） | 别的机器上的使用 |
| `projects/<p>/<s>/subagents/*.jsonl` | 子代理 transcript | 同上 | 同上 |

**⚠️ 一个实测出来的、曾经让整个检查失效的坑**（[`measurement-usage-records.md`](measurement-usage-records.md)）：

Claude Code 会把**全部可用 skill 的名单**写进 transcript（`attachment.type == "skill_listing"`，
一次真实会话 21 条，`names` 里是每一个已安装 skill）。而检查问的是"名字在 transcript 里出现过吗"
——于是**"已安装"本身就足以看起来"已使用"**，在任何保留 transcript 的机器上**报不出任何 zombie**。

置信度阶梯因此是**反的**：`low`（只有 `history.jsonl`）能报对，`medium`（有 transcript）结构上失效。
现在 `skillListing` 跳过那种记录。

**修复的性质要说准**：它除掉了一个**已确认的假阴性来源**，**没有证明**判据现在正确。

#### 4.1.1 精确判据，以及它为什么要被"校准"关起来

后来实测到了一次真实调用的形态：

```json
{"type":"assistant","message":{"content":[
   {"type":"tool_use","name":"Skill","input":{"skill":"dataviz"}} ]}}
```

这给出一个**远比"名字出现在某处"精确**的判据。但它**不能直接换上去**，因为两个判据的**错误方向相反**：

| 判据 | 错的时候往哪边错 | 后果 |
|---|---|---|
| 名字出现在任意位置（宽松） | 把没用过的算成用过 | 少报 zombie——**无害** |
| `input.skill` 精确匹配 | 把用过的算成没用过 | **建议你删掉正在用的东西**——有害 |

往有害方向错的改动，前提必须**被证明**，不能被假设。前提是"调用时记录的名字就是**目录名**"，
而它只在**一个内置 skill** 上被观测过一次。

**所以前提不由我证明，由工具在操作者自己的机器上、用它本来就在读的证据证明**。桥梁正是上面那条
被当作"不算证据"丢掉的 roster 记录——因为它用 Claude Code 自己的命名空间列出了**每一个**可用 skill：

| 步骤 | 判定 | 得到的结论 |
|---|---|---|
| **1（逐 skill）** | 这个 skill 的目录名出现在 roster 里 | roster **对这个 skill** 说的是目录名 |
| **2（全局）** | 每一个观测到的调用名都出现在 roster 里 | 调用记录说的是 roster 的命名空间 |

两条都成立 ⇒ 调用名就是目录名，**在这台机器上、对这些 skill、被证明了**。
任一条不成立 ⇒ **那个 skill 退回宽松判据**。退回是**逐 skill** 的，正是为了让一个刚装上、还没进过
任何 roster 的 skill 不至于把整轮都拽回去。

**危险的形状落在哪里、被谁抓住**：如果 skill X 被用过却记成了别的名字，那么 X 的调用**就在
transcript 里以那个别名躺着**——不匹配是可观测的，而步骤 2 测的正是它。
**残留的洞**是撞名：X 记成的名字恰好是另一个**已安装** skill 的名字，于是 Y 读作用过（安全侧）、
X 读作没用过（有害侧）。这需要一次刻意的重名，没有防。

**比较时名字要归一到叶子**（`baseName`）：roster 里既有 `product-management:brainstorm` 也有裸的
`dataviz`，而真实环境里 skill 会嵌套——`skills/synced/docx` 的 artifact 名是 `synced/docx`，
roster 里叫 `docx`。不剥前缀，整套校准在任何嵌套布局上**静默失效**，而这正是它在第一台真机上的表现。
剥了以后同叶名的两个 skill 不可分，歧义解析成"用过"，是安全侧。

**宽松判据只在 transcript 这条腿上被替换**。`history.jsonl`（你手打过的东西）永远照旧算数——
否则每一次手打的 `/skill` 调用都会重新掉进有害方向。

**一个真机上撞见的自污染回路，值得单独记一笔。** 在一次 Claude Code 会话里跑
`aguard clean --zombie`，输出会把每个 zombie 的名字印到终端，而**终端输出会进 transcript**
（transcript 是持续刷盘的，见 [`measurement-usage-records.md`](measurement-usage-records.md) §8.4）。于是**下一次跑同一条命令，这些名字已经"出现在 transcript 里"了，
宽松判据把它们全部读作"用过"**——实测：第一次报 7 个 zombie，第二次报 0 个，代码一个字没改。
精确判据不受这条回路影响，这是它存在的最好理由之一。

#### 4.1.2 `maxUsageFiles` 的含义变了

以前"读哪 64 个文件"无所谓——反正每个 skill 都匹配每个 transcript。现在**每多读一个文件都可能翻转
一个判断**，于是两件以前无关紧要的事变成必须：

- **按 mtime 倒序读**。`ReadDir` 给的是会话 UUID 的字典序，跟"最近 64 个会话"毫无关系。
- **撞上上限要说出来**。少读文件会缩窄"从没用过"的含义，不变量 #5 要求它出声。

同理，**校准被拒绝**（步骤 2 失败）或**某些 skill 退回宽松**（步骤 1 失败）也各出一条
**无 ID、无动作**的通知项。三条通知都**不提任何 skill 名字**——校准的输入受本节末尾的隐私边界管辖，
只计数，不外印。

#### 4.1.3 置信度天花板仍然不动

校准成立也**不**抬天花板。它除掉的是**假阴性**来源，而封顶这个检查的是另外两件事：这是**一台机器**的
历史，而且最多**64 个会话**。"在这台机器最近 64 次会话里没用过"不等于"没用过"。
`UnattendedSafe()` 依旧结构上不可达。

置信度映射（`usageSource.confidence()`）：

- 有 transcript → `medium`
- 只有 `history.jsonl` → `low`
- **永远到不了 `high`**：这是一台机器的历史，"这里没用过"不等于"没用过"

两个记录都没有 → 产出一个**没有 ID、没有动作**的通知项，明确说"检查没能跑"，
以免被误读成"检查过了没发现"。

**🔒 隐私边界（这一条改代码时必须守住）：** transcript 里有工具打印过的任何东西，包括凭据。
`usage.go` 因此**只做成员测试**：
- 从不返回内容，从不存行
- 它读到的任何东西不得进入 finding、报告或 JSON 输出
- 流式读取，单文件上限 `maxUsageBytes = 8 MiB`（**读最近的那 8 MiB，不是最前面的**），
  文件数上限 `maxUsageFiles = 64`（按 mtime 倒序）。**两个上限撞上了都出通知**——
  字节这个曾经从文件头读且完全不出声，实测把校准需要的那条调用记录整个切掉了
  （[`measurement-usage-records.md`](measurement-usage-records.md) §8.5）

### 4.2 `context_bloat`（描述过长）

`estimateTokens(description) > bloatThresholdTokens`（200）。Tier `C`，
`Action = line-delete`，带 blocker `content-edit-unimplemented`——**项是真的，动作还没实现**。

`estimateTokens` 是 `(rune数+3)/4`，明确标注为估算，CJK 混排误差较大。

### 4.3 `duplicate_fn`（描述高相似）

描述分词后的 Jaccard ≥ 0.6。Tier **`A2`**，因为动作不是"删掉这个"而是"选留哪一个"——
**没有安全默认值的问题，无论相似度多高都不能进批量**。

blocker 叫 `side-selection-required`（原来叫 `side-selection-unimplemented`）。
**改名不是措辞问题**：执行器落地之后，"还没实现"就成了假话，而一条被标成"还没实现"的约束
迟早会有人去"补完"它——补完的方式必然是挑一个默认边，那恰好是这一项存在的理由被抹掉的样子。
现在这个名字说的是一条**常驻要求**。

#### 4.3.1 `clean.Resolve`：blocker 是被回答的，不是被绕过的

```
--keep <name> ──► chooseSide  ──► item.AnswerBlocker(BlockerSideSelection)
                                        │
                                        ▼
                                  Executable()  ◄── 和 zombie 同一个门
                                        │
                                        ▼
                                   quarantine()  ◄── 和 zombie 同一条流水线
```

**没有旁路。** `Resolve` 不会跳过 `Executable()`，它只是把操作者刚刚回答掉的那**一个**理由从副本里
去掉，然后照常过门。所以一对里有一侧是软链装到 root 外的，加了 `--keep` 之后**仍然拒绝**——
`TestResolve_StillRefusesWhenAnotherBlockerRemains` 钉住。两个门迟早会不一致，这里只有一个。

移动走 `quarantine()`，与 zombie 完全同一条路径（同样的 `quarantinable`、同样的内容哈希复核、
同样的 `intended → rename → done`）。manifest 行的 `Kind` 记 `duplicate_fn`，`Undo` **不按 kind 过滤**
——一个人做出的选择，和一次自动清扫在可逆性上完全同级。

**`--keep` 精确匹配，连空格都不 trim。** 目录名可以以空格结尾，`browse` 与 `browse ` 并存时
trim 会选中错的那个，把操作者说要留的搬走。

#### 4.3.2 `--ask`：交互不削弱 A2

A2 的不变量是**每一次选边都有一个人做出来**，不是"一个进程只能处理一对"。所以逐对提问、
逐对按键是完全相容的；不存在"全都这样办"的键。

三层 fail-closed：

| 情况 | 行为 |
|---|---|
| stdin 不是终端 | **报错**，并指出 `--keep` 的写法。不提示、不默认、不阻塞 |
| 是终端但进不了 raw mode | 退回编号提示（`pickLine`） |
| 读到 EOF | **当作 abort**，绝不当作确认（`TestPick_EOFIsAbortNotConfirm`） |

**它自己不是执行器**：收到的答案原样交给 `Resolve`，和手打 `--keep` 走同一条路。

按键解析 (`decode`) 和状态机都在无终端依赖的文件里，用字节 fixture 全测；
只有 `termraw.go` 那约 30 行碰真实终端，且两个入口是**包级变量**，测试可以替换掉它们。

**一个真机上量出来的坑**：`--resolve` 最初用 pflag 的 `NoOptDefVal` 让裸 `--resolve` 表示交互。
代价是 `--resolve <id>` 这种**空格写法当场失效**——值掉到位置参数上，flag 取默认值，于是
"回答这一对"被静默改写成"逐对问我"。交互模式因此改成独立的 `--ask`。

#### 4.3.3 `--keep-both`：唯一一个写配置的清理动作

把这一项的 ID 追加到 `<root>/.aguardignore`。基线因此认两种条目：规则 ID（`INJ-004`，字母 + 三位数字）
和清理项 ID（`D-121d192b`，单字母 + ≥8 位十六进制），形状天然可分，老基线原样解析。

四条边界：

1. **目的地由 root 算出**，绝不取自项里的数据。
2. **`.aguardignore` 是符号链接就拒绝**——和 trash、manifest 同一条理由。
3. **只追加**，从不重写或重排操作者自己写的东西。
4. **任何攻击者可控的字节都不原样落盘**（`commentSafe`）。这是这个文件里最要命的一行：
   skill 名就是目录名，由 artifact 作者起，而 Unix 目录名**可以包含换行**。一个叫
   `evil\n*  **` 的 skill 会让注释断行，追加出一条压掉**所有路径上所有发现**的规则——
   而操作者以为自己只是说了"别再问我这两个"。基线是全工具唯一一个"注入一行就让工具闭嘴"的文件。
   `TestKeepBoth_CannotInjectABaselineRule` 钉住。

抑制照旧**被计数并说出来**（不变量 #5）。清理项没有严重度可镜像，所以那个计数就是全部披露。
ID 是内容寻址的，任一侧改名/移动 → ID 变 → 这一项**自动重新出现**。失效方向只能是"多显示"。

### 4.4 `stale_ref`（链接指向不存在的文件）

只匹配 markdown 链接 `](../../doc/path)` 且扩展名在白名单内。**反引号里的裸路径不匹配**——散文里那几乎
总是示例（`src/foo.ts`），是个大误报源。

解析顺序：先相对 skill 目录，再相对祖先项目根（含 `.git`/`.claude`/`package.json`/`go.mod`
标记的目录，上溯 8 层）。全都找不到才算失效。

**故意不可执行**（`Action = ActionNone`）："修好一个失效链接"没有确定性正确答案。

### 4.5 ID 生成（`id.go`）

输入 = `(kind, 排序后的规范化定位键集合)` 的 SHA-256，前 `idHexLen = 8` 个 hex，冲突则加长。

**为什么不能更短**（注释里的两条理由）：16 bit 在 50 项时碰撞概率约 2%，而 ID 命名的是破坏性动作；
更要紧的是哈希输入含**产物作者可控的目录名**、算法公开，短哈希不只是运气问题而是**可伪造**的。

---

## 5. 执行侧：`clean.Apply`

按实际执行顺序列出全部门禁。审计时逐条对照。

| # | 检查 | 失败后果 | 为什么在这里 |
|---|---|---|---|
| 0 | `collect.QuarantineUnsafe(root)` | 返回 error（exit 2） | root 若在 Claude Code 递归加载的树里，trash 目录也在那棵树里，被"隔离"的内容照旧每次会话进 context。**dry-run 也检查**——读计划的人有权知道计划是废的 |
| 1 | `h.Kind != "zombie"` | 跳过 | 今天只有 zombie 有执行器 |
| 2 | `h.Executable()` | 计入 blocked 并**点名报告** | 只看 Kind 会移动一个自己说"我不能被移动"的软链安装 skill |
| 3 | `reportPending` + `verifyChain` | 只报告 | **在提前返回之前**报。崩溃后 `skills/` 里已经没东西了，目标数为 0——恰恰是警告被抑制的那一刻 |
| 4 | `pathByName[t.name]` 存在 | 跳过并计数 | 今天 CLI 到不了，但存储的计划可以指向任何东西 |
| 5 | `quarantinable(root, src)` | 跳过并计数 | 见 §7 |
| 6 | 磁盘上的 `contentHash(src) == t.hash` | 跳过并计数 | **扫描 → rename 之间的窗口**。旧实现拿 item 的 locator hash 比 artifact 的 hash，那是同一个值的两份拷贝，真实窗口从未被检查 |
| 7 | `safeTrash(root, create)` | 返回 error | 见 §7。`dry-run` 传 `create=false`，**不创建目录** |
| 8 | `lock(root)` | 返回 error | 见 §8.3 |
| 9 | `contentHash(src) != ""` | 跳过并计数 | 空哈希会让 undo 的身份校验静默失效，所以**拒绝写这条记录**而不是写一条弱记录 |
| 10 | 写 `intended` → `os.Rename` → 写 `done` | rename 失败则写 `failed` 行 | 见 §8.1 |

**`freeTrashPath`**：`trash/<name>`，占用则 `<name>.1`、`.2`……所以第二次隔离同名 skill 不会覆盖第一次。
名字先经 `trashName` 压成**单个路径元素**（见 §7 规则 3）。

**输出契约**：有动作则打印 batch id 和 undo 命令；有跳过则打印数量并要求看上面的原因。
**跳过 > 0 → exit 3**（`partialGate`）。

---

## 6. 撤销侧：`clean.Undo`

`batch` 可以是 batch id 或 `"last"`。`restorable()` 把 `last` 定义为
**最近一个还有未撤销项的批次**——已经撤销过的批次不是"撤销我上一个动作"的答案。

### 6.1 五重再推导

每一条都**从文件系统重新推导**，任何一条单独就足以拒绝：

| # | 检查 | 防的攻击 |
|---|---|---|
| 1 | 两端都在 root 内（`withinDir` / `withinDirAllowMissing`） | 越界写 |
| 1b | **源必须在 trash 内**，且不是软链（`withinTrash` + `isSymlink`） | 只查"在 root 内"让 undo 变成通用移动原语：一行 `to: <root>/settings.json` 就把 deny 列表搬走并在 `from` 处落下攻击者的版本，还报 `restored`、exit 0 |
| 2 | `protected(root, r.From)` **和** `r.To` 都不是安全配置 | 安全配置永不是恢复目的地，**也不是恢复来源**——把它搬走和覆盖它是同一种损失 |
| 3a | `r.Hash != ""` | 无哈希的行曾经会跳过身份校验，攻击者只要**省掉这个字段** |
| 3b | `contentHash(r.To) == r.Hash` | 换货：隔离恶意 skill → 替换隔离副本 → 等操作者改主意 → 恢复装上替换品 |

外加两个存在性检查：目的地已存在则跳过（**不覆盖**），隔离副本消失则跳过。

### 6.2 哈希链坏了不拒绝恢复

`verifyChain` 的结果**只报告，不阻断**。理由写在代码注释里：文件已经不在 `skills/` 了、正躺在
trash 里，**拒绝恢复只能保证 manifest 存在的意义所要防止的那个损失一定发生**。
恢复本身的安全性来自上面五条（每条都从磁盘重推导）；链的职责是告诉操作者"历史被改过或丢过"——
这是他们没别的办法能看到的——并让他们带着怀疑去读接下来的预览。

### 6.3 恢复前必须预览

见 §9。这是防"协同改写"（同时改内容和 manifest 里的 hash）的**唯一**有效手段。

---

## 7. 路径安全：`internal/clean/paths.go`

文件头写明：这里每一个函数都对应一个**被实测验证过的攻击**，共同根因是
"对写下来的路径而不是解析后的路径做判断"。三条规则现在全局成立：

1. **先解析再判断。** `filepath.Abs` → `EvalSymlinks`；目的地不存在时解析**最近的存在的祖先**
2. **按文件系统的方式比较段。** macOS 大小写不敏感、Windows 忽略尾随点和空格，所以
   `Hooks`、`hooks.` 和 `hooks` 是同一个目录。这个二进制**同时交叉编译到两个平台**
3. **名字是标签，永远不是地址。** 叫 `synced/websearch` 的 skill 不能操纵自己隔离副本落在哪

### 函数与它们各自防住的东西

| 函数 | 作用 | 被防住的实测攻击 |
|---|---|---|
| `segEqual` | 折叠路径段 | 把目录拼成 `Hooks` 或 `hooks.` 绕过保护 |
| `resolved` | 解析（含不存在的目的地） | 软链父目录把最终路径偷运到别处 |
| `isSymlink` | 判断自身是否软链 | 跟随软链让决策落到调用者没点名的地方 |
| `trashName` | 名字压成单个路径元素 | `synced/websearch` 曾让每次 apply 在**写完写前记录之后**才 ENOENT 失败，留下一堆和崩溃无法区分的行；`../` 会走同一条路 |
| `safeTrash` | 验证隔离目录地址 | ① trash 是软链 → `ln -s /tmp/exfil <root>/.aguard-trash` 把内容移出扫描范围，分数 50→100、`--fail-on high` 从 1 翻到 0，而报告还在说"仍在配置根内所以仍然计分"，**`clean --apply` 成了从红到绿的最短路径**；② 解析后落在 Claude Code 递归加载的树里 → `ln -s <root>/rules <root>/.aguard-trash` 把载荷送进 `rules/<name>/SKILL.md`，每次会话加载 |
| `withinTrash` | 恢复的源必须在 trash 内 | 见 §6.1 的 1b |
| `quarantinable` | 移动的源是否合法 | Apply 曾只查容纳性，Undo 额外拒绝安全配置——**两者对"什么能动"意见不一致，而缝隙恰好是绝不能动的那棵树**：`skills/x → <root>/hooks` 的安装软链让 apply 搬走整个 hooks 树（所有 hook 静默停止触发），undo 随后拒绝放回去 |

**`quarantinable` 现在保证 Apply 的许可集是 Undo 可恢复集的子集**（同一个 `protected()` 调用，
外加"源必须真的是 `skills/` 下的已安装 skill"）。改这两个函数中任何一个时**必须同时看另一个**。

---

## 8. manifest：`internal/clean/manifest.go`

### 8.1 写前记录（write-ahead）

`intended` → `os.Rename` → `done`。中间崩溃留下一个没有 `done` 的 `intended`，
`pending()` 找出来、`reportPending` 报出来。

`failed` 状态存在的原因：rename 失败若留下裸 `intended` 行，`pending()` 无法与崩溃区分，
于是**每次后续运行都会伪造一条"上次被中断"的警告**，告诉操作者去手工恢复一个从未移动的文件。

`appendRecord` 结束前 `f.Sync()`，所以刚记下的意图不会因紧接着的崩溃丢失。

### 8.2 哈希链：能检测什么，不能检测什么

`Record.Prev` 的注释是这一段的权威说明，这里只做索引。

**能检测**（都有测试钉住）：
- 中间某行被**删除** → 下一行的 `Prev` 不匹配（`TestChain_DetectsADeletedRow`）
- 中间某行被**编辑** → 同上（`TestChain_DetectsAnEditedRow`）
- 半写/不可解析的行（截断写入、崩溃的编辑器、部分同步）

**不能检测**（同样有测试钉住）：
- **编辑最后一行是不可见的**（`TestChain_CannotDetectEndOfFileTampering`）。锚点在被保护的数据内部，
  没有后继来绑定末端。配合替换隔离内容还能同时骗过身份校验，因为两边达成一致
- 整个文件清空是不可见的：没有行就没有链可验
- 只删最后一行会被抓到，但**靠的是另一个机制**：写前记录成对出现，缺 `done` 会留下未确认的 `intended`

**所以诚实的说法是"完整性"，不是"防篡改"**，README 写的就是完整性。修掉剩下的需要把链头锚在攻击者
到不了的地方，而每个选项都要牺牲一个这个工具被选中的理由（root 里的第二个文件同样可写、root 外违反
clean 自己的容纳规则、keyring 要平台特定代码、签名要地方存私钥）。
[`issues/015`](../../issues/015-manifest-integrity-not-tamper-proof.md) 记录了为什么这笔交易不明显值得做：
**能写 `.aguard-trash/` 的攻击者已经能直接写 `skills/`，所以这买到的是隐蔽性，不是权限。**

`verifyChain` 读**原始字节**而不是重新序列化解析后的行——链的定义是在盘上的字节之上，
重新序列化会静默修复一个差异（重排的 key、多余的空格），而那恰恰是验证该发现的东西。

`Prev` 为空的行是链存在之前写的，报告为"不可验证"，**从不拒绝**——
让一次升级导致一个批次无法恢复，本身就是数据丢失。

### 8.3 锁

`lock()` 用 `O_EXCL` 锁文件，**故意不用 flock**：这个二进制在 `CGO_ENABLED=0` 下交叉编译到 Windows，
Unix-only 的建议锁在那里会静默退化成完全没有锁——那是最坏结果，因为它防的失败
（两个并发 apply 算出同一个空闲 trash 路径、一个覆盖另一个）是**静默的数据丢失**。

锁文件内容是 `pid=… at=…`，拿不到锁时打印出来并给出 `rm` 提示。

**⚠️ 这个锁只保护两个 aguard 运行之间。** 它不保护"Claude Code 正在跑"这种情况——实测确认
Claude Code 会 live watch `skills/`/`commands/`/`agents/`。

**这仍然不是代码强制，但已经不只是文档约定了**：`reportSessionHazard` 会在**即将移动东西的运行**上
打印这条提醒（`Apply` 与 `Undo`，dry-run 也算，因为那是做决定的地方）。

**作用域是这条改动的全部价值**：它**不会**出现在普通 `clean` 上（那是大家真正常跑的命令），
也不会出现在无事可做的运行上。**在一次罕见的、刻意的动作发生时给出的提醒会被读到；
每次调用都印的同一句话是墙纸**——报告里那个恒为 0 的"无人批量"计数刚因为这个理由被删掉。

**为什么现在还不做成检测。** 原来的理由是"没测过 Claude Code 什么时候刷盘"。
**现在测过了：持续刷盘**（[`measurement-usage-records.md`](measurement-usage-records.md) §8.4——
一次 skill 调用后 transcript 在几秒内就增长了 16 KB）。

所以"最近 N 秒内有 transcript 被写"**确实是**一个信号，只是不判决——会话可能刚结束。
这条待办因此从"需要先测量"变成"可以设计了"：要定的是阈值、误报时的行为（拒绝执行？还是只加重
警告？），以及一个刚结束的会话被误判成运行中时，操作者怎么强行继续。**这些是设计决定，不是未知数。**

### 8.4 `openManifest` / `readManifest` 拒绝非常规文件

`os.OpenFile` 跟随软链，所以 `ln -s /outside/victim <trash>/manifest.jsonl` 曾让每次 apply
把工具控制的行追加到扫描根之外的文件里——并把恢复记录指向操作者不掌控的存储。
两个入口都先 `Lstat` 并要求 `Mode().IsRegular()`。

---

## 9. 恢复预览：`internal/clean/preview.go`

**这是这段代码里最反直觉但最重要的一块，改之前请先读文件头注释。**

§6.1 的五重检查回答"这一行能不能动"，**不回答操作者真正的问题："我正要放回去的是什么"**。
两者在一次实测攻击里分开了：

```
隔离一个无害 skill
替换隔离副本为 curl … | bash
把 manifest 行的 hash 字段改成新内容的哈希
→ clean --undo last  →  "restored dead → skills/dead"
```

身份校验通过了，因为它拿 trash 内容比对**从 manifest 读来的**哈希——而那是任何有配置目录写权限
的东西都能编辑的文件。**这是循环验证，同一个文件里加多少密码学都破不了这个循环。**

这次攻击真正买到的不是权限（能写 `.aguard-trash/` 的已经能直接写 `skills/`）而是**隐蔽性**：
载荷伪装成"操作者在恢复自己的文件"。所以便宜且有效的答案不是更强的校验，而是
**在东西放回架子之前让操作者看一眼盒子里是什么。**

`previewRisk` 是这里真正回答问题的部分：**它跑完整的静态引擎**
（`collect.CollectTarget` + `detect.New().Run`，与 `aguard check <path>` 同一条管线），
打印它会发现什么。实测的那次攻击里载荷在 SKILL.md 的**正文**：列目录只看到 `SKILL.md`，
读前三行只看到 frontmatter，**两者都看不到 `curl … | bash`，规则能看到。**

三个细节：

- **内容打印前脱敏**（`detect.Redact`）。内容来自隔离树，正是这个工具存在的理由所要怀疑的东西，
  一个把凭据泄进终端或 CI 日志的恢复预览是自伤版的它所报告的泄露
- **失败被故意吞掉**：预览跑不起来不能阻断恢复，因为拒绝恢复不让任何人更安全，只保证损失发生
- **dry-run 也打印**，预览就是 dry-run 的全部意义

---

## 10. 与 `scan` 的耦合

| 耦合 | 在哪 | 为什么 |
|---|---|---|
| **隔离物仍然计分** | `collect.collectQuarantine` 产出 `KindQuarantined` artifact，`detect` 照常扫它 | 实测：不收集的话，隔离一个已知恶意 skill 让环境从 69/100 变成干净的 100/100。**`clean --apply` 不能洗分** |
| `QuarantineUnsafe` | `collect/loaded.go`，被 `Apply` 和 `safeTrash` 共用 | 一个判断，两处使用，不可能漂 |
| `TrashDir` 常量 | `collect` 包定义 | 收集器要知道跳过/收集哪个目录，clean 要知道往哪写 |
| `contentHash` | 复用 `collect.TreeHash` / `collect.FileHash` | "未改变"在整个工具里是同一个意思，也是声誉库的键 |
| `ScanResult.ToolVersion` | 写进 manifest 的 `tool_version` | 事后能知道哪个版本做的 |

---

## 11. 不变量审计表

**这一节是给审计用的。** 每条不变量 → 在哪强制 → 哪个测试钉住。改代码后跑一遍这些测试。

| 不变量 | 强制点 | 测试 |
|---|---|---|
| `scan` / `check` 永不写盘 | 无写调用 | 全套（任何写会破坏 hermetic fixture） |
| `clean` 不带 `--apply`/`--undo` 只读 | CLI 分支 | `TestApply_DryRunTouchesNothing` |
| dry-run 不创建 trash 目录 | `safeTrash(root, false)` | `TestApply_DryRunCreatesNoTrashDir` |
| 永不删除，只移动 | 只有 `os.Rename` | 全部 clean 测试 |
| 隔离可逆 | manifest + `Undo` | `TestApply_QuarantineReversible` |
| 只动 `skills/` 下的东西 | `quarantinable` | `TestApply_RefusesSourceOutsideSkills` |
| 安全配置永不被移动 | `quarantinable` → `protected` | `TestProtected_ResolvesSymlinkedAlias` |
| 安全配置永不是恢复目的地**或来源** | `Undo` 检查 2 | `TestUndo_RefusesPoisonedManifest` |
| 恢复的源必须在 trash 内 | `withinTrash` + `isSymlink` | `TestUndo_RefusesSourceOutsideTrash` |
| 精确使用判据只在本机自证后启用 | `calibration.active` + 逐 skill 步骤 1 | `TestUsage_CalibrationDeclinesOnUnknownInvocationName` · `TestUsage_CalibrationEnablesExactMatch` |
| 校准退回 / 读文件截断必须出声 | `usageCoverageNotes` | `TestUsage_NewestTranscriptsWinTheBudget` · `TestUsage_UnrosteredSkillKeepsTheLoosePredicate` |
| 手打过的 skill 永远算用过 | `usageEvidence.used` 先看 `prompt` | `TestUsage_TypedPromptSurvivesExactMatching` |
| 选边必须由人给出，绝无默认 | `chooseSide` 空 keep → error | `TestResolve_WithoutAChoiceMovesNothing` |
| 回答一个 blocker 不回答其它 | `AnswerBlocker` + `Executable()` | `TestResolve_StillRefusesWhenAnotherBlockerRemains` |
| 交互无终端时拒绝而非默认 | `stdinIsTerminal` 守卫 | `TestResolveInteractive_RefusesWithoutATerminal` |
| 输入耗尽算中止，不算确认 | `pick` 的 EOF 分支 | `TestPick_EOFIsAbortNotConfirm` |
| skill 名不能注入基线规则 | `commentSafe` | `TestKeepBoth_CannotInjectABaselineRule` |
| 无哈希的行不可恢复 | `Undo` 检查 3a | `TestUndo_RefusesRowWithoutHash` |
| 隔离内容被换过则拒绝恢复 | `Undo` 检查 3b | `TestUndo_RefusesSwappedContent` |
| 协同改写（内容+hash 一起改）会被**宣告** | `preview` / `previewRisk` | `TestUndo_CoordinatedRewriteSucceedsButIsAnnounced` |
| trash 是软链则拒绝 | `safeTrash` | `TestApply_RefusesSymlinkedTrash` |
| root 在被加载的树里则拒绝 | `QuarantineUnsafe` | `TestApply_RefusesRootInsideALoadedTree` |
| 名字不能当地址 | `trashName` | `TestApply_FlattensNameWithSeparator` |
| 扫描后内容改变则跳过 | `resolveTarget` 的 hash 复核 | `TestApply_SkipsWhenContentChanged` |
| 被拦下的项被计数**并点名** | `reportBlocked` | `TestApply_BlockedItemsAreCountedAndNamed`、`TestApply_SkipsBlockedItem` |
| 中断的移动会被报出来 | `pending` + `reportPending` | `TestManifest_PendingIsReported`、`TestUndo_SurfacesInterruptedMove` |
| 失败的移动不伪装成崩溃 | `stateFailed` | `TestApply_FailedMoveDoesNotLookLikeACrash` |
| 并发 apply 互斥 | `lock` | `TestLock_IsExclusive` |
| manifest 不能被重定向 | `openManifest` / `readManifest` 的 `IsRegular` | `TestManifest_RefusesNonRegularFile` |
| 链能发现删行/改行 | `verifyChain` | `TestChain_DetectsADeletedRow`、`TestChain_DetectsAnEditedRow` |
| 链**发现不了**改末行（已知边界） | —— | `TestChain_CannotDetectEndOfFileTampering`（**反向断言：修好了这个测试会失败**） |
| 链前的旧行不可验证但不被拒 | `verifyChain` 的 `Unchained` | `TestChain_PreChainRowsAreUnverifiableNotRefused` |
| 链断了警告但不阻断恢复 | `reportChain` | `TestChain_BreakWarnsButDoesNotBlock` |
| 隔离物仍然计分 | `collectQuarantine` | `internal/collect` 侧的测试 |
| transcript 内容永不进输出 | `usage.go` 只做成员测试 | 靠代码结构（函数只返回 `map[string]bool`） |
| 跳过 > 0 → exit 3 | `partialGate` | CLI 测试 |
| skill 名单不算使用证据 | `hygiene.isSkillListing` | `TestUsage_SkillListingIsNotEvidenceOfUse` |
| 只跳过名单记录，普通行照常匹配 | 同上 | `TestUsage_OnlyTheListingRecordIsSkipped` |
| 子代理 transcript 也算证据 | `usedNames` 的遍历 | `TestUsage_SubagentTranscriptsAreRead` |
| 会话风险提醒只在真要动东西时出现 | `reportSessionHazard` 的调用位置 | `TestSessionHazard_ShownOnlyWhenSomethingMoves`（含两条**否定**断言） |
| 无人批量档结构上不可达 | 置信度天花板（`usageSource.confidence`） | `TestConfidenceCeiling_KeepsUnattendedUnreachable`（**穷举证明 + 反向断言**） |
| 没有检测被接成 TierAuto+ConfHigh | `hygiene.Analyze` | `TestNoDetectionIsUnattendedSafeYet`（**反向断言**） |

---

## 12. 退出码

| 码 | 含义 |
|---|---|
| 0 | 完成 |
| 2 | **拒绝执行，什么都没改**（root 位置不对、trash 地址不合法、拿不到锁） |
| 3 | **部分执行**，原因已打印 |

`clean` 不使用 1（那是 `scan`/`check` 的 `--fail-on`）。

⚠️ 3 的存在理由：返回 0 后什么都没移动，会让 `aguard clean --apply && deploy` 相信清理跑过了。

---

## 13. 已知限制

| 限制 | 记录在 |
|---|---|
| 只有 zombie 有执行器；bloat / duplicate / stale 全部 report-only | [`012`](../../issues/012-clean-scope-and-remaining-gaps.md) |
| 逐项勾选交互（`-i`）未接入 CLI | [`012`](../../issues/012-clean-scope-and-remaining-gaps.md) |
| A2 永远需要人选边（这是要求，不是缺口） | blocker `side-selection-required`，`--resolve/--ask` 提供答法 |
| 软链安装的 skill（`~/.agents/skills/*`）无法被隔离 | blocker `target-outside-root` |
| 哈希链是完整性，不是防篡改 | [`015`](../../issues/015-manifest-integrity-not-tamper-proof.md) |
| `UnattendedSafe()` 结构上不可达，"派生态自动档"方案已实测否决 | [`018`](../../issues/018-clean-autoapply-rejected.md) |
| 锁不保护"Claude Code 正在运行"（已在移动前提醒，但无法检测） | §8.3 |
| zombie 置信度上限 medium，且是单机历史 | §4.1 |
| **skill 调用的 transcript 形态从未被观测**，修复只除掉已知错误，未证明判据正确 | [`measurement-usage-records.md`](measurement-usage-records.md) §8 |
| `maxUsageFiles = 64` 的含义在修复后变了，需重新评估（但要先做上一条） | 同上 §9 |
| token 数是估算，CJK 误差大 | §4.2 |
| `skills/synced/` 由同步方拥有，清理它下次同步会回来 | [`012`](../../issues/012-clean-scope-and-remaining-gaps.md) |

---

## 14. 改这块代码时的检查表

1. **动了 `quarantinable` 或 `protected`？** 两个一起看。Apply 的许可集必须仍是 Undo 可恢复集的
   子集，否则会重现"apply 搬走了 undo 拒绝放回"的状态
2. **动了路径判断？** 三条规则（§7）逐条对照，并想清楚 macOS 大小写和 Windows 尾随点
3. **加了新的 `Kind` 或执行器？** 先确定它的 Tier 与 Confidence，再确定 blocker。
   **不要为了让它可执行而放宽 `Executable()`**
4. **动了 manifest 格式？** `Prev` 是在**原始字节**上算的，任何字段顺序或序列化变化都会让
   已有的链在升级后断掉。加字段要想好旧行怎么办（参考 `Unchained` 的处理）
5. **动了 `preview`？** 记住它是防协同改写的唯一手段，且必须脱敏、必须失败静默、
   必须在移动**之前**打印
6. **动了退出码？** 3 是脚本唯一能读到"部分失败"的方式
7. **新增了"不做某件事"的边界？** 按仓库约定写**反向断言**的测试
   （修好之后测试会失败，提醒同步更新断言和 issue 状态）
8. 跑 §11 表里的全部测试，加上 `go test -race ./...`
