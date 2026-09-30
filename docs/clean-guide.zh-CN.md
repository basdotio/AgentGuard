# `aguard clean` — 使用说明

> 这是**使用**说明。要审计实现或改这段代码，看 [`clean-internals.md`](internals/clean-internals.md)：
> 那里有数据流、每一道门禁的顺序、不变量与钉住它的测试的对照表，以及改动前的检查表。

`clean` 是 aguard 的清理面。它回答两个问题：**哪些东西在白占上下文**，以及**哪些可以安全地移走**。

一句话记住它的边界：**`clean` 从不删除任何东西**。它只在你的配置根目录内部**移动**文件，把每一步写进可回放的记录，`--undo` 能原路放回。删除永远是你自己动手。

---

## 1. 最短路径

```bash
# 1. 先看。纯读，不碰磁盘。
aguard clean --root ~/.claude

# 2. 想让它找出"装了但没用过的 skill"，需要显式开
aguard clean --root ~/.claude --zombie

# 3. 预演。打印计划，不创建任何目录、不移动任何东西
aguard clean --root ~/.claude --zombie --apply --dry-run

# 4. 真做。移动到 ~/.claude/.aguard-trash/，打印 batch id
aguard clean --root ~/.claude --zombie --apply

# 5. 后悔了
aguard clean --root ~/.claude --undo last
aguard clean --root ~/.claude --undo b20260813T074252-1   # 或指定 batch

# 6. 确认无误后，删除由你自己执行
rm -rf ~/.claude/.aguard-trash
```

**重复的 skill 走另一条线**，因为它要你选边，不能批量：

```bash
# 一对一对问过来（↑/↓ 选，回车确认）
aguard clean --root ~/.claude --ask

# 或者点名一对，在命令行里把选择说清楚
aguard clean --root ~/.claude --resolve D-0cb2ed5b --keep browse
aguard clean --root ~/.claude --resolve D-0cb2ed5b --keep-both
```

两条路的执行完全相同（同一个门禁、同一份 manifest、同样 `--undo`），区别只在**你怎么把选择说出来**。
`--ask` 详见 §4.1。

`--root` 应当指向**配置根**：`~/.claude` 或 `<项目>/.claude`。指向别处会被拒绝，原因见 §5。

### 1.1 flag 速查

| flag | 作用 | 会写盘吗 |
|---|---|---|
| （无） | 列出全部清理项 | ❌ 纯读 |
| `--zombie` | 额外找"装了但没用过"的 skill（弱信号，见 §2） | ❌ 纯读 |
| `--json` | 输出 JSON | ❌ |
| `--dry-run` | 配合 `--apply` / `--undo` / `--resolve` / `--ask`：只打印计划 | ❌ |
| `--apply` | 把**全部**可执行的 zombie 移进 `.aguard-trash`（需要 `--zombie`） | ✅ 移动 |
| `--resolve <D-…> --keep <name>` | 回答一对重复：留下点名的那个，另一个移走 | ✅ 移动 |
| `--resolve <D-…> --keep-both` | 两个都留，把这一项记进 `.aguardignore` | ✅ 写配置 |
| `--ask` | 逐对提问（↑/↓ 或数字），答完一次性执行 | ✅ 移动 / 写配置 |
| `--undo <batch\|last>` | 原路放回，每一步重新推导安全判断 | ✅ 移动 |

**互斥**：`--apply` / `--undo` / `--resolve` / `--ask` 两两互斥；`--keep` 与 `--keep-both` 互斥，
且都必须配 `--resolve`。写错组合会**报错**，不会挑一个执行——一次带着选择的命令被静默丢掉选择，
是这个工具修过的缺陷之一。

---

## 2. 它能报什么

| kind | 含义 | 可执行 | 为什么 |
|---|---|---|---|
| `zombie` | 在使用记录里从未出现过的 skill | ✅ 移动 | 唯一有执行器的一项 |
| `context_bloat` | frontmatter 的 `description` 超长（>200 token） | ❌ | 每次会话都占 system prompt；但"改写别人的描述"没有确定的正确答案 |
| `duplicate_fn` | 两个 skill 描述高度相似（Jaccard ≥0.6） | ⚠️ 要你选边 | 动作本身可逆，但"留哪一个"没有安全默认值，所以永远进不了批量。答法见 §4.1 |
| `stale_ref` | SKILL.md 链接到不存在的文件 | ❌ | "修一个坏链接"不是确定性操作 |

