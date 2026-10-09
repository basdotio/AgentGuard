<!-- SPDX-License-Identifier: MIT -->
# 019 — 同一个目录换一种写法就换一个答案:`clean --root .` 拒绝撤销,`check .` 不按 root 布局读,相对写法下闸门对插件 skill 不审就放行

- **来源**:P-012 合入后留下的同根因入口(P-012「不做什么」里那条合并 proposal 的 (1)(2)(3),以及未决 7、未决 9);
  人定(2026-10-09)合成一条
- **依赖**:P-012(已在 `main`)
- **分支**:`p/019-raw-root-entry-points`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

P-012 在 `collect.CollectAll` 入口把 root 锚定成绝对路径,`scan` 的报告从此与 `--root` 怎么敲无关。另外几个入口仍拿
**敲进来的字符串**做判断,于是同一个目录的答案取决于路径怎么写:

1. **`internal/clean`**:`withinDir` 对 base 只 `EvalSymlinks` 不 `Abs`(`internal/clean/clean.go:532`),`EvalSymlinks(".")` 还是 `.`,
   和解析后的绝对 trash 路径 `Rel` 不起来,出错即拒 —— 于是 `safeTrash` 把 root 自己的 `.aguard-trash/` 判成 "outside the scanned root"。
   `clean` 命令照旧把 `--root` 原样交给 `clean.*`(`cmd/aguard/main.go` 的 clean `RunE`)。
2. **`check` / `hash` 的路由**:`collect.CollectTarget` 用 `looksLikeRoot(path)` 判"像不像 root",而它看的是
   `filepath.Base(dir) == ".claude"`(`internal/collect/collect.go:110`)—— `Base(".")` 是 `.`,`Base("<abs>/.claude/.")` 也是 `.`。
   工作目录是一个没有 `plugins/installed_plugins.json` 的 `.claude` 时,`check .` 走"其他目录"分支,整棵树读成**一个** `directory`
   artifact;`check ../.claude` 走 root 布局。`hash` 用同一个 `CollectTarget`,打印的身份也跟着变。
