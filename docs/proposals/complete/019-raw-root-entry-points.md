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

## 完成的判据

fixture 都在 `t.TempDir()` 现搭,临时目录先 `EvalSymlinks`(macOS 的 `/var → /private/var`),工作目录用 `os.Chdir` 并设 `PWD`、在
`t.Cleanup` 里还原(沿用 P-012 的 `anchorChdir`/`anchoredChdir`;这几个包里没有 `t.Parallel`)。写法矩阵统一为:`<abs>`、`<abs>/`、
`<abs>/.`、`.claude`、`.claude/.`(工作目录 `<home>`)、`.`、`./`、`../.claude`(工作目录 root)、`..`(工作目录 `<root>/skills`)、
`home/.claude`(工作目录 `<base>`)、`../home/.claude`(工作目录 `<base>/sibling`),外加"工作目录经符号链接"(`<base>/via → home`,
写 `.claude`)。

- [x] `TestClean_RootSpellingIsTheAbsoluteRun`(`internal/clean/rootspelling_test.go`,新):root 下一个 zombie skill、artifact 路径是
  collect 给的绝对路径。每种写法依次:`Apply` 预演的输出与 `Result` 等于绝对写法;真 `Apply` 成功;随后 `Undo(…, "last")` 预演的输出
  等于绝对写法对同一批次的预演;真 `Undo` 把 skill 放回原处;`KeepBoth` 预演的输出等于绝对写法;`QuarantineRefusal` 的答案等于绝对写法
  (空,即可以移动)。"经符号链接"那行的路径在链接的坐标系里,把链接映射回 home 之后比。预期 W1 红:除 `<abs>`、`<abs>/`、`<abs>/.`
  以外的九行在第一步就报 "outside the scanned root"(`Apply` 预演返回错误,测试停在这一步)
- [x] 反向断言(同一文件):`.aguard-trash` 是指向 root 外的符号链接时,**每种写法**都拒绝(错误里说 "it is a symlink"),root 外的
  目录里什么都没多出来;`--root` 落在 `rules/` 里面(工作目录 `<root>/rules`,写 `.`)仍以 "inside a \"rules\" directory" 拒绝。
  W1 时就绿,修后仍绿
- [x] `TestCollectTarget_RootSpellingRoutesLikeTheAbsoluteTarget`(`internal/collect/targetspelling_test.go`,新):P-012 的"安装形状"
  fixture(没有 `plugins/installed_plugins.json`),每种写法 `CollectTarget` 都走 root 布局 —— `Result.Root` 等于绝对 root,清单
  (kind、name、hash、path)和 note 等于绝对写法("经符号链接"那行只比 kind、name、hash 和 note 规则,P-012 未决 5)。预期 W1 红五行:
  `<abs>/.`、`.claude/.`、`.`、`./`、`..` 的 `Root` 为空、清单只有一个 `directory`
- [x] 反向断言(同一文件):单目标原样 —— 工作目录 root 下 `CollectTarget("skills/plain")`、`CollectTarget("./skills/plain")` 的
  `Root` 为空、一个 `skill`、`Path` 就是敲进来的字符串;一个**不叫** `.claude`、带 `skills/` 和 `settings.json` 的目录,工作目录在它里面
  写 `.`、在外面写绝对路径,都仍是一个 `directory` artifact(`looksLikeRoot` 没被放宽)。W1 时就绿,修后仍绿
- [x] `TestCheck_RootSpellingInsideAConfigRootIsTheAbsoluteReport`(`cmd/aguard/rawroot_test.go`,新):同一 fixture 走 `checkTarget`,
  JSON(去掉 `scanned_at`)与绝对目标**逐字节相同**("经符号链接"那行用 P-012 的去路径视图)。预期 W1 红五行(同上)
- [x] `TestHashCommand_RootSpellingPrintsTheAbsoluteHashes`(同文件):真二进制在各工作目录跑 `aguard hash <写法>`,stdout 与
  `aguard hash <abs>` 相同。预期 W1 红五行(打印 `directory:.` 一行)