每一项都带一个**内容寻址的稳定 ID**（如 `Z-3f2a91b8`），由 kind + 定位键的 sha256 得来，不含显示名和内容哈希 —— 所以环境里多装一个无关 skill 不会改变已有项的 ID。

### zombie 的置信度必须看

zombie 靠**使用记录里有没有这个 skill** 判断，这是弱信号：

- 只有 `history.jsonl`（你手输的 prompt）→ **low**。Claude 自己主动调用的 skill 不会留在那里。
- 还有 session transcript（`projects/*/*.jsonl`）→ **medium**。但仍然只是**这台机器**的历史：在别的机器上用过的，这里读作没用过。
- **永远不会是 high**，所以它永远进不了批量执行。

**两种匹配方式，工具自己决定用哪种。** 默认是"名字在 transcript 里出现过吗"——很钝，一句
"用 pptx 试试"就算数。如果 transcript 里能同时找到 Claude Code 的 skill 名单和真实的调用记录，
工具会先**在你的机器上验证**"调用时记录的名字就是目录名"，验证通过才改用精确的"这个 skill
被调用过吗"。验证不通过、或某个 skill 不在名单里，就**退回**钝的那种——并在输出里说明。
退回永远只会让 zombie **更少**，不会更多。

**每条 zombie 都会告诉你它看了多少次会话**（"Never invoked in the 15 session transcript(s)…"）。
这个数很重要：它是这台机器上**全部**的历史，不是采样。15 次会话里没出现过,跟"从没用过"是两回事——
装了三个月、上个月用过一次的 skill 会稳稳地出现在清单里。**先看这个数,再决定信不信这一条。**

还有两条会写进输出的说明：只读了最近 64 个会话（更早的没看），以及哪些 skill 没用上精确匹配。
看见它们不用紧张，它们说的是"这次的证据比理想情况窄"。

⚠️ **在 Claude Code 会话里跑这条命令，会污染下一次的结果。** 输出里印出的 skill 名字会进
transcript，钝匹配下一次就把它们全读成"用过"。实测：第一次报 7 个，第二次报 0 个，
中间代码一个字没改。

**所以：`--zombie` 请在普通终端里跑，不要在 Claude Code 会话里跑。**
（`scan` / `check` 不受影响——只有 zombie 这一项拿 transcript 当证据。）

精确匹配对这个回路免疫，但它**要校准通过才启用**，而校准需要 transcript 里同时有 skill 名单和
真实调用记录。所以在你自己的机器上它到底生效没有，不要假设——看输出里有没有那条
"precise invocation matching was declined / 某些 skill 没用上精确匹配"的说明。
有那条说明，就说明这次跑的是钝匹配，上面那句规避就是硬要求。

隐私约定：transcript 只做**成员测试**（"这个名字出现过吗"），内容不进任何 finding、报告或 JSON 输出。

---

## 3. 四个 tier：可写性与可批量性是两件事

| tier | 名称 | 可逆性 | 能批量吗 |
|---|---|---|---|
| **A1** | auto | 移动，可 undo | ✅ 可以（且置信度必须是 high） |
| **A2** | choice | 移动，可 undo | ❌ 不行 —— 需要人来选边（但**可以逐项答**，见 §4.1） |
| **B** | config | 改配置 | ❌ 未实现 |
| **C** | content | 改文件内容 | ❌ 未实现 |

两个判据是独立的：

- **tier 决定"能不能写"** —— 一个不可逆的操作不该由这个工具执行。
- **"要不要人来选"决定"能不能批量"** —— `duplicate_fn` 是 A2：动作本身可逆，但"留哪个"是整个决定的核心，不存在安全默认值，所以再高的相似度也不能让它进批量。
  **注意"不能批量"不等于"不能做"**：`--ask` 逐对提问、`--resolve --keep` 逐对点名，每一次选边都由你做出，所以两者都不违反 A2 —— 违反它的是"一个按键/一个 flag 一次答完多对"，那样的入口不存在。
