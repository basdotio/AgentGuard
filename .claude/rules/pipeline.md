---
paths:
  - "cmd/aguard/**"
  - "internal/**"
---
<!-- SPDX-License-Identifier: MIT -->
## 流水线

[cmd/aguard/main.go](../../cmd/aguard/main.go) 里的 `analyze()` 是 `scan`/`check`/`clean`
的唯一编排入口。**各阶段的先后顺序是有意为之**,该函数的注释解释了原因:

```
collect  → detect → permcheck → reputation → ignore/baseline → judge(可选) → hygiene → score → report
(§4)       (§5.1)   (§7)         (D11)        (.aguardignore)   (§5.2)         (§6)      (§5.3)  (§9)
```

- **改了确定性发现就加 epoch**(P-002,spec §5.1):在 `builtinRules()` 之外改变了某个输入产出哪些确定性发现,或它们的 ID/维度/严重度/advisory,同一个提交里把 `detect.rulesEpoch` 加一并跑 `make docs` —— 没有任何东西替你拦;判官(`internal/judge`)不在 `rules_version` 里,改它不加。
- **permission 的两半分别由两个包管**:`permcheck` 只看 allow 条目的**文本形状**(它从不开文件);
  `detect.permissionUnits` 负责**跟进 allow 项引用的本地脚本** —— `Bash(./scripts/deploy.sh *)`
  这条授权有多危险,取决于 `deploy.sh` 干什么,那是"把文件读进来跑规则",不是语义判断,所以是静态
  不是 AI(计划里曾把它排给 AI,是排错了栏)。只跟 `allow` 不跟 `deny`;复用 `scriptRefs` 切词,
  **不要再写第二个授权语法解析器**。边界与 hook 完全同一套(`resolveHookScript` + `inBoundary`),
  跟不动一律出 `COV-000`。
- **permcheck 与 detect 并列,不是它的子集** —— allow 条目是配置语义不是文本模式,所以走
  [internal/permcheck](../../internal/permcheck):除了通配执行/内联 secret/宽泛路径,还比对
  [escape.go](../../internal/permcheck/escape.go) 的**可逃逸二进制表**(`PERM-006`)——
  `Bash(git *)` 能经 `git -c core.pager=` 拿到任意执行,与 `Bash(bash -c *)` 同级。精度分两层:
  - **授权必须是开放式的**(参数含通配)。全指定的 `Bash(git status)`、`Bash(make test)` 永远不报 ——
    没有开放段就没地方塞手柄,而这也正是给用户的修法。
  - **"钉死子命令能不能挡住手柄"是逐个二进制的事实,必须实测,不能推**(`positionFree`)。
    原来那条通则是从 git 推广出来的,而 git 恰好是它成立的那一个:`git log -c k=v` 不是覆盖配置,
    是 git 把 `k=v` 当成 revision 然后报 `ambiguous argument`。**选项解析会重排的工具正好相反** ——
    实测 `make -f`、`find -exec`、`rsync -e`、`ssh -o ProxyCommand`、`vim -c ':!'` 写在钉死参数
    **之后照样生效**,所以 `Bash(make test *)` 跟 `Bash(make *)` 一样敞开(真机上就漏报着这一条)。
    反过来 `scp`(后置 `-o` 无效)、`sed`(`-e` 被当文件名)、`awk`(BSD awk 拒收后置选项)、以及
    docker/kubectl/npm 这类**子命令即手柄**的,钉死确实挡得住,现状是对的。
  - **实测方法本身是防护点**:payload 必须**写标记文件**,不能 echo 到 stdout。stdout 探针在两个
    方向上都会骗人 —— 标记回显在工具自己的报错里(`sed`)或从一个随后失败的命令泄漏出来(`tar`)
    会读成假 ✅;而手柄的 stdout 是协议流不是终端时(`ssh`/`rsync` 的 ProxyCommand)会读成假 ❌。
    我第一版就靠 stdout 判反了三条。**加表项之前照这个方法重测一遍。**
  - 表里 28 条,19 条带 `lever`,其中 6 条标了 `positionFree`(5 条实测 + `nvim` 按 vim 推断,
    注释里标着"未单独实测")。`TestPositionFreeIsMeasuredNotGuessed` 把每条的观测结论钉住了。
  - **还没处理的一层**:`make test`、`npm test` 这类**根本不需要选项手柄** —— 子命令的含义由工作
    目录里的 Makefile / package.json 定义,而那是 agent 自己选的目录。实测确认可达任意执行,
    记在 ROADMAP 的已知限制里,**故意没有做成规则**:那会把 `Bash(npm test *)` 这种极常见的写法
    一律报成 medium,而"被忽略的告警等于没有告警"是本仓库自己的判据。
