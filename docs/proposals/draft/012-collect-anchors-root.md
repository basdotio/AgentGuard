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