- **置信度单独把关** —— 它不改变 tier，只决定这一项够不够格进批量。

---

## 4. blocker：为什么明明报了却动不了

一项被报出来但不执行时，会带一个 blocker 字符串。这些是稳定标识，`clean --json` 的消费方可以依赖：

| blocker | 含义 |
|---|---|
| `target-outside-root` | 目标解析后落在 `--root` 之外（symlink 安装的 skill）。报告但绝不移动 —— 移动它会伸出你指定的树之外。 |
| `content-edit-unimplemented` | 需要删文件里的行，没有执行器。项目和 token 估算都是真的，只是自动修复缺席。 |
| `target-not-under-skills` | 目标在 root 内但不在 `skills/` 下——软链装的 skill（`skills/x → shared/x`）。只有 `skills/` 是 `clean` 拥有的面，搬真身等于从别人的目录里挖内容。见 [`issues/019`](../issues/019-symlink-installed-skills-report-only.md)。 |
| `target-is-security-config` | 目标是安全配置，永不移动。 |
| `already-quarantined` | 目标已经在 `.aguard-trash/` 里了。 |
| `target-unlocatable` | 目标无法表达成 root 的相对路径，任何判断都不可信，所以拒绝。 |
| `side-selection-required` | 需要你说留哪一个。**不是"还没做"，是一条常驻要求**——选择本身就是这一项要问的东西，所以它永远进不了 `--apply`。答法见 §4.1。 |

### 4.1 怎么回答 `side-selection-required`

三种答法，都要你亲自说：

```bash
# 留 browse，把 devkit 移进 .aguard-trash（可 --undo）
aguard clean --root ~/.claude --resolve D-0cb2ed5b --keep browse

# 两个都留：记下这一项，以后不再列出来
aguard clean --root ~/.claude --resolve D-0cb2ed5b --keep-both

# 一对一对问过来
aguard clean --root ~/.claude --ask
```

`--ask` 每对印出两个候选。在真实终端里用 **↑/↓ 选、回车确认**；也可以直接敲 `1` / `2`。
另外 `b` 两个都留、`s` 跳过、`q` 退出。

**不选就什么都没选中**——没按过方向键直接回车、空行、看不懂的输入，一律当"跳过"。**没有默认边。**

不是终端（管道、CI）时自动退回打字模式，并会说一声；读到输入结束就停下，什么都不改。

三条你应该知道的行为：

- **答完才动手。** 它先把所有答案收齐，再一次性执行。所以中途 `q` 退出 = **一件事都没做**，
  哪怕你前面已经答了几对。
- **保留胜过丢弃。** 相似的 skill 会两两配对，`a b c` 会出**三对**，同一个名字可能在这对里被你
  留下、在那对里被丢掉。这种时候**留下的那次赢**，而且会印出来告诉你：
  `keeping b: you kept it in one pair and dropped it in another, and a keep wins.`
- **一次走查 = 一个 batch。** 印出来的 `--undo <id>` 正好撤销这一次，不多不少。

> 早期有过一个方向键版本，因为一批安全缺陷被整体撤回后重做
> （[`issues/020`](../issues/020-interactive-picker-withdrawn.md)）。方向键**回来了**，但换了位置：
> 它现在只是一层"按键 → 你本来也能敲出来的那个字符"的翻译，改不了答案导致什么。
> 出问题的旧版是把终端解码放在了一切之下。

`--keep` 的匹配是**精确的**：不忽略大小写、不认前缀、连首尾空格都不去。
看着不近人情，但目录名可以以空格结尾，`browse` 和 `browse ` 同时存在时"顺手 trim 一下"
就会把你想留的那个搬走。名字打错了它会把两个名字都印出来给你复制。

`--keep-both` 把这一项的 ID 追加到 `<root>/.aguardignore`。ID 是按目标路径算出来的，
所以**任何一边被改名或移动，ID 就变了，这一项自动重新出现**——失效方向永远是"多显示"。
被压掉了几条会写在输出里，抑制永不静默。