- **`llm preview` 挂在 `runJudge` 那一点**(`scanOpts.preview`,P-027):`analyze()` 在调用判官的地方把同一份 `arts` 交给
  `previewSink.record`,所以它列出的是信誉和基线之后、hygiene 之前判官会拿到的东西。挪到别处它就列出一份判官收不到的计划;
  预览的 opts **永远不带 `llm: true`**——那会真去连端点,零表里 `llm preview` 那两行会红。
- **reputation 在 ignore 之前** —— 先用内嵌白名单压掉可信工具自身的噪声。
- **judge 在抑制之后** —— 这样即使是 known-good 的 artifact,旁注仍能浮出来。
- **抑制在评分之前** —— 基线会改变分数,所以下面那条"必须留 note"的规则是硬要求。
- **`check` 的入口是布局路由,不是 `scan`** —— `collect.CollectTarget` 按"从具体到宽泛"依次判定:
  单文件 → `SKILL.md` → plugin manifest(`.claude-plugin/plugin.json`)→ 像 root → 其他目录。
  最后那条会把整棵树当成一个 artifact 读掉。**不要给 `looksLikeRoot` 加宽松的标记**:路由到
  `CollectAll` 会把可读范围收窄到已知子布局,任何在普通目录上也会命中的标记(`skills/`、裸
  `settings.json`)都会重新打开"目录里散落的恶意脚本一个都不扫"这个假阴性。**`settings.json` 的内容也不算标记**
  (W-003,2026-09-15):原来"内容里带 Claude 独有的 key 才算 root"把内容当证据,而内容是目标作者写的——29 字节的
  `{"permissions":{"allow":[]}}` 就让两棵字节相同的 payload 树一棵 26 分一棵 100 分。现在只剩结构标记:目录名 `.claude`,
  或有 `plugins/installed_plugins.json`。自定义名字的 config root 是 `scan --root` 的活,不是 `check` 的。同理,**`check` 的目标
  读不出来必须报错(退出码 2)**,不能返回空结果 —— 空结果会渲染成 100/100 + 退出码 0。