3. **闸门和 `version` 从原样 root 取 home**:`cmd/aguard/gate.go` 的 `gateOptions` 写 `Home: filepath.Dir(root)`,
   `cmd/aguard/version.go` 的 `pluginVersionLine` 写 `filepath.Dir(filepath.Clean(root))`。home 交给 `collect.PluginInstalls`,
   它对每个插件做 `withinDir(home, real)`(不变量 #2)—— 相对的 home 和绝对的 `real` 关联不了,出错即拒,**每个 CLI 装的插件都被丢掉**;
   带尾斜杠或尾点的绝对写法(`gateOptions` 不 `Clean`)下 home == root,桌面版装的插件(在 `<home>/Library/…` 下)全找不到。
   闸门找不到 skill 时按设计 fail-open:放行并出 `GATE-000`。

实测(本仓 `main` `fd28344` 构建的二进制;`HOME` 指向 fixture。fixture A:`<home>/.claude` 下 `skills/plain`(`scripts/run.sh` 里
`curl … | bash`)、`skills/linked → <home>/agents-store/linked`(绝对目标,HOME 内)、`settings.json` 一条 hook
`sh ~/.claude/hooks/pre.sh`(`curl … | bash`)、`history.jsonl`、空的 `.aguard-trash/`,**没有** `plugins/installed_plugins.json`。
fixture B:`<home>/.claude/plugins/installed_plugins.json` 装着 `myplug@mk`(skill `hello`,`curl … | bash`)和 `aguard@AgentGuard` 0.18.0,
桌面版仓库里装着 `dplug`(skill `dhello`,`curl … | bash`)。闸门喂 `PreToolUse[Skill]`,`permission_mode: default`):

| `--root` / 目标的写法 | 工作目录 | `clean --undo last --dry-run` | `clean --zombie --apply --dry-run` | `check`(A) | `hash`(A) | `version` 插件行(B) | 闸门 `myplug:hello`(B) | 闸门 `dplug:dhello`(B) |
|---|---|---|---|---|---|---|---|---|
| `<abs>` | 任意 | Nothing to undo,退出 0 | would quarantine plain,退出 3 | 2 skill + 1 hook | 3 行 | "matches this binary" | `ask`(EXEC-001) | `ask` |
| `<abs>/` | 任意 | 同上 | 同上 | 同上 | 同上 | 同上 | `ask` | **`GATE-000`** |
| `<abs>/.` | 任意 | 同上 | 同上 | **1 个 `directory`** | **`directory:.`** | 同上 | `ask` | **`GATE-000`** |
| `<base>/via/.claude`(经符号链接的绝对写法) | 任意 | 同上 | 同上 | 2 skill + 1 hook | 3 行 | 同上 | `ask` | `ask` |
| `.claude` | `<home>` | **拒绝,退出 2** | **拒绝,退出 2** | 2 skill + 1 hook | 3 行 | **无** | **`GATE-000`** | `ask`(碰巧) |
| `.claude/.` | `<home>` | **拒绝** | **拒绝** | **1 个 `directory`** | **`directory:.`** | **无** | **`GATE-000`** | **`GATE-000`** |
| `.`、`./` | root | **拒绝** | **拒绝** | **1 个 `directory`** | **`directory:.`** | **无** | **`GATE-000`** | **`GATE-000`** |
| `../.claude` | root | **拒绝** | **拒绝** | 2 skill + 1 hook | 3 行 | **无** | **`GATE-000`** | `ask`(碰巧) |
| `..` | `<root>/skills` | **拒绝** | **拒绝** | **1 个 `directory`** | **`directory:.`** | **无** | **`GATE-000`** | **`GATE-000`** |
| `home/.claude` | `<base>` | **拒绝** | **拒绝** | 2 skill + 1 hook | 3 行 | **无** | **`GATE-000`** | `ask`(碰巧) |
| `../home/.claude` | `<base>/sibling` | **拒绝** | **拒绝** | 2 skill + 1 hook | 3 行 | **无** | **`GATE-000`** | `ask`(碰巧) |
| `.claude` | 经符号链接到达的 `<home>` | **拒绝** | **拒绝** | 2 skill + 1 hook | 3 行 | **无** | **`GATE-000`** | `ask`(碰巧) |

"碰巧":home 是相对的 `.`/`..`,桌面版仓库的路径从工作目录拼出来也是相对的,`Rel` 恰好算得出。拒绝时的原文是
"refusing to use .aguard-trash: it resolves to …/.aguard-trash, outside the scanned root"(`.claude/.aguard-trash` 等随写法变)。
闸门在相对写法下对 root 里的 skill(`plain`)不丢,但理由里的路径和给出的 `aguard approve …` 命令是相对的(`skills/plain`),
换个工作目录照抄就失败。`clean --json`(只列计划)走 `scanEnv`,P-012 之后已与写法无关。

后果:

- `clean` 的撤销和隔离在 `cd ~/.claude` 之后最自然的写法下**全都不能用**,而 `--undo` 正是出事之后要用的那一条。
- `check .` 在一个 `.claude` 里给出的 artifact 清单、分数构成和哈希与 `check "$PWD"` 不同;`aguard approve .` 记下的是另一份身份。
- 闸门:一个带 high 发现的插件 skill,在绝对写法下 `ask`,相对写法下**不审就放行**,只留一条 `GATE-000`。注册进 settings.json 的
  hook 命令不带 `--root`(用 `defaultRoot`,通常是绝对路径),所以日常加载走不到这里;走得到的是手动喂事件调闸门
  (`CLAUDE.md` 列的最快回路)、相对的 `CLAUDE_CONFIG_DIR`,以及 `--root ~/.claude/` 这类补全出来的写法。
- `version` 在相对写法下不报插件版本,"插件比二进制新/旧"这条提示消失。

P-012 未决 9 那一处(工作目录经符号链接时部分证据路径缩成两段)另算:实测相对写法与**同一条经符号链接的绝对写法**输出相同
(`settings.json` → `.claude/settings.json`,`hooks/deep/x/pre.sh` → `x/pre.sh`,两种写法都这样),它不是写法依赖,而是 detect `relPath`
对经符号链接的绝对路径的既有显示规则。

## 初步方向

导出一个锚定函数 `collect.AnchorRoot`(就是 `CollectAll` 现在内联的那几行:`filepath.Abs`,失败退回 `Clean`,**不** `EvalSymlinks`),
`CollectAll` 改用它;`looksLikeRoot` 对锚定后的路径判;`clean` 在写路径的入口(`quarantine`、`Undo`、`KeepBoth`、`QuarantineRefusal`)
锚定一次;`gateOptions` 和 `pluginVersionLine` 先锚定再取 `Dir`。detect 里同义的 `anchorRoot`(P-010)和 `absRoot`(P-009)改成调用它
(机械替换,两条都已有写法矩阵测试钉着)。单目标的 `check ./skill` 不动;`relPath` 不动(会改绝对写法的输出)。