- [x] `TestGate_RootSpellingResolvesTheSamePlugins`(同文件):fixture B 形状 —— CLI 装的 `myplug@mk`(skill `hello`,`curl … | bash`)、
  桌面版仓库里的 `dplug`(skill `dhello`,同上)、root 里的 skill `plain`。每种写法:`gateOptions` 的 `Root` 等于绝对 root、`Home` 等于它的
  上一级;`runHook` 对 `myplug:hello`、`dplug:dhello`、`plain` 三个 `PreToolUse[Skill]` 的回复与绝对写法**逐字节相同**,且都是 `ask`。
  预期 W1 红:九行相对写法 `Root`/`Home` 不同、`myplug:hello` 是 `GATE-000`;`<abs>/`、`<abs>/.` 两行 `Home` 等于 root、`dplug:dhello`
  是 `GATE-000`;`plain` 在相对写法下理由里的路径是相对的。fixture 放在短的临时目录里:闸门把超过 160 个字符的路径截断后再写进理由,
  `t.TempDir()` 带着子测试名,长到"经符号链接"那行的 `via` 和 `home` 会被截在不同的字符上
- [x] 反向断言(同一测试):一个 `installPath` 解析到 HOME 外的插件 `outplug`,**每种写法**都不被解析(`outplug:x` 是 `GATE-000`,
  `collect.PluginInstalls` 不含它)—— 不变量 #2 的插件边界不因锚定放宽。W1 时就绿,修后仍绿
- [x] `TestPluginVersionLine_RootSpelling`(同文件):`pluginVersionLine(<写法>, "v0.18.0")` 每行都等于绝对写法的
  "plugin aguard 0.18.0 matches this binary"。预期 W1 红九行(相对写法返回空串)
- [x] 反向断言:绝对写法的结果先用字面值钉住(clean 预演那行的目标路径、`check` 的 artifact 清单、闸门两条 `ask` 带 `EXEC-001`、
  version 那一行),W1 时就绿,修后不改一字仍绿
- [x] 反向断言:绝对写法的二进制输出修前修后逐字节相同(去掉 `scanned_at`、`tool_version`):fixture A 的 `scan --json`、`check --json`、
  `hash`、`clean --undo last --dry-run`、`clean --zombie --apply --dry-run`,fixture B 的 `version` 第二行和两条闸门回复;
  `check ./skills/plain`、`check skills/plain`(单目标)修前修后逐字节相同;真机 `scan --root ~/.claude` 修前修后 JSON 逐字节相同
  (review 包只贴 overall、artifact/发现/note 数)
- [x] W5(detect 去重)之后:`func absRoot` 不存在,detect 的 `anchorRoot` 只剩一行转调 `collect.AnchorRoot`(名字留着,P-010 的测试注释和
  `detect.md` 引用它),仓库里 `filepath.Abs(root)` 只剩 `AnchorRoot` 自己那一处;变异(临时改、跑、还原,不提交)`collect.AnchorRoot`
  只 `Clean` 不 `Abs` → collect、detect、clean、cmd 四个包的写法矩阵都红,改成 `EvalSymlinks(Abs(root))` → root 本身是符号链接的那几行红,
  证明一个函数钉住了全部入口