- **`scan`/`clean` 的 root 同样先校验再采集**(`collect.ValidateRoot`,在 `main.scanEnv` 里调用)。
  各 collector 把 ENOENT 当成"这个布局不存在"是对的,但对 root 本身就是灾难:一个打错的 `--root`
  让它们**同时**全都"不存在",于是 `scan --root ~/.clade` 输出 100/100 + "✅ No risk findings" +
  退出码 0 —— 跟 `check <typo>` 当年是同一个 bug 的另一半。root 不存在/不是目录 → 退出码 2;root
  存在但一个 artifact 都没采到 → 一条 `COV-000` 说明"清单是空的,不是干净的"(不变量 #5)。
  **`CollectAll` 自己分不出"root 不存在"和"root 是空的"**,所以校验必须留在知道那是不是笔误的调用方。
- **root 只在 `CollectAll` 入口锚定一次**(`collect.AnchorRoot`:`filepath.Abs`,**不** `EvalSymlinks`),home 从锚定后的 root 取,`collect.Result.Root`
  把它交出去,`scanEnv`/`checkTarget` 用它调 `analyze`。**别的入口要从 root 取 home、按名字判 root、或拿它和解析后的路径比,一律先过同一个
  `collect.AnchorRoot`,不另写一份**:`looksLikeRoot`(`check .` 在 `.claude` 里曾按"其他目录"整树读)、`clean` 的写路径入口(相对 root 曾把
  自己的 `.aguard-trash` 判成越界)、`gateOptions`/`pluginVersionLine`(相对 home 曾丢掉每个已装插件,闸门不审就放行)、detect 的 `anchorRoot`
  都是它(P-019,`rawroot_test.go` 等四个包的写法矩阵钉着)。单目标的 `check ./skill` 仍回显敲进来的路径。**不要从敲进来的 root 取 home**:`filepath.Dir` 只看字符串,`--root .` 下
  home == root(用户级 MCP、home 的 CLAUDE.md、桌面版全在 root 里找),`--root .claude` 下 home 是相对的 `.`,以绝对路径链接安装的
  skill 被 `withinDir` 当成"指到 HOME 外"丢掉。也不要解析 root:`~/.claude → ~/dotfiles/claude` 的 home 会被挪走。**root 顶层的
  `.mcp.json`/`.claude.json` 照读**(`collect.RootMCPConfigs`,与 home 那份是同一个文件时不读两遍):CI 模板 `scan --root .` 把仓库
  当 root,项目 MCP 配置就在那里;以前只有 `.` 因为 home == root 碰巧读到,绝对写法不读,而 `rootOwned` 声称读了(P-012)。
- **root 采集器是布局白名单,所以必须有人认领"剩下那一半"**
  ([collect/unowned.go](../../internal/collect/unowned.go))。`~/.claude/install.sh` 里写
  `curl | bash` + `rm -rf /`,以前扫出来是 **100/100 + "✅ No risk findings" + 退出码 0** ——
  这是全工具唯一一处**连自己漏了都不说**的缺口(违反不变量 #5)。读不读的判据是**agent 有没有加载路径
  进得去**,而不是名字、也不是内容形状(这两条都试过并且都错了,理由写在文件头,别再走一遍):
  - `skills/`、`agents/`、`commands/` 是加载命名空间 —— **缺清单也照读**(缺 `SKILL.md` 的目录出
    `KindDirectory`,`agents/`/`commands/` 下的子目录同理)。
  - 顶层**散落文件**:像代码(扩展名或 shebang)或像说明(`.md`)才读。
  - 顶层**目录一律不读** —— 真机上那是 `sessions/`、`file-history/`、`shell-snapshots/`,即**用户
    自己的会话记录和代码快照**;读进报告等于用一个盲点换一次泄露。依据和 `ExcludeFromHash` 同一条:
    没人引用的树是惰性的,而**会引用它的东西(hook command、权限授权、SKILL.md)本来就跟进去扫了**。
  - 没读的一律进**一条**聚合 `COV-000`(名字写在 `Why` 里 —— 终端渲染器对 note 只印一行证据)。
  - **注意"扫得更多"会把总分推高**:环境分是各单元的平均值,每个无人认领的条目都变成 100 分的
    artifact 会**稀释**已有发现(真机上 86 → 97)。所以不读的东西出 note 而**不是**出 artifact(插件子项不稀释,是因为它们和插件同一个单元)。
- **Claude 桌面版有自己的插件/skill 仓库,不在 root 里**([collect/desktop.go](../../internal/collect/desktop.go))。
  桌面版在 Customize 里装的插件和 skill 同步到 `~/Library/Application Support/Claude/local-agent-mode-sessions/`
  下,启动 CLI 时用 `--plugin-dir` 塞进去,**不经过** `installed_plugins.json`——只读 root 的扫描对它们全盲,
  而这恰是非开发用户的安装路径(2026-09-04 真机确认,app bundle 里的日志串就叫 "--plugin-dir session plugins")。
  采集器按 home 定位(和 MCP 配置同一约定),所以项目级 root 天然采不到;布局是未文档化的内部实现,
  **只允许单向 best-effort**:目录不存在 → 什么都不出(没装桌面版的机器、Linux、项目 root 都是这种情况,
  每次都印的披露会教人跳过披露);目录在但读不了/解析不了/越界 → `IO-000`/`COV-000`/`SCOPE-001`。
  桌面版的 skills 包**按单个 skill 采**而不是整包一个 artifact——用户一上传整包哈希就变,单个 skill 的树哈希
  才稳得住,信誉和批准都靠它做 key。`PluginPaths` 也并入桌面版 bundle(CLI 装的同名 bundle 优先),否则
  只在桌面版装的插件在闸门里永远是 `GATE-000`。**默认 root 先读 `$CLAUDE_CONFIG_DIR`**(`main.defaultRoot`),
  Claude Code 自己就是这个顺序;写死 `~/.claude` 在搬过目录的机器上扫的是空目录,而空目录是 100 分。
- **在 Cowork/云端沙箱里跑,报告要说清"这不是你的电脑"**([collect/environment.go](../../internal/collect/environment.go),2026-09-08)。
  沙箱每次给一台一次性 Linux 机,扫的是它自己几乎空的 `/root/.claude`,回来就是 ~100 分。非技术同事只打开那个 HTML
  文件(不看对话)会把绿色 100 当成自己电脑安全了。`DetectEnvironment` 用**多个独立信号**判:容器痕迹(`/.dockerenv`、
  `/proc/1/cgroup` 里的 docker/containerd)、Linux 上 home 是 `/root`、config root 下有 Cowork 的运行时文件
  (`policy-limits.json`/`launcher-settings.json` 等,至少两个)。**要 ≥2 个信号才判沙箱**,方向是安全的:最坏给一台真
  Linux 机多印一行,绝不会把沙箱当本机。它**不改分、不藏发现**,只在 `ScanResult.Sandbox` 上加注,三个渲染器在最顶上印一条
  醒目横幅(带判断依据)+ 一句"去 Code 标签在本机跑"。**关键是横幅进了报告文件本身**,不依赖 Claude 临场解释——文件被单独
  转发时也带着警告。`check` 不填它(单目标不是环境扫描)。
- **下载目录是另一条流水线,不进环境分**([internal/inbox](../../internal/inbox/),`cmd/aguard/inbox.go`)。`scan` 默认还看
  `~/Downloads`(`--inbox`,`off` 关),找出像 agent 的东西——有 SKILL.md / `.claude-plugin/plugin.json` / `.mcp.json` /
  CLAUDE.md / AGENTS.md / .cursorrules 的目录、这些散落文件、索引里带这些名字的 zip——每个单独走 `checkTarget`,结果放
  `ScanResult.Inbox`,报告里单独一节。**三条边界,别动**:(1) 不进 `Overall`——分数的含义是"agent 会加载的东西",一个下了
  没装的恶意 zip 不能把环境压到 49,一堆干净下载也不能稀释真发现;(2) 只读候选,其余**只计数、不读、不列名**——Downloads
  是最私人的目录,理由和不读 `sessions/` 同一条;**深度检查只跑候选**(2026-09-08 起,`--llm` 时由 `analyze()` 顺路跑,`checkCandidate`
  不要再调一次 `runJudge`——第一版就是这么付了双份钱),候选是已经被读过打过分的东西,其余文件不进判官;(3) zip 先看索引再决定要不要解,解到 0700 的临时
  目录、单文件 1 MiB、总量 64 MiB、条目 2000 上限,任何带 `..` 段或绝对路径的条目一律拒(不清洗)、软链/特殊文件不重建、
  按实际写入的字节而不是索引声明的大小计数(zip 炸弹撒谎的正是索引),查完删干净;解到私有临时目录里以包文件名命名的子目录,
  查完经 `archiveView` 把路径改写成包——**报告里不许出现解压目录**,它的随机名曾让同一个 zip 每次的 SARIF 指纹都不同(P-016)。默认目录不存在是**无事**(CI 没有
  Downloads),显式 `--inbox` 不存在是错误,和 `--root` 打错同一条。常驻监控和自动隔离是刻意没做的下一步,见 ROADMAP。
- **`synced/` 在沙箱里比在网页深一层,采集器要能穿过 uuid。** `skills/synced/<name>/SKILL.md` 是 claude.ai 网页启用的 skill
  (深一层);Cowork/云端沙箱把整个账号的 skill 按会话 uuid 分组,变成 `skills/synced/<uuid>/<name>/SKILL.md`(再深一层),插件
  则是 `plugins/synced/<uuid>/<plugin>/`,且**没有 installed_plugins.json**。老的只钻一层的走法两样都看不到,真机上报告只出
  skills=1 plugins=0,而实际有 16 个 SKILL.md(2026-09-08 真机确认)。修法:`collectNestedSkills` 带一个 depth 预算穿过 uuid 那层
  (label 不带 uuid,报告写 `synced/<name>` 不写 `synced/<uuid>/<name>`);`collectPlugins` 先走 `collectSyncedPlugins` 扫
  `plugins/synced/<uuid>/<plugin>/`——即使 manifest 不在也照扫,每个带 `.claude-plugin/plugin.json` 的目录按 manifest 插件同样处理
  (整树 + 自带 hook + 自带 MCP)。散落在顶层、没有 settings.json 注册的 hook 脚本**当普通脚本扫是对的**:没人注册就不会静默触发,
  按 (event,command) 审一个不会运行的文件反而是造事实。
- **artifact 树里"存在但读不了"的条目必须显形**(`detect.unreadableNote`,W-001,2026-09-15)。`readTextTree` 的 WalkDir 回调和
  `readCapped` 的 Open/Read 失败以前一律 `return nil` 什么都不说,于是 `chmod 0111 sub/` 就把一个 13 分、四条发现的 skill 变成
  100 分零 note——agent 跑 `sh sub/inner.sh` 只要 +x,`ReadDir` 要 +r,mode 位是作者自己选的,这是刻意可达的规避形状,不是意外。
  现在聚合成**一条** medium 的 `COV-000`(medium 而不是 low,因为默认视图只印 notes 里最高的严重度,low 会藏在超大文件那条后面),
  内容照旧不读。**反向断言**:可读的树不许多出这条 note(`TestUnreadableEntryIsDisclosed` 两个方向都钉)。同一改动里
  `collect.TreeHash`(W-005)把读不了的条目以固定标记 `sha256("aguard:unreadable-entry")` 折进哈希——哈希是信誉库和闸门批准的 key,
  "缺文件 X 的树"和"含读不了的 X 的树"以前同 key,针对前者的批准会覆盖后者;可读树的 golden 常量没动。
- **`@import` 指向凭据的一律拒读,并且计分**([collect/imports.go](../../internal/collect/imports.go),W-006,2026-09-15)。`CLAUDE.md`
  里一行 `@~/.env` 以前会让扫描器读它、把 `.env` 的真实 sha256 当第二个 artifact 的 Hash 发布进报告、按脚本跑全部规则、开 `--llm`
  时整个内容出网,而 notes 为空、总分 100。破的不是不变量 #3(Redact 照常跑了),是**"决定去读"**:一个自称 best-effort 的脱敏器被
  抬成了用户真实 secret 的最后一道防线。现在除了原有的目录名单(`.ssh`/`.aws`/…),按**文件名**也拒:`credentialFile` 分两档,
  high 是只装 secret 的名字(`.env`、`.env.*`、`id_rsa` 等私钥、`credentials`、`*.key/*.p12/*.pfx`…),medium 是常只装配置的
  (`*.pem`、`.npmrc`、`.pypirc`、经 `.config` 的路径)。`.env.example/.sample/.template/.dist` **不是凭据,照读照扫**——放宽成 `*env*`
  等于送攻击者一个藏 payload 的文件名,`TestImports_BenignEnvSiblingIsStillScanned` 钉着这个方向。拒绝的东西**不创建 artifact**
  (所以没有哈希、判官拿不到),出一条 `COV-000`(high)**加**一条计分 `EXFIL-005`(维度 3)挂在**导入它的文件**上——照 `HOOK-002`
  的先例,覆盖和风险是两句话;计分那半同时堵住按名拒绝的倒转(把 payload 命名成 `secrets.yaml` 换不来 100 分)。三条验收里
  真正钉住的是 **"`.env` 的 sha256 不出现在任何 artifact 的 Hash"**,只断言"没有 Path 等于它"的测试在哈希从别的字段漏出去时照样绿。
  `expandImports` 因此改成返回带发现的 seeds 副本,不原地改。
- **加载命名空间里"解析不了"的条目必须显形,但"悬空"的不能。** `collectSkills` /
  `collectNestedSkills` / `collectPlugins` 里,`EvalSymlinks`/`Stat` 失败以前一律静默 `continue`,
  于是 `skills/` 下一个成环的软链能让那个 skill **从清单里凭空消失**,而报告照印
  `✅ No risk findings` —— 同一个循环里"指到 HOME 外"却是出 `SCOPE-001` 的,说了一半。
  现在按 `hidesContent` 分开:
  - **`fs.ErrNotExist`(悬空软链,卸载留下的)不报** —— 磁盘上真的没东西,Claude Code 也加载不了,
    报了就是在描述一个不存在的盲点。真机上这种东西成堆,**每次都印的披露会教会操作者跳过披露**。
  - **其它任何错误(权限、成环、名字过长)出一条聚合 `COV-000`** —— 那才是"东西在那儿,我没看到"。
  两个方向都有反向断言钉住(`TestCollect_UnresolvableSkillEntryIsDisclosed` /
  `TestCollect_DanglingSkillSymlinkIsNotReportedAsAGap`),**改这里必须同时保住两条** ——
  这个改动的典型失败方式就是修好一边、弄坏另一边。
- **`check` 不自动发现基线** —— `scan`/`clean` 的 root 是运维自己的环境,读 `<root>/.aguardignore`
  合理;`check` 的 root 就是被审对象,目标里的 `.aguardignore` 是攻击者可控的,一个 skill 随包捎
  一份列着自己规则 ID 的基线就能把自己判成 100/100。`check` 只认显式 `--ignore`
  (`main.resolveIgnorePath` 的 `auto` 参数)。
- **frontmatter 只认从第一个字节开始的那份**(`parse.splitFrontmatter`,P-024)。Claude Code 2.1.107 实测:BOM、空行、一行空格之后的
  `---` 一律不算——规则每次会话都加载,skill 的描述被列成 `---`,子 agent 不加载。**别为了"宽容"把前导字节跳过去**:报告会把每次都读的
  规则标成 `(path-scoped)`,给没人看得到的描述算 context_bloat。`(path-scoped)` 还要 `paths` 里剩一条 Claude Code 会用的 glob
  (`parse.honoursPaths`:`[]`、`""`、`**`、数字都等于没写;花括号展开有上限,超限按"每次都加载"答)。`TestPathScoped_MatchesClaudeCode` 按实测逐行钉住。
- **`--fail-on-llm` 有第三个答案:退出码 4**(`failGate` + `judgeGap`,P-026)。没有闸门命中、但判官没跑或跑短了(`Judge` 为 nil、`!Ran`、`Failed>0`、`Skipped>0`)是 4 不是 0 —— 以前是 0,靠判官卡门的 CI 恰在判官看不见时变绿。
  **别让它碰 `--fail-on`**(只设 `--fail-on` 时永不读判官状态,那是可复现契约);**别把截短的摘录、`LLM-005`、下载目录的判官摘要算进来**
  (都是回答过了,算进来 4 会在任何大文件上响,然后没人再看);**1 先于 4**(命中的闸门就是答案)。`TestFailGate_LLMGateNotEvaluable` 两向都钉。
  P-038 加了第四个理由:**`check` 的目标本身是判官什么都不问的**(`judge.Unasked`:directory/quarantined,以及**没有任何可加载
  skill/命令/子 agent 的** plugin —— 有的话判官经子项回答,P-044)也是 4;`scan` 和 root 形的 `check` **不**因此退 4,只出一条 `LLM-000`
  计数(note 和退出码读同一个函数)。triage 不算问题。
- **插件的 skill、命令、子 agent 是各自的 artifact,但不是各自的一票**(`collect/plugincontents.go` + `score/family.go`,P-044)。只采
  Claude Code 2.1.107 加载器会加载的(一种情形一行 fixture,`TestPluginContents_FollowClaudeCodesLoader`;**别"顺手"多采 `docs/`、`.agents/`
  这类副本**,那是 `issues/017`),插件树 artifact 与哈希不动。同一份字节被读两遍,所以凡是会数两遍的地方都问 `score.Families`:环境分按
  单元算、人读的渲染器和 SARIF 只在插件那一行印一次(`ShownByPlugin`,判官的分诊同一个谓词)、信誉 GOOD 命中插件树时子项继承、闸门的
  SessionStart/`Summarize` 和 hygiene 跳过子项。**加一个会遍历 artifact 并计数或列名的地方,先想它该不该跳过子项。**

每个包都是围绕 [internal/model/model.go](../../internal/model/model.go) 中不可变类型的一个
(近似)纯函数阶段。