**这些 blocker 就是执行器自己的答案。** 计划期调用的是 `clean --apply` 会用的同一个函数
（`clean.QuarantineRefusal`），不是另写一份判断——所以清单里说动不了的，执行时不会突然能动，
反过来也一样。曾经不是这样：软链装的 skill 在清单里没有 blocker、被算进"N executable"，
执行时才被拒。

运行时还会打印这些拒绝原因（不是 blocker，是执行时的实时判定）：

```
skip <name>: security configuration is never moved
skip <name>: not an installed skill (hooks/x); only skills/ is quarantined
skip <name>: already quarantined
skip <name>: content changed since it was listed; re-run `clean` and review again
skip <name>: cannot hash <path>, so the move would not be verifiably reversible
```

---

## 5. 安全边界（这一节是这个命令的核心）

### 永不触碰

`settings.json`、`settings.local.json`、`.claude.json`、`.mcp.json`，以及 `hooks/` 下的任何东西 —— 既**不作为移动来源**，也**不作为恢复目标**。判定在**解析符号链接之后**做，且段名比较忽略大小写与尾部的点和空格（macOS 大小写不敏感，Windows 忽略尾部点）。所以 `hk → hooks` 这样的别名、`Hooks`、`hooks.` 都拦得住。

### 只动 skills/

一个 zombie 按定义就是装好的 skill。解析后不落在 `<root>/skills/` 下的一律拒绝。这条挡住了一整类问题：`skills/x → <root>/hooks` 这样的安装符号链接曾能让 apply 搬走整个 hooks 树（所有 hook 静默失效），而 undo 又拒绝放回去 —— apply 的许可集现在**在构造上**是 undo 可恢复集的子集。

### 隔离目录的地址是被实测过的

`<root>/.aguard-trash/` 不会被 Claude Code 加载 —— 这是**测出来的**，不是推的（见 `docs/internals/measurement-startup-loading.md`，Claude Code 2.1.229）。

但**安全来自位置，不来自"点开头"**。实测：`rules/`、`agents/`、`commands/` 是递归扫描且**不跳过隐藏目录**，`rules/.aguard-trash/x.md` 照样进 system prompt。所以：

- `--root` 落在 `rules`/`agents`/`commands`/`skills`/`output-styles`/`workflows`/`plugins` 里面 → **拒绝，exit 2**。判定前会先转绝对路径并解析符号链接，所以 `--root .`、指向 `rules/` 的符号链接、`--root .../Rules` 都拦得住。
- `.aguard-trash` 本身是符号链接 → **拒绝**。跟着它走会把隔离物移出被扫描的树，那样它就不再被计分 —— 曾经能让分数从 50 跳到 100、`--fail-on high` 从 1 变 0。

这类失败比"漏报"更糟：漏报会让你继续找，**假保证会让你停下来**。

### 隔离物仍然计分

`.aguard-trash` 里的东西作为 `KindQuarantined` 被采集并扫描。移动不是删除，所以分数不该因为移动而变好 —— 这是为了让 `clean --apply` 不能成为把红色分数洗白的捷径。真正让分数变好的是内容确实消失，而那是你手工执行的动作。

### undo 不相信记录

`manifest.jsonl` 是配置目录里的一个普通文件：任何能往那儿写的东西（skill 的安装脚本、一个 hook、一个 MCP server —— 正是这个工具的威胁模型）都能追加一行。所以 undo **重新推导**每一个判断，五条各自独立地足以拒绝：

1. **封闭性** —— 两端都必须在 `--root` 内。
2. **来源必须在隔离目录内** —— 恢复只能是"从垃圾桶里拿东西出来"。只检查"在 root 内"曾让 undo 变成通用的移动原语。
3. **安全配置排除** —— 既不能是目标也不能是来源。
4. **必须有哈希** —— 空哈希曾让身份校验变成可选，攻击者只要不写这个字段。**没有哈希就不恢复。**
5. **内容身份** —— 垃圾桶里现在的东西必须还是当初放进去的那个。否则就是一次替换：隔离掉恶意 skill、把隔离副本换掉、等你改主意，恢复装上的是替换品。

