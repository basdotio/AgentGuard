<!-- SPDX-License-Identifier: MIT -->
# 012 — --root 用相对写法时,符号链接安装的 skill 整个不被收集,`--root .` 还漏掉 home 下的配置;CI 模板的 `.mcp.json` 只是碰巧被读到

- **来源**:collect 只 `Clean` 不 `Abs`:相对写法下以符号链接安装的 skill 被整个丢弃,`--root .` 把 home 取错,用户级 MCP、
  home 的 `CLAUDE.md` 和桌面版仓库都漏掉;发布的 CI 模板 `scan --root .` 读到仓库顶层 `.mcp.json` 只是因为 home 碰巧等于 root,
  绝对写法从不读它。移植自旧仓 agent-guard 的 P-055(私有仓)
- **依赖**:无(与 P-010 独立可合,见未决问题)
- **分支**:`p/012-collect-anchors-root`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

`collect.CollectAll` 入口只 `filepath.Clean(root)`,不 `Abs`,然后 `home := filepath.Dir(root)`
(`internal/collect/collect.go:216-217`)。`home` 管着两件事:用户级/项目级配置去哪找(`<home>/.claude.json`、
`<home>/.mcp.json`、`<home>/CLAUDE.md`、桌面版仓库与会话缓存),以及符号链接安装的 skill 解析后必须落在哪
(不变量 #2:`withinDir(home, real)`,越界出 `SCOPE-001`)。`Dir` 只看字符串,于是:

- **相对写法**(`.claude`、`home/.claude`、`../home/.claude`):home 是相对路径。以绝对路径为目标的 skill 符号链接
  (`skills/x → ~/.agents/skills/x`,最常见的安装方式)解析出绝对路径,`withinDir` 里 `filepath.Rel(相对, 绝对)` 报错,
  按出错即拒返回 false —— **这个 skill 整个不被收集**,还得到一条说错原因的 `SCOPE-001`
  "Skill dir symlink points outside HOME"(它明明在 HOME 里)。以相对路径为目标的符号链接碰巧能活。
- **`--root .`、`--root ./`**(`cd ~/.claude` 之后最自然的写法):`Dir(".")` 还是 `.`,home == root。上面那些
  home 级位置全在 root **里面**找;skill 的边界缩成 root,任何指出 root 的 skill 符号链接都被拒。报告的
  "Locations" 一节(`cmd/aguard/main.go` 的 `scanLocations`,同样 `Dir(Clean(root))`)还把
  "User MCP config" 写成 `.claude.json`、状态 `absent` —— 告诉读者它看过了、没有。

实测(`main` `dec64ca`,v0.18.0 构建的二进制,`HOME` 指向 fixture,`--inbox off`;fixture:root 内一个普通 skill、
一个以绝对路径链到 HOME 内的 skill、一个以相对路径链到 HOME 内的 skill、一个链到 HOME 外的 skill,
`<home>/.claude.json` 里一条 `sh -c "curl … | bash"` 的 MCP server、`<home>/.mcp.json` 里一条普通 server,`<home>/CLAUDE.md`;
每个 skill 脚本都是 `curl … | bash`):

| `--root` 的写法 | 工作目录 | artifact | 风险发现 | `SCOPE-001` |
|---|---|---|---|---|
| `<home>/.claude`、`<home>/.claude/`、`<home>/.claude/.` | 任意 | 6 | 4 × `EXEC-001` | 1(HOME 外那个,正确) |
| `.claude`、`.claude/` | `<home>` | 5(少绝对链接的 skill) | 3 | **2**(多一条误报) |
| `home/.claude` | `<home>` 的父目录 | 5 | 3 | **2** |
| `../home/.claude` | `<home>` 的兄弟目录 | 5 | 3 | **2** |
| `.claude` | 经符号链接到达的 `<home>` | 5 | 3 | **2** |
| `.`、`./` | `<home>/.claude` | **1**(只剩普通 skill) | **1** | **3**(两条误报) |

`.claude.json` 里那条 `curl | bash` 的 server 在 `--root .` 下整个从清单里消失(绝对写法下它带一条 high 的 `EXEC-001`)。
总分这几行恰好都是 69(一条 high 封顶),但清单和发现不同 —— 闸门放行与否取决于被丢的东西里有没有比剩下的更重的。

**反方向也不一致**:home == root 时,`<home>/.mcp.json` 读的就是 **root 里的** `.mcp.json`。仓库本身当 root
(`hack/github-action.yml` 发给用户的 CI 模板就是 `aguard scan --root . --fail-on high`)时,`.` 写法读到仓库顶层的 `.mcp.json`,
绝对写法读不到 —— 同一个二进制实测,仓库顶层 `.mcp.json` 里一条 `curl | bash` 的 server:`--root .` overall 69、一条 `EXEC-001`、
`--fail-on high` 退出 1;`--root "$PWD"` overall 100、零发现、零 note、退出 0(`unowned.go` 的 `rootOwned` 把 `.mcp.json`
记成"已有人读",所以连披露都没有)。`--root .` 下仓库顶层的 `CLAUDE.md` 还被收两遍(`CLAUDE.md` 与 `CLAUDE.md (project)`
同一个文件)。

真机 `~/.claude`(同一个二进制,`--inbox off`):绝对写法 overall 69 / artifact 175 / 发现 806 / note 10;
`cd ~` 后 `--root .claude` 为 69 / 137 / 643 / 15;`cd ~/.claude` 后 `--root .` 为 69 / 83 / 574 / 15。

后果:同一个环境,清单取决于 root 怎么敲;被丢的是**合法安装方式**的 skill 和用户级 MCP,两者都是这个工具要审的头号对象,
丢的时候还附一条把原因说错的 note。

## 初步方向

在 `CollectAll` 入口一次锚定 root:`filepath.Abs`(自带 `Clean`),**不** `EvalSymlinks`(解析 root 自己的符号链接会把 home 挪走);
home 从锚定后的 root 取。`scanEnv` 把同一个锚定后的 root 交给 `analyze`,让 detect、Locations、基线路径和 collect 在同一个
坐标系里。相对写法下 JSON 里的 `path` 会变成绝对路径;绝对写法的输出必须逐字节不变,树哈希不变(哈希的是内容,不是 root 路径)。
不碰 `internal/detect`(那半是 P-010)。

## 完成的判据

fixture 都在 `t.TempDir()` 现搭,临时目录先 `EvalSymlinks`(macOS 的 `/var → /private/var`)。"安装形状"那份:root 是
`<base>/home/.claude`;`skills/plain`(普通 skill,`scripts/run.sh` 里 `curl … | bash`)、`skills/linked → <home>/agents-store/linked`
(绝对目标,HOME 内)、`skills/rel-linked → ../../agents-store/rel`(相对目标,HOME 内)、`skills/escaper → <base>/outside/evil`
(HOME 外);`<home>/.claude.json` 一条 `sh -c "curl … | bash"` 的 MCP server;`<home>/.mcp.json` 一条 server;`<home>/CLAUDE.md`。
写法:`<abs>`、`<abs>/`、`<abs>/.`、`.claude`、`.claude/`(工作目录 `<home>`)、`.`、`./`(工作目录 root)、`home/.claude`
(工作目录 `<base>`)、`../home/.claude`(工作目录 `<base>/sibling`),外加"工作目录经符号链接"(`<base>/via → home`,
工作目录 `<base>/via`,`PWD` 指向链接,写 `.claude`)。工作目录用 `os.Chdir` 并在 `t.Cleanup` 里还原(Go 1.23 没有 `t.Chdir`;
这两个包里没有 `t.Parallel`)。"仓库当 root"那份:root 是 `<base>/work/repo`(`skills/demo`、顶层 `.mcp.json` 里一条
`sh -c "curl … | bash"` 的 server、顶层 `.claude.json` 里一条 server),它的 home `<base>/work` 也有自己的 `.mcp.json`、`.claude.json`。

- [ ] `TestCollectAll_RootSpellingKeepsTheInventory`(`internal/collect/rootspelling_test.go`,新):"安装形状"每行 `CollectAll` 的
  (kind, name, hash, path) 清单、`Env`、notes 的 (rule, 证据文件)都等于绝对写法;"经符号链接"那行路径在符号链接的坐标系里,
  只比 kind/name/hash、`Env` 和 note 的规则。预期 W1 红七行:`.claude`、`.claude/`、`home/.claude`、`../home/.claude`、
  经符号链接那行少 `linked`、多一条 `SCOPE-001`;`.`、`./` 两行只剩 `plain`(「问题」里的二进制实测与此一致)。
  `<abs>/`、`<abs>/.` 两行 W1 时就绿(`CollectAll` 已经 `Clean`)
- [ ] 反向断言(同一测试):每行 `escaper` 都被拒 —— 有一条证据指向 `skills/escaper` 的 `SCOPE-001`,没有任何 artifact 的
  path 落在 `<base>/outside` 下。"被拒"W1 时就绿,修后仍绿;"`SCOPE-001` 恰好一条"W1 时在同样那七行红(多出来的是误报)
- [ ] 反向断言(同一测试):三个 skill 的树哈希每行相同,且等于直接对解析后的目录算的 `TreeHash` —— 哈希的是内容,不是
  root 怎么写;`TestHashGolden` 不改一字仍绿
- [ ] `TestScan_RootSpellingIsTheAbsoluteReport`(`cmd/aguard/collectroot_test.go`,新):"安装形状"每种写法走 `scanEnv`,
  JSON(去掉 `scanned_at`)与绝对写法**逐字节相同** —— 包括 `root`、`locations`、artifact 的 `path`、全部发现与证据
  ("经符号链接"那行只比去掉路径字段的视图)。预期 W1 红九行:七行清单就不同(同上),`<abs>/`、`<abs>/.` 两行只差
  `root` 回显
- [ ] `TestCollectAll_RootLevelMCPConfigIsRead`(`internal/collect/rootspelling_test.go`,新):"仓库当 root"在 `<abs>` 与 `.`
  两种写法下都收到顶层 `.mcp.json`、`.claude.json` 里的 server **和** home 那两个文件里的 server,两行清单相同。
  预期 W1 红两行:绝对写法缺 root 顶层的两个(从来不读),`.` 缺 home 的两个(home == root)。反向断言(同一测试):
  root 顶层的 `.mcp.json` 是指向 `<home>/.mcp.json` 的符号链接时,那个文件的 server 只出现一次(同一个文件不读两遍)
- [ ] `TestScan_CITemplateShapeBlocksUnderEverySpelling`(`cmd/aguard/collectroot_test.go`,新):CI 模板的形状
  (`hack/github-action.yml` 在本仓 `main` 上仍是 `aguard scan --root . --fail-on high --sarif aguard.sarif`):仓库顶层
  `.mcp.json` 里一条 `curl | bash` 的 server,`--root .` 与 `--root "$PWD"` **都**带 `EXEC-001`、`--fail-on high` **都**拦
  (`failGate(out, "high", "", false)` 返回退出 1)。预期 W1 红在 `"$PWD"` 一行(修前 overall 100、零发现、零 note);
  `.` 一行 W1 时就绿,修后仍绿 —— 原本拦得住的仍然拦得住
- [ ] 反向断言(实现时补,W6):`TestCollectAll_LinkedRootKeepsItsHome`(`internal/collect/rootspelling_test.go`):root 本身是符号链接
  (`~/.claude → ~/dotfiles/claude`)时,`<abs>`、`<abs>/`、`.`(工作目录在链接里)、`.claude` 四行的清单等于同样内容的普通 root,
  `Result.Root` 是链接本身而不是它指向的地方 —— 钉住"只 `Abs` 不 `EvalSymlinks`";`TestCheck_RelativeRootShapedTargetIsTheAbsoluteReport`
  (`cmd/aguard/collectroot_test.go`):`check .claude`、`check ./.claude`、`check ../home/.claude` 的 JSON 与绝对目标逐字节相同 ——
  钉住 `checkTarget` 用 `Result.Root`
- [ ] 反向断言:root 顶层**没有**这两个文件时,绝对写法的 `scan --json` 修前修后逐字节相同(去掉 `scanned_at`、`tool_version`):
  "安装形状" fixture 上(二进制前后),以及真机 `scan --root ~/.claude`(真机 `~/.claude` 下两个文件都不存在,已确认);
  review 包只贴 overall、artifact/发现/note 数
- [ ] 反向断言:不变量 #2 的既有测试一字不改仍绿 —— `TestEscapingSymlinkSkillNoted`、`TestCrossRootSymlinkIgnored`、
  `TestInstalledSymlinkSkillFound`、`TestWithinRoot`、`TestCollect_UnresolvableSkillEntryIsDisclosed`、
  `TestCollect_DanglingSkillSymlinkIsNotReportedAsAGap`;`git diff --stat origin/main -- internal/collect/collect_test.go
  internal/collect/hash_test.go internal/collect/pathsafe.go internal/collect/hash.go internal/detect` 为空
- [ ] `make verify` 绿;`go version` 不切换工具链

## 不做什么

- **不碰 `internal/detect`**:hook 与授权脚本按 home 跟进是 P-010。`scanEnv` 把锚定后的 root 交给 `analyze`(见未决 1),
  于是 `scan`/`clean` 这条路上 detect 也拿到绝对 root —— 这是"collect 与 detect 同一坐标系"的必然结果,不是在这里修 detect;
  `check` 单目标、闸门、`clean` 的恢复预览仍靠 P-010
- 不改 `withinDir`、install-symlink 守卫、`collectNestedSkills`、`collectPlugins` 的边界逻辑:不变量 #2 的"先解析再判断、出错即拒"
  一行不动,只改喂给它们的 root 和 home 在哪个坐标系里
- 不 `EvalSymlinks` root(未决 2)
- **同根因的另外三处不在这里修,人定(2026-10-09)合成一条 proposal,本条合入后由 lead 另开**:
  (1) `internal/clean`:本仓 `main` 上实测,fixture 的 root 下有 `.aguard-trash/` 时 `cd <root> && aguard clean --root . --undo last --dry-run`
  报 "refusing to use .aguard-trash: it resolves to …/.aguard-trash, outside the scanned root"、退出 2(`clean.withinDir` 对相对 root
  只 `EvalSymlinks` 不 `Abs`),同一 root 的绝对写法正常(`Nothing to undo`、退出 0);`clean` 命令照旧把 `--root` 原样交给 `clean.*`。
  (2) `CollectTarget` 的路由:`looksLikeRoot` 按敲进来的字符串看目录名 —— 实测工作目录是一个没有 `plugins/installed_plugins.json`
  的 `.claude` 时,`check .` 按"其他目录"整树读成 1 个 `directory` artifact,`check ../.claude` 走 root 布局出 5 个 artifact。
  (3) `cmd/aguard/gate.go` 的 `Home: filepath.Dir(root)` 和 `cmd/aguard/version.go` 的 `pluginVersionLine` 从原样 root 取 home
- `scanLocations` 只多出 root 顶层那两个文件的行(存在时才有,W4);取 home 的那两行不改 —— 它从 `analyze` 收到的 root 已经锚定
- 不改 `unowned.go` 的 `rootOwned`:它说 `.mcp.json`、`.claude.json` 已有人读,W4 之后这句话成真
- 不改哈希定义、不改 JSON schema(字段不增不减)、spec 不改(spec 没写 home 怎么从 root 取;本条修的是实现偏离
  "home 是 root 的父目录"这一既有说法,见 `CollectAll` 的注释)
- 不改 `hack/`(CI 模板照旧 `--root .`)
- 不加依赖,`go.mod` 不动

## 不能说什么

- 不说"`--root` 怎么写结果都一样":`check` 的路由仍看字符串(不做什么里那条合并 proposal 的 (2));工作目录经符号链接时
  artifact 的 `path` 留在符号链接的坐标系里(清单、哈希、发现相同,路径字符串不同);P-010 合入前,`check`/闸门里 hook 脚本
  跟进仍随写法变
- 不说以前的扫描"漏掉了恶意 skill":能说的是相对写法丢了以绝对路径符号链接安装的 skill、`.`/`./` 还丢了 home 级配置和
  桌面版;读了会出什么取决于内容
- **必须说**:相对写法和带尾斜杠的写法,JSON/SARIF/HTML/markdown 里的 `root`、artifact 的 `path`、collect note 证据里的路径
  都变成锚定后的绝对路径 —— 有意的输出变化,只影响这些写法的用户。SARIF 结果的 `uri` 取自证据的相对路径,对 root 内文件
  不变(指纹不变),`aguard/root` 属性随 `root` 变
- **必须说**:root 顶层有 `.mcp.json` 或 `.claude.json` 的环境,**绝对写法**的输出也会变 —— 多出这两个文件里的 MCP server
  (可能连带发现,用绝对路径跑 CI 的仓库可能因此新红)。这是绝对写法自己的假阴性被修好,不是回归:`rootOwned` 一直声称读了它们,
  `--root .` 一直在读。root 顶层没有这两个文件时绝对写法逐字节不变;真机 `~/.claude` 下两者都不存在
- **必须说**:工作目录经符号链接、`--root` 用相对写法时,collect 没解析过的 root 内文件(如 `settings.json`)的证据路径从完整
  相对路径变成两段尾巴(`settings.json` → `.claude/settings.json`)—— 与同一条经符号链接的**绝对**写法今天的输出相同,
  skill 的证据不变(未决 9)
- 不说真机分数变了:真机默认 root 是绝对路径、顶层没有那两个文件,本条不改它的任何输出

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 两个包的写法矩阵和"仓库当 root",跑红 | `collect, cmd: tests — a relative --root drops a symlink-installed skill, --root . loses the user-level MCP config, and an absolute root never reads its own .mcp.json, so no two spellings report the same inventory (P-012)` |
| 2 | `CollectAll` 入口锚定 root,`Result.Root` 交出锚定后的 root | `collect: CollectAll anchors the root once, so a relative --root keeps symlink-installed skills and finds the user-level config (P-012)` |
| 3 | `scanEnv`、`checkTarget` 用 `Result.Root` 调 `analyze` | `cmd: scan and check analyse with the root collect anchored, so evidence, locations and the baseline share collect's frame (P-012)` |
| 4 | root 顶层的 `.mcp.json`、`.claude.json` 照读(同一个文件不读两遍),`scanLocations` 列出它们 | `collect, cmd: a .mcp.json or .claude.json at the top of the root is read under every spelling, so the CI template's --root . keeps blocking a poisoned repository config (P-012)` |
| 5 | `.claude/rules/pipeline.md` 补一条防护点(200 行上限,`TestClaudeRulesAreScopedToExistingPaths` 管着) | `rules: pipeline.md says the root is anchored once in CollectAll and callers analyse with Result.Root (P-012)` |
| 6 | 两条反向测试:root 本身是符号链接;`check` 相对的 root 布局目标 | `collect, cmd: tests — a root that is itself a symlink keeps its home, and check of a relative root-shaped target reports what the absolute one does, so resolving the root or dropping Result.Root turns them red (P-012)` |
| 7 | 本文件、索引 | `proposals: P-012 (P-012)` |

## 未决问题

1. **锚定点放在哪?**
   **建议**:`CollectAll` 入口(`filepath.Abs`),并经新字段 `collect.Result.Root` 交出;`scanEnv` 和 `checkTarget` 用它调
   `analyze`(`check` 只有走 root 布局的目标才有 `Root`,单 skill/目录/文件的目标原样,`check ./skill` 的输出不变)。
   只有一个锚定点:另一种做法是 main 里再 `Abs` 一次,那是两份同义代码靠注释同步。不让 detect 拿到原样 root 是必须的 ——
   collect 交出绝对路径而 detect 拿相对 root 时,`relPath` 关联不了两个坐标系,证据退化成两段尾巴(CI 模板的 SARIF `uri`
   和指纹就跟着变了)。`main.go` 改三处(`scanEnv`、`checkTarget`、W4 在 `scanLocations` 加的几行)。与 P-005(在 `scanEnv`
   的 `return` 前加了几行)会有一处文本冲突,解法是保留两边;与 P-010 不冲突(它不碰 `main.go`)。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
2. **`Abs` 还是 `EvalSymlinks`?**
   **建议**:只 `Abs`(自带 `Clean`)。解析会把 `~/.claude → ~/dotfiles/claude` 的 home 挪到 `~/dotfiles`(P-010 未决 3 的同一条理由);
   符号链接仍只在 `withinDir` 检查时解析。`Abs` 失败(工作目录已被删)退回 `Clean`,与今天相同。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
3. **相对写法下输出里的路径变成绝对路径,接受吗?**
   **建议**:接受,写进不能说什么。"报告不随写法变"正是判据;`.aguardignore` 按证据的相对 `file` 做 glob,root 内文件的证据不变,
   已有基线不受影响。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
4. **和 P-010 的关系?**
   **建议**:不动 detect,两条谁先合都行。P-010 的 `anchorRoot` 与 P-009 的 `absRoot` 都在 detect 包,本条在 collect 包不另起同名函数
   (只用 `filepath.Abs`),不会撞出重复定义;cmd 测试文件和辅助函数名避开 P-010 的 `cmd/aguard/rootspelling_test.go`
   (`spellingTempDir`、`withWorkingDir` 等),本条用 `collectroot_test.go` 和 `anchored*`。两条都合入后,`Engine.Run` 收到的已是
   绝对 root,`anchorRoot` 幂等。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
5. **测试矩阵要不要包括"工作目录经符号链接"?**
   **建议**:要,但只比清单、哈希和 note 规则:`Abs` 用的是 `$PWD`,路径留在符号链接的坐标系里,这是 `Abs` 的定义,不是缺陷;
   边界由 `withinDir` 解析后判,所以这一行 `linked` 照收、`escaper` 照拒。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
6. **CI 模板的 `--root .` 会因此失去仓库顶层的 `.mcp.json`。**
   home == root 时,`<home>/.mcp.json` 读的正是 root 顶层的 `.mcp.json`;绝对写法从来不读它,而 `unowned.go` 的 `rootOwned`
   把 `.mcp.json`、`.claude.json` 记成"已有人读",所以绝对写法下连 note 都没有(已经是一个违反不变量 #5 的静默缺口)。
   `hack/github-action.yml` 发给用户的 CI 模板正是 `aguard scan --root . --fail-on high`(仓库本身当 root)。本仓 `main` 实测
   (仓库顶层 `.mcp.json` 里一条 `sh -c "curl … | bash"` 的 server):`--root .` overall 69、`EXEC-001`、`--fail-on high` 退出 1;
   `--root "$PWD"` overall 100、零发现、零 note、退出 0。**只做锚定,`--root .` 就变成后者:CI 模板对这种仓库从拦变成静默放行。**
   - **A(建议)**:本条一并读 root 顶层的 `.mcp.json` 和 `.claude.json`(存在时;与 home 那两个是同一个文件时不读两遍),
     按 server 出 artifact,与 home 级的同名、不同 `path`(既有先例,见 `mcpServersFrom` 的注释)。`rootOwned` 的说法从此成真。
     代价:绝对写法的输出会变 —— 只在 root 顶层真有这两个文件时;真机 `~/.claude` 下两者都不存在。
   - **B**:只锚定;root 顶层这两个文件另开 proposal。合入本条到那条合入之间,CI 模板对仓库顶层 `.mcp.json` 是静默的绿。
   - **C**:B,再把 `.mcp.json`、`.claude.json` 从 `rootOwned` 拿掉,让它们在 unowned 的 `COV-000` 里被点名"没读"。闸门仍放行,
     但至少不静默。
   **建议**:A。
   **已决(2026-10-09,人)**:A。理由是上面的实测:CI 模板的 `--root .` 今天拦得住仓库顶层 `.mcp.json` 里的 `curl | bash`,
   只做锚定会让它静默放行。绝对写法的输出因此在 root 顶层有这两个文件时会变,写进不能说什么:那是假阴性被修好,不是回归。
7. **`check .` 的路由也随写法变,要不要一起修?**
   **建议**:不,另开(见不做什么):它改的是 `check` 的 artifact 形状(整树一个 directory → root 布局多个 artifact),
   要单独做前后对比;本条的判据只看 `scan --root` 和 `CollectAll`。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用);人另定它与 `clean`、`gate.go`/`version.go` 两处合成一条 proposal,本条合入后另开。
8. **与 P-010 都合入后,P-010 的 `TestScan_RootSpellingDoesNotChangeTheResult` 会红,谁来改?**
   两种写法的报告逐字节相同,红的原因在 P-010 测试的 `spellingView`:它按**敲进来的** root(`filepath.Clean(root)`)去掉 collect note
   证据的前缀;本条之后那些路径是锚定后的绝对路径,绝对写法那份去得掉前缀,相对写法那份去不掉。改成按报告自己的 root 去前缀
   (`filepath.Clean(out.Root)`)一行就绿;这一行在 P-010 自己的分支上行为不变(那里 `out.Root` 就是敲进来的 root)。
   **建议**:后合的那个 PR 在 rebase 时带上这一行:P-010 先合,本条 rebase 时改;本条先合,P-010 rebase 时改。PR 描述里写明。
   本仓的组合实测见「完成」。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
9. **工作目录经符号链接、`--root` 用相对写法时,root 内一部分文件的证据路径变短,算不算越界?**
   collect 的路径在 `$PWD` 的坐标系里(未决 5),detect 的 `relPath` 解析 root、不解析绝对文件路径,于是 collect 没解析过的 root 内文件
   (`settings.json`、三层以上的 `commands/ns/foo.md` 这类)退化成两段尾巴:`settings.json` 上的证据从 `settings.json`(今天的相对写法)
   变成 `.claude/settings.json`。这正是今天用**同一条经符号链接的绝对路径**写 `--root` 时的输出,所以判据"等于绝对写法"成立;
   但对这一小类用户是证据路径的倒退(`.aguardignore` 里按 `settings.json` 写的 glob 会失配,方向是多报不是少报)。skill 的证据不受影响
   (collect 给的是解析后的路径)。修法在 detect 的 `relPath`(绝对路径也解析目录),会改经符号链接的绝对写法的输出,不在本条范围。
   **建议**:接受,写进不能说什么;`relPath` 这一处交给 lead 决定是否并入那条合并 proposal。本仓实测见「完成」。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
