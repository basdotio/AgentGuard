---
paths:
  - "plugin/**"
  - ".claude-plugin/**"
---
<!-- SPDX-License-Identifier: MIT -->
## Claude Code 插件(`plugin/`)

面向用户的入口在 [README.md](../../README.md#use-it-from-inside-claude-code-plugin);这里只写不要怎么改。
布局:仓库根 `.claude-plugin/marketplace.json` + 子树 `plugin/`(`.claude-plugin/plugin.json`、
`skills/`、`commands/`)。

- **`plugin/` 里只有 skill 和 command,故意不含本仓库的任何文档** —— 这是全节最重要的一条。
  `CLAUDE.md`、`docs/rules.md`、`README.md` 把引擎能检出的攻击模式**全都原文引了一遍**,所以整棵
  仓库树扫出来是 **0/100 High**(3 条 `EXEC-001`、`FS-003`、`FS-001`、`EXFIL-003`+`OBF-004`……)。
  插件装出去落在 `~/.claude/plugins/cache/`,那里**正是 `aguard scan` 会读的地方** —— 把文档一起
  ship,等于让每个用户的体检报告把本工具自己列成高危。**已经踩过一次**:`plugin.json` 起初放在
  仓库根上,`aguard check .` 的路由从 `directory` 变成 `plugin`,装出去的 cache 就是整棵仓库树。
  回归判据是一条命令:

  ```bash
  ./bin/aguard check plugin --fail-on low     # 必须 exit 0(实测 100/100)
  ```

- **marketplace.json 的 `name` 必须等于公开仓库名(`AgentGuard`)。** 桌面版 Customize 里添加 marketplace 时,是按用户
  输入的 `owner/repo` 取仓库名当 marketplace 名记在账号里的;点 Update 时它拿这个名字让内置 CLI 刷新,而 CLI 是按
  marketplace.json 里声明的名字注册的。两个名字不一样,CLI 回 "not found. available marketplaces",桌面版翻译成
  `MARKETPLACE_ERROR:NOT_REGISTERED`,界面只显示 "Couldn't check for updates",插件永远停在装机时那个版本
  (2026-09-04 真机日志确认,marketplace 原来叫 `agentguard`)。所以 marketplace 名是 `AgentGuard`,改仓库名就要
  同时改这里。插件 ID 是 `aguard@AgentGuard` —— 为什么不是 `agentguard`,见下一条。
- **插件名不能和任何 marketplace 名只差大小写**(2026-10-01,[issues/022](../../issues/022-plugin-install-case-collision-macos.md))。
  Claude Code 先把插件暂存到 `cache/<plugin.json 的 name>`,再挪进 `cache/<marketplace>/<插件>/<版本>`,而"目标在暂存目录里"
  的判断是区分大小写的字符串比较。插件原来叫 `agentguard`,在默认的 macOS(不区分大小写)上和 `cache/AgentGuard` 是同一个
  目录,`claude plugin install` 报 `EINVAL` 并且**不登记** —— README 写的那条安装路径从来没走通过。marketplace 名被上一条钉死,
  能动的只有插件名,于是 0.17.0 改成 `aguard`。**也不能叫 `guard`**:旧 `basdotio/guard` marketplace 的缓存目录就叫这个,暂存
  那步会先把它 `rm -rf`。**`plugin.json` 的 name 必须等于 marketplace 条目名**:前者是 skill 命名空间,闸门拿它去查以后者为
  key 的 `PluginPaths`,两个一分开,每个带命名空间的 skill 都是 `GATE-000`(只改 `plugin.json` 那个"零迁移"方案就是死在这)。
  `TestPluginNameCannotCollideInTheInstallCache` 钉住这三条,在旧名字上验证过会红。旧名字的安装由 `aguard version` 认出来并给出
  换装命令(`legacyBundleName`):旧名字不会再收到更新,所以它永远报不出"插件比二进制新",不主动提示就等于静默停更。
- **`marketplace.json` 必须留在仓库根,`plugin.json` 必须不在** ——
  `/plugin marketplace add <repo>` 只认根上的 `.claude-plugin/marketplace.json`;而
  `collect.CollectTarget` 认的是 `.claude-plugin/plugin.json`,把它加回根上就会把整个仓库重新
  路由成一个 plugin artifact,也就是上一条那个坑。条目里的 `"source": "./plugin"` 是这两条约束
  唯一的交点,别改成 `"./"`。
- **skill 正文里描述注入要转述,不要引原文。** `agentguard-audit/SKILL.md` 起初引了一句字面的
  "ignore previous instructions" 当例子,当场吃到一条 `INJ-001` high —— 于是这个 skill 会拦住
  用户自己的 pre-commit、CI 和加载时闸门。**一个把自己判成恶意的安全工具没有任何说服力**,
  所以在这几份文件里写攻击形状一律用转述。
- **skill 的 `description` 是唯一的触发面,而它是 always-on 成本**(`claude plugin details` 实测
  ~981 token/会话,几乎全在三条 description 上)。`agentguard-audit` 的描述里那串规则 ID
  (`INJ-001`、`EXEC-001`、`COV-000`……)**是有意留的**:用户问"`INJ-001` 是什么意思"要能命中,
  删掉省下的几十个 token 换不回那次命中。
- **description 不得超过 1024 个字符,name 不得超过 64 个**——这是桌面版的硬限制,不是建议。app 里的限制表
  (`nameChars:64, descriptionChars:1024, skillsPerEntry:20`)校验每个 skill 的 frontmatter,超限的 skill **静默丢掉**,
  插件卡片上就少一条。audit skill 从 v0.4.0 到 v0.5.0 一直是 1159 字符,桌面版从来只列出另外两个,直到 2026-09-08 才
  有人注意到 "2 skills";CLI 没有这个限制,所以 `claude plugin validate` 和自扫都绿。`TestSkillFrontmatterFitsDesktopLimits`
  钉住三个数。往描述里加触发词之前先减,别只加。
- **跨目录引用只有两种合法形态**:同级 skill 的相对路径(`../agentguard-audit/references/install.md`
  ——三个 skill 共用那份安装说明),和指向仓库的**绝对 URL**。**不要写 `../../docs/rules.md` 这类
  指出 `plugin/` 之外的相对链接** —— 装出去之后 cache 里没有那半棵树,链接必断。
- **插件不带 MCP server,也不注册任何自己的 hook。** 一个"帮你审 hook"的插件顺手给你装一个 hook,
  正是它教用户提防的形状。`aguard hook install` 保持显式、且先 `--dry-run` 给人看。
- **发版时三处版本号一起改**:`plugin/.claude-plugin/plugin.json`、`.claude-plugin/marketplace.json` 的插件条目、git tag。
  marketplace 条目里的 `version` 不是可选的装饰:桌面版的更新检查读的是账号级名单里的 `availableVersion`,
  服务端从 marketplace.json 条目算,条目没写 version 就永远显示 "No changes since the last release"
  (2026-09-05 真机确认;CLI 那边读 plugin.json,两边不一致时 CLI 静默取 plugin.json)。
  `TestMarketplaceEntryVersionMatchesPlugin` 钉住两个文件相等,以及 marketplace 名等于 `cmd/aguard/version.go` 的
  `homeMarketplace`(`AgentGuard`)。skill/command 是纯文本,没有构建步骤,也**不进 canonical 哈希的排除名单**(它们不是
  生成物)。
- **`aguard version` 的升级提示按"这一份安装"给命令,不写死 marketplace**(2026-09-30)。原来是字面量 `agentguard@guard`:
  从本仓库装的(`@AgentGuard`)拿到的是一条找不到 marketplace 的命令;从旧分发仓库 `basdotio/guard` 装的(停在 0.9.0)拿到的是
  一条能成功、但永远追不上的命令。现在 `collect.PluginInstalls` 带出每个渠道自己记录的 marketplace(`installed_plugins.json` 的
  key、桌面版 manifest 的 `marketplaceName`)和是不是桌面版;`updateHint` 据此给 `claude plugin update`、桌面版 Customize,或
  "换到 `basdotio/AgentGuard`"三种之一。marketplace 名来自配置文件,而这一行原样打印、还会被 skill 转述给模型,所以**不是
  普通名字的一律不回显**(`plainMarketplaceName`),`TestUpdateHint` 里有一条带反引号和换行的用例钉着。**改仓库名或
  marketplace 名时,`homeMarketplace`/`homeMarketplaceRepo` 要一起改。**