### 恢复前会预览

每一项恢复之前（`--dry-run` 也一样）打印将要放回去的东西：大小、内容哈希，以及**跑一遍静态
规则**告诉你"放回去会报什么"。

```
restoring dead ← ~/.claude/.aguard-trash/dead
      directory, 1 file(s), 68 B, sha256:0b9d9c5c98526b13…
      │ SKILL.md
      ⚠ scan of the quarantined copy: 1 high
      ⚠   EXEC-001 curl piped to shell
```

为什么跑规则而不是只显示头几行：实测下 payload 在 SKILL.md 的**正文**里——列文件名只看到
`SKILL.md`，显示头三行只看到 frontmatter，两者都看不到 `curl … | bash`。规则能看到。

这一条针对的是一个真实场景：有人**同时**替换了隔离副本**和** manifest 里记的哈希，于是身份
校验通过（两边一致）。它拿不到新权限（能写 trash 的东西本来就能写 `skills/`），拿到的是
**隐蔽性**——payload 以"你主动恢复自己的文件"的形式进来。预览把隐蔽性拿掉了。

### manifest 是哈希链（完整性，不是防篡改）

每行带上一行的 SHA-256，读时重算。**能测到和不能测到的都是实测的**：

| 情形 | 检测到吗 |
|---|---|
| 删掉/改写**中间**某行 | ✅ 后一行的 `prev` 不匹配 |
| 半截、不可读的行 | ✅ |
| 删掉**最后**一行 | ✅ 但不是靠链——写前记录成对，缺 `done` 会留下未确认的 `intended` |
| 改写**最后**一行 | ❌ 后面没有行来锚定它 |
| **完全清空**文件 | ❌ 没有行就没有链可验 |

最后两项是"锚点在被保护数据内部"的固有性质。所以这里说的是**完整性可验证**，
**不是防篡改**——详见 `issues/015`，包括为什么外部锚点的每个选项都要付掉这个工具的某个卖点。

**断链只警告，不拒绝恢复。** 文件已经不在 `skills/` 了，拒绝只会**确保**丢失而不是防止丢失。
恢复本身的安全由下面五重校验保证；链的职责是告诉你历史丢过或被编辑过，并让你带着怀疑读预览。

老版本写的记录（没有 `prev`）报告为"无法验证"但**仍然可以恢复**——让一次升级变成一个批次
恢复不了的原因，本身就是数据丢失。

### 写前记录

先写 `intended` 行 → `rename` → 写 `done` 行。中途崩溃留下一个没有确认的 `intended` 行，**下一次运行会报出来**（在"无事可做"的提前返回之前报，因为崩溃后 `skills/` 里已经没东西了，正是那一刻最需要这条警告）。`--undo` 遇到这种半成品也会点名，退出码 3。

rename 失败会写一条 `failed` 行关闭这个意图 —— 否则失败的移动会伪装成崩溃，每次运行多一条"上次被中断"的假警告。

### 排他锁

O_EXCL 锁文件，不是 flock —— 这个二进制要在关闭 CGO 的情况下交叉编译到 Windows，而 Unix-only 的建议锁在那边会静默退化成完全没锁，那是最坏的结果（两个并发 apply 算出同一个空闲路径，一个覆盖另一个，静默丢数据）。

### 名字是标签，不是地址

`synced/websearch` 这样带斜杠的 skill 名会被压成一个路径段（`synced__websearch`）。undo 仍然放回原来的嵌套路径，因为真实的 from/to 记在 manifest 里，不靠命名约定重建。

---

## 6. 退出码

| 码 | 含义 |
|---|---|
| 0 | 正常完成（可能什么都没做，输出会说清楚） |
| 2 | 拒绝执行 —— 地址不安全、锁被占、目标读不了。**没有任何东西被改动。** |
| 3 | 部分跳过 —— 有些做了，有些被拒。逐条点名了原因。 |

3 是刻意可达的：symlink 安装的 zombie、被拒的 manifest 行、失败的 rename 都会走到它。一个"能操作的项一个都没有"的运行不应该和"本来就没事可做"长得一样。