- [x] 既有测试一字不改仍绿:`git diff --stat origin/main -- internal/clean/clean_test.go internal/collect/collect_test.go
  internal/collect/rootspelling_test.go internal/collect/hash_test.go internal/detect/rootspelling_test.go cmd/aguard/rootspelling_test.go
  cmd/aguard/collectroot_test.go cmd/aguard/main_test.go cmd/aguard/gate_e2e_test.go` 为空(`TestHashGolden`、P-009/P-010/P-012 的写法矩阵、
  不变量 #2 的边界测试都在里面)
- [x] `make verify` 绿;`go version` 不切换工具链

## 不做什么

- **不改 detect 的 `relPath`(P-012 未决 9)**:实测相对写法已与同一条经符号链接的绝对写法输出相同(「问题」末段),判据"等于绝对写法"
  成立;修它(绝对路径也解析目录)会改**经符号链接的绝对写法**和"`~/.claude` 本身是符号链接"的输出 —— SARIF `uri`、指纹、
  `.aguardignore` 的 glob 都跟着变 —— 超出本条。另开
- **不改单目标 `check` 的输出**:`check ./skills/plain` 仍回显敲进来的路径;`check .` 在一个 skill 目录里仍把 artifact 叫 `.`(绝对写法叫
  `plain`)—— 改名会改单目标的输出,而本条钉住它不变。另开
- 不放宽 `looksLikeRoot`:标记仍只有"目录名是 `.claude`"和"有 `plugins/installed_plugins.json`",只是对锚定后的路径判
  (`.claude/rules/pipeline.md` 讲了为什么)
- 不 `EvalSymlinks` root(P-010 未决 3、P-012 未决 2 的同一条理由);不改 `clean` 的 `withinDir`/`resolved`/`protected`/`quarantinable` 的判断
  逻辑、不改 `collect.withinDir`、`PluginInstalls` 的边界 —— 只改喂给它们的 root 和 home 在哪个坐标系里
- 不改 `hook install`/`uninstall`/`status`、`approvals`、`forget` 的路径回显:它们按敲进来的 root 读写的是同一个文件,输出里回显的路径
  随写法变,但答案不变(实测 `hook install --dry-run`、`hook status` 两种写法输出相同,`approvals` 只差回显的路径)
- 不改哈希定义(`internal/collect/hash.go` 不动)、不改 JSON schema、不改规则和分数;spec 不改(spec 没写 home 怎么从 root 取)
- 不改 `hack/`,不加依赖,`go.mod` 不动

## 不能说什么

- 不说"`--root` 怎么写结果都一样":工作目录经符号链接时路径留在符号链接的坐标系里(P-012 未决 5);经符号链接时部分证据缩成两段尾巴
  (不做什么第一条);单目标 `check` 回显敲进来的路径
- 不说以前的闸门"放过了恶意插件":能说的是相对写法和带尾斜杠/尾点的写法下,闸门**找不到**已装的插件 skill,按设计放行并出 `GATE-000`;
  注册进 settings.json 的 hook 命令不带 `--root`,日常加载用的是 `defaultRoot`(通常是绝对路径)
- **必须说**:`check .`(以及 `check <abs>/.claude/.`、`check ..`)在一个 `.claude` 里从"整树一个 `directory`"变成 root 布局 —— 和
  `check "$PWD"` 一样:root 顶层没人引用的目录**按名披露、不读**(`COV-000`),artifact 清单、分数构成、`hash` 打印的身份和 `approve`
  记下的哈希都跟着变。这是向绝对写法看齐,不是放宽路由
- **必须说**:相对写法下 `clean` 的输出、`check` 的 JSON、闸门理由里的路径和它给出的 `aguard check`/`aguard approve` 命令都变成锚定后的
  绝对路径
- 不说真机输出变了:真机默认 root 是绝对路径,本条不改它的任何输出(以实测为准)

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | clean、collect、cmd 三处写法矩阵,跑红 | `clean, collect, cmd: tests — a relative --root makes clean refuse its own trash, check . inside a config root reads one directory, and the gate and version lose the installed plugins, so one directory gets a different answer per spelling (P-019)` |
| 2 | 导出 `collect.AnchorRoot`,`CollectAll` 用它,`looksLikeRoot` 判锚定后的路径 | `collect: one exported root anchor, and check routes on the anchored path, so check . inside a config root takes the root layout (P-019)` |
| 3 | `clean` 在写路径入口锚定 | `clean: the root is anchored where every move, restore and baseline write starts, so a relative --root no longer refuses its own trash (P-019)` |
| 4 | `gateOptions`、`pluginVersionLine` 先锚定再取 home | `cmd: the gate and version take home from the anchored root, so a relative or slash-ended --root finds the installed plugins (P-019)` |
| 5 | detect 的 `anchorRoot`、`absRoot` 换成 `collect.AnchorRoot` | `detect: the engine and the content hashes anchor with collect.AnchorRoot instead of two private copies of it (P-019)` |
| 6 | `.claude/rules/pipeline.md` 改 P-012 那条防护点,`detect.md` 改函数名(净零行) | `rules: pipeline.md says every entry point that routes on the root or derives home from it anchors with collect.AnchorRoot (P-019)` |
| 7 | 本文件、索引 | `proposals: P-019 (P-019)` |

## 未决问题

1. **锚定函数放在哪、叫什么?**
   **建议**:`collect.AnchorRoot`,内容就是 `CollectAll` 现在内联的那几行(`filepath.Abs`,失败退回 `Clean`,不 `EvalSymlinks`)。collect 是
   P-012 定的锚点所在;clean、gate、detect、cmd 都已经 import collect,不新增 import 边。
   **已决(2026-10-09)**:按建议。
2. **clean 在哪一层锚定?**
   **建议**:包内写路径的入口 —— `quarantine`(`Apply`、`Resolve`、`ResolveInteractive` 共用的唯一移动路径)、`Undo`、`KeepBoth`、
   `QuarantineRefusal`,而不是 `main.go` 的 clean `RunE`。与 P-010(`Engine.Run`)、P-012(`CollectAll`)同一个约定:包的所有调用方一次覆盖。
   `withinDir` 的判断不改 —— 它从此只收到绝对 base;只改它而不锚定入口,决定是对了,输出里回显的路径仍随写法变。
   **已决(2026-10-09)**:按建议。
3. **`check` 的路由在哪锚定?**
   **建议**:`looksLikeRoot` 自己先锚定再判 —— "目录叫 `.claude`"是目录的属性,不是字符串的;单目标的三个分支仍用敲进来的 `path`,
   `check ./skill` 不变。`CollectAll(path)` 照旧自己锚定(幂等)。
   **已决(2026-10-09)**:按建议。
4. **`check .` 在 `.claude` 里改走 root 布局,读的东西变少(顶层没人引用的目录只披露不读),接受吗?**
   **建议**:接受,写进不能说什么。判据是"等于绝对写法",而 `check "$PWD"`、`check ../.claude` 今天就是 root 布局;`looksLikeRoot` 的注释
   和 `pipeline.md` 说了为什么 root 形状的树按 root 的规矩读是对的。反方向(把绝对写法改成整树读)会改所有 `check ~/.claude` 用户的输出。
   **已决(2026-10-09)**:按建议。
5. **闸门在哪锚定?**
   **建议**:`gateOptions` —— home 是在这里从 root 推出来的,`gate.Options` 的 `Root`/`Home` 由调用方给。`runHook` 读 approvals 用的
   `gate.ApprovalsPath(root)` 不改:相对路径和锚定后的路径指同一个文件。
   **已决(2026-10-09)**:按建议。
6. **detect 的 `anchorRoot`(P-010)和 `absRoot`(P-009)要不要在这里收成一个?**
   **建议**:收,单独一个提交(W5):两者函数体逐字相同,改成调用 `collect.AnchorRoot` 是机械替换;P-009 的 `hash` 写法测试、P-010 的
   detect/cmd 写法矩阵、P-012 的矩阵都钉着它,变异检查证明一个函数钉住全部入口。
   **已决(2026-10-09)**:按建议。
7. **P-012 未决 9(`relPath`)并进来吗?**
   **建议**:不并。实测它不是写法依赖(相对写法与同一条经符号链接的绝对写法输出相同);修法会改经符号链接的绝对写法和"`~/.claude`
   本身是符号链接"的输出,本条的反向断言正是"绝对写法不变"。作为后续单独提。
   **已决(2026-10-09)**:按建议。
8. **`check .` 在一个 skill 目录里把 artifact 叫 `.`,算不算本条?**
   **建议**:不算,另开。它是单目标的命名,改了会改 `check ./skill` 这一类输出;本条钉住单目标不变。
   **已决(2026-10-09)**:按建议。

## 完成

```
合入:PR #40(2026-10-09;sha 用 git log --grep P-019 找)
发布:待发
证据:TestClean_RootSpellingIsTheAbsoluteRun(internal/clean/rootspelling_test.go);W1 在本仓 main(fd28344)上红九行(.claude、.claude/.、.、./、../.claude、..、home/.claude、../home/.claude、经符号链接的 .claude,都在 Apply 预演就报 "refusing to use <写法>/.aguard-trash: it resolves to …/.aguard-trash, outside the scanned root")→ W3 后十二行全绿;<abs>、<abs>/、<abs>/. 三行 W1 时就绿
证据:反向断言 TestClean_RootSpellingKeepsTheRefusals(同文件)—— 十二行符号链接的 .aguard-trash 都以 "it is a symlink" 拒绝、root 外目录为空、skill 未动,工作目录在 rules/ 里写 . 仍以 inside a "rules" directory 拒绝;W1 时就绿,修后仍绿
证据:TestCollectTarget_RootSpellingRoutesLikeTheAbsoluteTarget(internal/collect/targetspelling_test.go);W1 红五行(<abs>/.、.、./、.claude/.、..:Root 为空、清单只有一个 directory)→ W2 后十三行全绿;反向断言 TestCollectTarget_SingleTargetsKeepTheirSpelling(同文件,skills/plain 与 ./skills/plain 仍是 Path 原样的单个 skill;不叫 .claude 的目录五种写法仍是一个 directory)W1 时就绿,修后仍绿
证据:TestCheck_RootSpellingInsideAConfigRootIsTheAbsoluteReport、TestHashCommand_RootSpellingPrintsTheAbsoluteHashes(cmd/aguard/rawroot_test.go);W1 各红五行(同上五种写法)→ W2 后全绿,check 的 JSON 与绝对目标逐字节相同,hash 的 stdout 相同
证据:TestGate_RootSpellingResolvesTheSamePlugins(同文件);W1 红十一行(九行相对写法 gateOptions 的 Root/Home 是相对的、myplug:hello 回 GATE-000;<abs>/、<abs>/. 两行 Home 等于 root、dplug:dhello 回 GATE-000)→ W4 后十二行全绿,三条回复与绝对写法逐字节相同、都是 ask 带 EXEC-001;fixture 换成短临时目录后,把 gate.go、version.go 临时退回 W3 再跑一次,仍红(两条测试共 20 个子测试)
证据:反向断言 同一测试 —— installPath 在 HOME 外的 outplug 在十二行都不被 PluginInstalls 解析、outplug:x 都回 GATE-000;绝对写法三条 ask 的字面断言 W1 时就绿
证据:TestPluginVersionLine_RootSpelling(同文件);W1 红九行(相对写法返回空串)→ W4 后十二行都是 "plugin aguard 0.18.0 matches this binary"
证据:变异(临时改、跑、还原,未提交):collect.AnchorRoot 只 Clean 不 Abs → collect 4 条(含 P-012 的三条)、detect 2 条(TestContentHash_SameConfigTwoMachines、TestRun_RootSpellingKeepsTheBoundary)、clean 1 条、cmd 7 条(按 RootSpelling|CITemplate|RelativeRootShaped 过滤跑;含 P-010、P-012 的写法测试)红;改成 EvalSymlinks(Abs(root)) → TestCollectAll_LinkedRootKeepsItsHome、TestImports_DoNotDuplicateCollectedFiles、TestRun_RootSpellingKeepsTheBoundary 红;每次还原后 git status 只剩当时未提交的改动
证据:二进制前后(main fd28344 vs 本分支,fixture 在 /private/tmp 下,HOME 指向 fixture):「问题」表的十三种写法修后在 clean --undo last --dry-run、clean --zombie --apply --dry-run、check --json、hash、version 第二行、两条闸门回复、clean --json 上与绝对写法逐字节相同(经符号链接那两行的 check/clean --json 与经符号链接的绝对写法相同);<abs> 与经符号链接的绝对写法这八种输出修前修后逐字节相同;fixture A 绝对写法 scan --json(去掉 scanned_at、tool_version)修前修后逐字节相同(4587 字节);单目标 check ./skills/plain、check skills/plain、skill 目录里的 check . 修前修后逐字节相同
证据:真机 ~/.claude(--inbox off):main 与本分支绝对写法都是 overall 69 / artifact 180 / 发现 806 / note 10,JSON(去掉 scanned_at、tool_version)逐字节相同(15724 行)
证据:不做什么 —— git diff --stat origin/main -- internal/collect/hash.go internal/collect/pathsafe.go internal/collect/plugins.go internal/collect/desktop.go internal/gate internal/detect/detect.go internal/report hack docs/spec docs/rules.md go.mod go.sum 为空;既有测试 git diff --stat origin/main -- internal/clean/clean_test.go internal/collect/collect_test.go internal/collect/rootspelling_test.go internal/collect/hash_test.go internal/detect/rootspelling_test.go cmd/aguard/rootspelling_test.go cmd/aguard/collectroot_test.go cmd/aguard/main_test.go cmd/aguard/gate_e2e_test.go 为空
证据:未决 7 实测(工作目录经符号链接,fixture 加一条内联 curl … | bash 的 hook 和 hooks/deep/x/pre.sh):main 上 --root .claude 与 --root <base>/via/.claude 的证据都是 .claude/settings.json、x/pre.sh,不经符号链接时两种写法都是 settings.json、hooks/deep/x/pre.sh —— 不是写法依赖,未改
证据:.claude/rules/pipeline.md 159 → 162 行,detect.md 仍 200 行;TestClaudeRulesAreScopedToExistingPaths 绿
证据:make verify: all gates passed;go version go1.23.5(无工具链切换),go.mod 第二行 go 1.23.5,module 行 github.com/basdotio/AgentGuard,无新依赖
```