**`--ask` 中途退出（`q` / Ctrl-C / 输入结束）是 0**，因为那不是失败：它先收齐所有答案再一次性执行，所以中止意味着**一件事都没做**，哪怕你前面已经答了几对。

---

## 7. 它刻意不做的事

- **不做运行时数据的保留策略。** Claude Code 自己的 `cleanupPeriodDays`（默认 30 天）按龄清理 transcripts、caches、plans、debug 日志等；`claude project purge` 按项目删状态，带计划和逐项确认。重做一遍只会更差，还引入第二个删除来源。
- **不改写你的文件内容。** `context_bloat` 有量化有 ID，但没有执行器。悄悄重写别人写的描述，比列出来让人自己看是更差的交易。
- **不替你在重复项里选边。** 提供 `--ask` / `--resolve --keep` 让你说出选择，但**没有默认边**：不选就什么都不动。
- **不清理 `skills/synced/`。** 实测那是同步拥有的目录，会自动重填 —— 隔离那里的东西下次同步就还原了。
- **不删除。** 一次都不。

---

## 8. JSON 契约

`clean --json` 输出一个**信封**，不是裸数组：

```json
{
  "schema": 1,
  "tool_version": "v0.1.0",
  "root": "/Users/you/.claude",
  "items": [ { "id": "D-121d192b", "kind": "duplicate_fn", … } ]
}
```

**`schema` 是为什么加信封的全部理由。** 之前是裸数组，**没有任何地方能说明自己**——
所以改一个 blocker 字符串就是一次静默破坏，而且下一次还是。`scan --json` 从来没这个问题：
它是对象，本来就带 `tool_version`。

契约包括三样，**改任何一样都要 bump `schema`**：

1. **JSON 键名**（信封的 `schema`/`tool_version`/`root`/`items`，以及项里的 `id`/`kind`/`tier`/…）
2. **枚举取值**：`tier` 是 `A1`/`A2`/`B`/`C`，`confidence` 是 `low`/`medium`/`high`，
   `action` 是 `""`/`move`/`config-remove`/`line-delete`
3. **blocker 字符串**（§4 那张表里的每一个）

这三样由 [`internal/model/contract_test.go`](../internal/model/contract_test.go) **钉住**——
改一个字符串必须先弄红一条测试，而那条测试的失败信息会告诉你还要同步什么。
加它的原因很直接：`side-selection-unimplemented` 改成 `side-selection-required` 时，
**全套测试一片绿**。改名本身是对的，静默不对。

> **`schema: 1` 相对更早的版本是破坏性的**：`clean --json` 曾经直接输出那个数组。
> 老消费者读 `[0].id`，现在要读 `.items[0].id`。这次破坏是刻意花掉的——
> **静默破坏只能用一次，就用在让以后的破坏都不静默上。**

---

## 9. 已知限制

- **只处理 skill。** rules、workflows、output-styles、memory、导入链、项目级 MCP 都已被 scan 采集并计分，但都不在 clean 的视野里。
- **`--apply` 仍是"移走全部僵尸"，没有 `--apply-items <ids>`。** A2 的选边语法已经有了（`--resolve --keep` / `--ask`，§4.1），但 zombie 还不能逐个点名 —— 要么全做，要么不做。
- **`.aguard-trash` 自身没有保留策略。** 它是我们发明的目录，Claude Code 不会碰它 —— 所以我们自己成了一个新的增长源。清它的责任目前在你。
- **运行中的会话会实时看到变更。** 实测 Claude Code 监视 `skills/`、`commands/`、`agents/`。锁保护两个 aguard 之间，不保护 aguard 与正在对话的 agent 之间。**建议在没有活跃会话时执行 `--apply`。**
- **祖先 `.claude` 的 skill 会被漏掉。** 实测 project 作用域会向上找祖先 `.claude`，`--root <项目>/.claude` 看不到那些。
- **被选中的 output-style 一旦被隔离，settings 里会留一个悬空引用**，目前没有配置完整性检查。

更完整的缺口清单见 `issues/012-clean-scope-and-remaining-gaps.md`。
