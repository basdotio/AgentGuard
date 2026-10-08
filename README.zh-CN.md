<!-- SPDX-License-Identifier: MIT -->
# AgentGuard(`aguard`)

[English](README.md) | **中文**

![go](https://img.shields.io/badge/go-1.23%2B-00ADD8)
[![license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

> 本地、离线的 AI Agent 环境安全体检 + 垃圾清理工具。给用 Claude Code 的开发者一个"agent 版 360":**扫恶意、查权限、清垃圾**,一条命令,不出你这台机器。
>
> `scan` 与 `check` 严格只读。`clean` 是唯一会写盘的命令,而且它只在你的配置根目录内部**移动**——**永不删除**。

## 是什么

AI 编码 agent 会**自动加载**一堆外部东西——skills、MCP 服务器、hooks、subagents、指令文件(CLAUDE.md)——每一个都拿到你的真实工具和权限,任何一个都可能藏隐藏指令。AgentGuard 在你本地静态扫描这些 artifact,给出风险体检 + 垃圾清理建议。

**定位:体检 + 预警 + 清理,不是"查杀保证"。** 风险分是相对风险提示,不是安全认证。详见下方"能力边界"。

## 特点

- **本地优先、离线**:不执行任何被扫内容,不联网,不上传任何代码。扫描严格只读;唯一会写盘的 `clean` 只移动、不删除。
- **单二进制**:纯 Go、`CGO_ENABLED=0`,`curl` 下来即可跑。
- **10 维度检测**:注入 / 过度权限 / 数据外泄 / 代码执行 / 供应链 / 混淆 / 后门 / 资源滥用 / 文件系统 / 意图不符(意图判定为可选 LLM 增强,默认关)。
- **永不删除的垃圾清理**:重复、上下文膨胀(量化可回收 token)、失效引用、僵尸 skill。`clean` 只会把东西**移进**你配置根目录下的回收目录,每一步都记录在案,`--undo` 能原路放回。删除永远是你自己动手。
- **权限体检**:内联密钥、通配任意命令执行、过宽路径、缺 deny 兜底。
- **加载时闸门**:注册成 Claude Code 的 hook,在 **agent 加载一个 skill 之前**审它,带发现的会先问你一句 —— 见 [`docs/install-gate.zh-CN.md`](docs/install-gate.zh-CN.md)。
- **能在 Claude Code 里直接跑**:自带插件(三个 skill + `/aguard-setup`、`/aguard-scan`、`/aguard-vet`、`/aguard-gate`、`/aguard-help`、`/aguard-llm`),用大白话要一次体检,拿回来的是分好级的结论,不只是一屏输出。
- **隐私**:密钥值在生成报告前即 `<REDACTED>`,原始凭证绝不落入报告。

## 安装

**从 npm 装**(macOS / Linux,不用自己挑平台)。二进制就在包里,所以没有安装脚本,
装的时候不下载任何东西:

```bash
npx --yes @bas.io/guard@latest check ./some-skill   # 装之前先审一个东西
npx --yes @bas.io/guard@latest scan --report        # 体检这台机器的整套配置
npm i -g @bas.io/guard                              # 或者装下来:PATH 上多一个 `aguard`
```

**先用 `check`**:它判断的是你指给它的那个 artifact,这个论断跟在哪台机器上跑无关;
而 `scan` 论断的是**它运行的那台机器**。

> **不要从 `npx` 装加载时闸门。** `aguard hook install` 注册的是它运行时那个二进制的绝对路径,
> 而 npx 的路径是 npm 之后会回收的缓存 —— 回收之后每个 skill 都未经审计地加载,
> 而外表看起来仍然受保护(`GATE-001`)。命令发现这种情况时会警告你。
> 要装闸门就用 `npm i -g`,或者用 release 二进制。

**下载预编译二进制**(macOS / Linux,不需要装 Go)。平台从
`darwin-arm64` · `darwin-amd64` · `linux-amd64` · `linux-arm64` 里挑:

```bash
REPO=basdotio/AgentGuard
PLAT=darwin-arm64                                   # <- 换成你的
curl -fsSLO "https://github.com/$REPO/releases/latest/download/aguard-$PLAT"
curl -fsSLO "https://github.com/$REPO/releases/latest/download/SHA256SUMS.txt"

# 先校验再运行 —— 一个安全工具没资格要求你无条件信任它自己的下载。
shasum -a 256 --ignore-missing -c SHA256SUMS.txt     # Linux: sha256sum
chmod +x "aguard-$PLAT" && sudo mv "aguard-$PLAT" /usr/local/bin/aguard
```

**从源码构建**(Go 1.23+):

```bash
git clone https://github.com/basdotio/AgentGuard && cd AgentGuard
make build          # 产出 bin/aguard
# 或直接:
CGO_ENABLED=0 go build -o bin/aguard ./cmd/aguard
```

每个 release 都带这四个静态二进制加一份 `SHA256SUMS.txt`,由
[`.github/workflows/release.yml`](.github/workflows/release.yml) 从版本 tag 构建 —— 跑的就是
维护者本地那条 `make dist`。

**Windows 只支持从源码构建,这是有意的。** 它能交叉编译通过,但测试套件从没在上面跑过一次,
代码里没有任何 `GOOS` 分支,而扫描器依赖的符号链接边界在那边行为不同。一个从没被执行过的
二进制,对于一个"用户会照它的判定采取行动"的工具来说,比少一个平台更糟。

## 在 Claude Code 里直接用(插件)

上手最快的一条路。仓库自带一个插件,含三个 skill 和六个斜杠命令 —— 你用大白话问就行
("我的 `~/.claude` 安全吗"、"这个 skill 装之前帮我审一下"),不用背参数:

**终端版** Claude Code,用斜杠命令:

```
/plugin marketplace add basdotio/AgentGuard
/plugin install aguard@AgentGuard
```

**VS Code / JetBrains 扩展**里没有 `/plugin` —— 到 shell 里用 `claude` 命令跑同样两步:

```bash
claude plugin marketplace add basdotio/AgentGuard
claude plugin install aguard@AgentGuard
```
终端里第三方 marketplace 默认**不**自动更新。打开一次,skill 和命令的更新就会自己到:`/plugin` →
**Marketplaces** → `AgentGuard` → **Enable auto-update**(桌面版添加 marketplace 时的 "Sync automatically"
就是同一个开关)。二进制是另一回事,它从不自动更新;`aguard version` 会说它是否落后于插件。

**v0.17.0 之前装过插件?** 那时它叫 `agentguard`,这个名字不会再收到更新。改名是因为在不区分大小写的文件系统上
(macOS 默认就是),它和 marketplace 名 `AgentGuard` 在 Claude Code 的安装缓存里撞成同一个目录,安装报 `EINVAL`
([issues/022](issues/022-plugin-install-case-collision-macos.md))。换一次就好;`aguard version` 会按你的安装给出确切命令:

```bash
claude plugin install aguard@AgentGuard
claude plugin uninstall agentguard@AgentGuard
```


仓库是公开的,插件就在 `main` 上,所以直接用上面那条命令即可 —— 不需要 `#…` 后缀,
不需要仓库访问权,也不需要配 git 凭证。

然后重启 Claude Code(扩展里是 reload window),再跑 `/aguard-setup`。它会在二进制缺失时装好、
跑第一次扫描、陪你把发现过一遍、最后问你要不要装加载时闸门。`curl` 下载不需要 token —— 它读的
release 是公开的。从源码构建(`make build`,Go 1.23+)这条路仍然可用。

| | |
|---|---|
| `/aguard-setup` | 从零到被守住:二进制、首次扫描、修复计划、闸门 |
| `/aguard-scan [root]` | 体检一个配置根,并把发现分级成可执行的清单 |
| `/aguard-vet <路径\|URL>` | 装之前/提交之前审一个 skill、插件或文件 |
| `/aguard-gate [动作]` | 装/查/修加载时闸门,管理 approvals |
| `/aguard-help` | 白话版使用说明:你可以怎么问它,不用记任何命令 |
| `/aguard-llm [status\|test\|setup]` | 配置或检查可选的 AI 深度检查(用你自己的在线模型账号) |

三个 skill(`agentguard-audit`、`agentguard-vet`、`agentguard-gate`)在请求对得上时也会自己
触发。它们带的是 `--help` 给不了的那部分:两个分数怎么读、哪些发现是"形状"而不是"判决"、
维度 0 的 note 为什么不是风险、以及一份干净的报告仍然**没有**证明什么。

插件就是 [`plugin/`](plugin/) 这棵子树 —— **只有 skill 和 command,故意不含本仓库自己的文档**。
那些文档把引擎能检出的攻击模式全都原文引了一遍,一起发出去的话,每个用户跑 `aguard scan` 都会
看到本插件自己被报成高危。这棵子树在 `--fail-on low` 下扫出 `100/100`,改动把这条打破了,那是
改动的 bug。

需要 `aguard` 二进制在 `PATH` 上(skill 会帮你装)。插件不带 MCP server,也不注册任何自己的
hook —— `aguard hook install` 仍然是一个显式的、先 `--dry-run` 给你看的步骤。

## 用法

```bash
aguard scan                      # 全盘体检当前用户的 ~/.claude
aguard scan --root ~/.claude     # 指定根目录
aguard scan --verbose            # 额外打印每条覆盖度说明的完整正文
aguard scan --html report.html   # 额外写出自包含 HTML 报告
aguard scan --md report.md       # 额外写出 markdown 版报告,贴进 PR 评论或 issue("-" = stdout)
aguard scan --json               # 机器可读输出
aguard scan --fail-on high       # CI/闸门:发现 ≥high 时退出码 1

aguard clean                     # 垃圾清理:列出可清项 + 量化可回收 token(纯读)
aguard clean --zombie            # 额外找"装了但没用过"的 skill(弱信号,需显式开)
aguard clean --zombie --apply    # 移进 <root>/.aguard-trash(永不删除)
aguard clean --undo last         # 原路放回,每一步重新推导安全判断
aguard clean --ask               # 逐对回答重复的 skill(↑/↓ 或敲 1/2)

aguard check ./some-skill        # 装前闸门:静态扫描单个 skill/目录/文件(默认 --fail-on high)

aguard hook install              # 加载时闸门:agent 加载每个 skill 之前先审一遍
aguard approve ./some-skill      # 信任这份确切的字节,闸门不再问
aguard approvals                 # 列出已信任的内容

aguard version
```

终端报告默认列出**全部**发现,并把覆盖度说明折成一行 —— 这行仍然带着条数、其中的最高严重度和
规则 ID。`--verbose` 把这些说明的正文完整打出来,想知道"这次扫描哪些地方**没读**"时用它。
两种模式下发现都不会被折叠;`--json` / `--html` / `--md` 始终包含全部内容,所以 CI 看到的东西不会因为
某个人加没加这个参数而变。

**退出码**:`0` 未达阈值 · `1` 发现 ≥ `--fail-on` 级别 · `2` 运行错误
(以及**仅 `clean` 有的** `3` = 部分执行,每一条被拒的都点名了原因)。
被信号中断的运行以 `128 + 信号号` 结束 —— Ctrl-C 是 `130`,输出管道被关闭是 `141` ——
经 npm launcher 和直接跑二进制完全一致。
`scan` 默认不因发现而非零退出(信息模式);`check` 默认 `--fail-on high`(闸门模式)。

**报告里出现一条发现,想知道它是什么意思?**[`docs/rules.md`](docs/rules.md) 列了工具能打印的
每一个规则 ID —— 维度、严重度、以及它为什么会响。它由引擎自己的规则集生成、CI 会校验不漂移,所以
你在那里读到的严重度就是会拦你构建的那个严重度。(该文件是英文的:正文即代码里的字符串。)
它的页眉写着**规则版本**,和报告 `--json` 里的 `rules_version`、`aguard version` 打印的是同一个值:
两份报告不一样、规则版本也不一样,就是两次之间规则变了。

## 清理(`clean`)

`clean` 是唯一会写盘的命令,它的边界窄到可以一次说完:

- **它永不删除。** 它动的每一样东西都是**移进** `<root>/.aguard-trash/`,而且**先记录再移动**。
  `--undo` 放回去时,每一个安全判断都从文件系统**重新推导**,而不是相信记录里写的。
  确认无误之后的 `rm -rf` 由你自己执行。
- **只动 `skills/`。** `settings.json`、`settings.local.json`、`.claude.json`、`.mcp.json`
  以及 `hooks/` 下的任何东西,**永不被移动、也永不被覆盖恢复**——而且是**解析完符号链接之后**才判断,
  因为对"写下来的路径"做的判断,是一个符号链接可以骗过的判断。
- **被隔离的东西仍然计分。** 移进回收目录**不会**让你的分数变好,只有真的删掉才会,而那是你的动作。
  否则 `clean --apply` 就成了把红色 `--fail-on high` 变绿的最短路径。
- **没有一样东西是替你选的。** 一对重复的 skill 问的是"留哪一个",而且**没有默认边**:
  用 `--resolve <id> --keep <名字>`、`--keep-both`,或者 `--ask` 逐对过一遍。
  **一个可逆的操作建立在猜测上,它仍然是猜测。**

`--zombie`("装了但没用过")需要显式开启,而且**置信度永远到不了 high**:
它只看得见这台机器的历史,并且会告诉你那是**几次会话**。

完整说明(含安全边界与它刻意不做的事)见 [`docs/clean-guide.zh-CN.md`](docs/clean-guide.zh-CN.md);
实现与"不变量 ↔ 钉住它的测试"对照表见 [`docs/internals/clean-internals.md`](docs/internals/clean-internals.md)。

## 加载时闸门(`aguard hook`)

`check` 只在你想起来问的时候回答;闸门是在一个 skill 即将被加载的那一刻回答 —— 不管有没有人想起来:

```bash
aguard hook install    # 合并进 ~/.claude/settings.json(先备份),然后重启 Claude Code
```

Claude Code **没有**"安装"这个 hook 事件,而且就算有也覆盖不全 —— 一个 skill 还可以是 `git clone`、
`cp`、或者手动放进去的。加载才是守得住的那条边界,而且它就是要紧的那条:**磁盘上的 skill 是惰性的**,
在 agent 把它读进上下文之前什么也做不了。批准以 artifact 的 **canonical 哈希**为 key,所以改动一个
已批准的 skill,闸门自己就会重新问。完整契约(包括它**刻意不覆盖**的部分)见
[`docs/install-gate.zh-CN.md`](docs/install-gate.zh-CN.md)。

## 检测对象

skills(SKILL.md + 脚本 + 资源)· MCP 配置(静态只查注入文本和 `NODE_OPTIONS` 这类解释器预加载的 `env`;包没钉版本、`env` 里写死凭证这两种只有可选的 LLM 判官会报,`LLM-009`)· **hooks**(settings.json,可静默执行 shell,重点)· **权限白名单** · subagents · slash commands · 已安装的**插件** · CLAUDE.md · **Claude 桌面版自己的仓库**(在 Customize → Plugins 装的插件和 Customize → Skills 里的 skill 放在 `~/Library/Application Support/Claude/` 下而不是 `~/.claude`,由桌面版以 `--plugin-dir` 交给 CLI;会被采集并标注 "via Claude Desktop")。默认 root 会读 `$CLAUDE_CONFIG_DIR`。**下载目录**也查:`~/Downloads` 下长得像 agent 的东西(skill 文件夹、插件、MCP 配置、指令文件、装着这些的 `.zip`)逐个单独检查,列在单独的 Downloads 一节——它们没装进去,所以永不计入分数;目录里其余东西只计数,不读、不列名。`--inbox <目录>` 换位置,`--inbox off` 关闭。`aguard check foo.zip` 可以直接查一个下载的压缩包。安装型软链 skill(如 `~/.agents/skills/*`)会解析真身审计;越界指向系统路径的软链会被跳过并告警。

其中两个面单独加强 —— 它们都是"看着很窄、实际交出执行权":

- **hooks 按 command 逐条审计**,不是按事件:一条发现能指名道姓到 `PreToolUse[Bash]#1`。
  如果 hook 只是指向一个脚本(`sh "$CLAUDE_PROJECT_DIR/.claude/hooks/pre.sh"`),那个脚本
  也会被读进来一起扫 —— 否则一个文件名就把 payload 藏住了。被引用的脚本只在 home 目录内读取,
  越界的不读(`COV-000`)并另报 `HOOK-002`(计分):覆盖缺口和"静默执行点上有一段审不了的第二阶段"
  是两回事。`PermissionRequest` 升为 high,因为它拿走的是授权决定权。`type: http` 的 hook 是正式
  审计对象:转到本机是 `HOOK-003` low,`PermissionRequest` 转到本机或任何非回环目标都是 high。
  hook command 里出现 shell 串联(`;` `&&` `||` `|` 反引号 `$(`)
  报 `HOOK-001` —— 脚本里有管道再正常不过,hook 里才说明问题,所以这条规则只在 hook command 上跑。
- **权限白名单**会比对"可逃逸二进制"清单(`PERM-006`):`Bash(git *)` 就是套着窄外衣的任意执行
  (`git -c core.pager=…`),`find` `awk` `tar` `docker` `ssh` `npm` `make` `xargs` `rsync`
  这类开放参数的授权同理。参数写全的授权(`Bash(git status)`)不会被报。授权指向**你自己的脚本**时
  (`Bash(./scripts/deploy.sh *)`),那个脚本也会被读进来一起扫 —— 这条授权有多窄,取决于脚本干什么。
  边界与 hook 相同:只在 home 目录内读取,越界的报成覆盖缺口。

## LLM 意图判定(可选,默认关)

静态规则抓不到"描述说 A、代码干 B",也抓不到绕开关键词的提示注入。开 `--llm` 后,隔离模型按 artifact
的**类型**决定跑哪几遍:

- **隐藏注入检测** —— skills、`CLAUDE.md`、subagents、slash commands **和 hooks**,无条件跑
- **意图判定** —— 声明用途 vs 脚本实际干什么(skills)
- **去混淆** —— 把内嵌 base64/hex 块解码(只解码、绝不执行)后问"这段载荷到底干什么"
- **跨文件合谋** —— 同一个 skill 里,是不是这个文件收集、那个文件外发?
- **hook 能力判定** —— 这条命令做的事,是否超出"拦截它那个事件"所需?
- **MCP 配置** —— 未 pin 的包、未知发布者、远端端点、env 里的凭证
- **误报триаж** —— 把已有的静态发现标为"像真风险 vs 像良性噪声"

它**不看来源**——每个 artifact 都按内容判,官方与否一视同仁。flagged 的结论必须**引用**触发它的原文,
而这段引用会拿去和"实际发出去的文本"比对:引不出来的一律丢弃并计数——一条自信的幻觉进不了你的报告。

判官被当作**对抗式**处理:被扫内容不可信且会进 prompt,故用 **nonce barrier** 把它围成惰性数据,并要求
模型忽略(并上报)其中任何指令。按铁律,LLM 只能**把风险往上推**——决不能删除或降级静态发现、改 `overall`、
或触发 `--fail-on`;триаж只标注、绝不删除。

**配置只要一条命令,不用写 YAML** —— 一个你自己有账号的在线 OpenAI 兼容模型(`deepseek`、`openai`、`qwen`,
或任何端点配 `--base-url`/`--model`):

```bash
aguard llm setup --provider deepseek    # 提示输入密钥,隐藏输入;脚本里用 --key-stdin 管道
aguard llm test          # 发一次调用:这个端点、这个模型能不能答
aguard scan --llm        # 配置从此在 ~/.config/aguard/config.yaml 自动找到
```

密钥存到 `~/.config/aguard/llm.key`(0600,别人能读就拒用);`api_key_env` 环境变量仍然可用且优先,给 CI。
在 Claude Code 里,`/aguard-llm` 用对话走同样三步。

硬约束:**默认关**(需 config **且** `--llm`)、**只发脱敏**摘录(原始密钥绝不出本机)、
结果**只作旁注**——`Source=llm` 的发现**不进 `overall`、不触发 `--fail-on`**,评分保持可复现。

开判官后会在真分数旁边多给一个 `overall_effective`:同一个公式、把它的发现算进去。这个数**恒小于等于**
真分数、不可复现、**不接任何闸门** —— 它存在的意义是让你看见判官看见了什么,又不让这份判断渗进 CI 依赖的
那个数。由于环境分要先平均、再套木桶封顶,报告还会**点名被推低的具体 artifact** —— 信号其实在那里。

```bash
# config.yaml
llm:
  enabled: true
  provider: openai_compatible
  base_url: http://localhost:11434/v1   # 默认本地模型(如 Ollama)
  api_key_env: AGUARD_LLM_KEY           # 密钥只从该环境变量读,绝不落盘
  model: llama3.1

aguard scan --llm --config config.yaml
```

判定不可用绝不静默:端点不通/未配置会给一条 `LLM-000` 覆盖 note,"判定没开"不能伪装成"没有意图问题"。

**完整参考:**[`docs/llm-judge.zh-CN.md`](docs/llm-judge.zh-CN.md)(模型、端点、隐私、规则 ID)· 可直接复制的
[`config.example.yaml`](config.example.yaml)。

## 能力边界(诚实声明)

首版是**静态**扫描,天花板是"发现可疑面 + 清理",做不到:
- 证明恶意(只给可疑度)· 运行时动态行为(远程二段 payload、条件后门)· 加密/强混淆载荷的真实意图 · MCP 端点实际行为 · 依赖包内部 · 零日/未知手法。

还有几处覆盖边界需要知道:
- **插件**按整棵树扫,里面打包的 skills/commands/hooks 内容**会**被读到,但插件自带的 hook
  不会像 `settings.json` 里的那样按 (event, command) 逐条审计。
- **hook 引用的脚本**只跟进一层,且只在 home 目录内读取。越界不读,并计分为 `HOOK-002`。
- 配置 root 下**没有采集器认领的顶层目录不读** —— 真机上那些是你自己的会话记录、配置备份和 shell
  快照(`sessions/`、`history.jsonl`、`file-history/`),把它们读进报告等于用一个盲点换一次泄露。
  它们会被一条 `COV-000` **点名**,不是静默跳过。看起来像代码或说明的散落**文件**照读;`skills/`、
  `agents/`、`commands/` 里的内容即使缺了清单也照读 —— 那几处才是 agent 真正加载的路径。
- **生成/vendored 目录**(`dist/`、`build/`、`out/`、`node_modules/`、`vendor/`、`coverage/`)
  不读,也排除在 canonical 哈希之外(这样 artifact 重新构建后身份不变)。扫描会以一条 `COV-000`
  note 说明这件事。而且——**跳过是按名字判的,名字是 artifact 作者自己起的**——所以一旦 artifact
  把 agent **指进**这类目录(比如 `SKILL.md` 写"运行 `dist/setup.sh`"),会报 `SUP-004` 并计分。

维度 7/8(后门 / 资源滥用)的命中一律标注**"仅提示,非确认"**;`EXFIL-002`(跨文件的
凭证→外连组合)同样如此 —— 两个不相干的文件各占一半是很常见的,所以它只是"值得看一眼的组合",
不是结论。

外泄检查是**结构化**的,不是一条模式:同一个文件既读凭证又对外发起请求 = `EXFIL-001`;如果中间还
**先编码**,那是 `EXFIL-003`,并在混淆维度另出一条 `OBF-004` —— 因为这时离开这台机器的东西,对任何
盯着网络或日志的人都是不可读的。若该文件里所有网络目标都是回环地址(`127.0.0.1`/`localhost`/`::1`),
链仍报出,但降到与 `EXFIL-002` 同一档(low + advisory)—— 数据没离开本机。"对外"也不只是 HTTP
客户端:DNS 查询、`nc`、`/dev/tcp`、`scp`、`ssh` 一样能把数据带走。这些都不是证明:编码本身有正当
用途,所以发现说的是"攻击就长这个形状",绝不说"这就是攻击"。

## 基线(`.aguardignore`)

复核过一个环境的发现后,把可接受的静默掉,让重复扫描只剩真信号。`scan` 与 `clean` 读
`<root>/.aguardignore`(或 `--ignore <文件>`):

```
# 每行一条规则,空行和 # 注释忽略
INJ-001                 # 全局静默该规则
EXEC-004  vendor/*       # 仅当证据路径匹配时静默
*         dist/*         # 该路径下静默任意规则
```

被静默的发现在**评分前**丢弃,数量以 `IGN-000` note 报告——基线不会静默藏住真正的新风险。

`check` **不会**自动发现基线。它的目标正是你要审的东西,所以目标目录里的 `.aguardignore` 是写
artifact 的人写的,不是你写的——否则一个 skill 只要随包捎一份列着自己规则 ID 的基线,就能把自己
判成 100/100。`check` 只认显式的 `--ignore <文件>`。

## 信誉库(内嵌 allowlist / blocklist)

AgentGuard 内嵌一份小巧、**随版本更新、离线**的信誉名单,按 artifact 的 canonical
hash 索引——无网络,默认开:

- **已知良性**(如经人工审阅、记录了理由的官方市场插件 superpowers)→ 其发现被静默,可信工具自带的
  `rm -rf`/`curl|bash` 示例不再淹没报告。`REP-GOOD` note 记录条数 + 最高被抑制级别(不静默)。
- **已知恶意** → 即使静态零命中也升 `REP-BAD` 发现。

**目前名单里真正有东西的是 allowlist 那一半。** blocklist 的**机制**是接好的(哈希精确匹配、
`REP-BAD` 定级 critical、会 gate),但**内容是空的** —— 策展已知恶意哈希是持续工作,而这个文件
里不放任何充数的示例条目:它会进评分,一条演示记录就是一条假记录。`aguard version` 打印的是
真实条目数。所以现阶段请把信誉库的价值理解为"给可信工具包降噪",而不是"病毒库覆盖" ——
检出靠的是静态规则。

因为名单内嵌且版本化,它是确定的、**会进评分**。用 `--no-reputation` 关闭。维护者按哈希加条目:

```bash
aguard hash ./some-skill     # 打印 canonical hash,加进信誉名单
```

云端信誉服务(最新 allowlist + 社区 blocklist,只发哈希、可选开)在规划中——见设计文档;
内嵌名单是它永远可用的离线子集。

**Claude 桌面版自带的 skill**(docx、xlsx、pptx、skill-creator、import-memory 等)是名单里唯一不是仓库的
来源:其中几个没有任何可以钉 commit 的公开地址。它们的条目改钉桌面版自己给每个 skill 记的 `updatedAt`
(`source: claude-desktop`),审阅和指纹要求不变,续期只能在装了桌面版的机器上跑。

## 用作闸门(pre-commit / CI)

`check` 在发现达到 `--fail-on` 级别时返回非零退出码,可接入任意闸门:

```bash
# git pre-commit(扫暂存的 skill)
cp hack/pre-commit .git/hooks/pre-commit && chmod +x .git/hooks/pre-commit

# CI(GitHub Actions)
cp hack/github-action.yml .github/workflows/agentguard.yml
```

两者见 [`hack/`](hack/)。单个 skill:`aguard check ./some-skill`(默认 `--fail-on high`)。

## 报告

`aguard scan --html report.html` 写出自包含(离线、无脚本)HTML 报告:风险分仪表盘、
环境瓦片、按 artifact+规则聚合的行级取证发现、垃圾/卫生区。对你自己的 `~/.claude` 跑一次即可查看。
`aguard scan --report` 写同一份报告但不用你想文件名:落到 `~/.config/aguard/reports/scan-<时间戳>.html`
(或 `$XDG_CONFIG_HOME` 下),在扫描 root 之外,并打印路径。插件每次扫描都带它,`/aguard-setup` 结束时会
打开报告。报告只留在本机,里面有你自己的路径和脱敏后的片段。

`--md <路径>` 把同一份报告写成 GitHub 风味的 markdown,给扫描结果下一步通常要去的地方:PR 评论或 issue。
`-` 表示写到 stdout、不再印终端报告,审一个新 skill 的人不用手抄就能把结果贴上去:

```bash
aguard check ./new-skill --md - | gh pr comment 42 --body-file -
aguard check ./new-skill --md review.md      # 或者写成文件再粘
```

被扫目录里的每个名字、路径、片段都放在代码跨度里,所以一个叫 `@某人` 或 `<img src=…>` 的文件名
没法在你的评论里 @ 人或加载图片;发现表格、白话结论、"仅提示"标注、"没检查到"的说明都在,顺序和终端一致。
贴 `check` 或 `scan --root .` 的结果;整机 `scan` 还会列出它在 `~/Downloads` 里看到的东西,
公开贴之前加 `--inbox off`。

## 开发

```bash
make test           # go test -race -cover ./...
make lint           # golangci-lint
make dist           # 交叉编译发布二进制 + SHA256SUMS.txt 到 dist/
```

第一次接触这套代码?[`docs/architecture.zh-CN.md`](docs/architecture.zh-CN.md) 是 as-built 地图:
流水线以及"为什么阶段顺序是承重的"、包划分、不变量及各自的强制执行点,还有一份对"哪些还没做"的
诚实交代。

## 许可证

[MIT](LICENSE)

## 参与开发

见 [CONTRIBUTING.md](CONTRIBUTING.md)(英文):一个改动一个分支,对 `main` 开 pull request,请人 review 前 `make verify` 要绿。
`make hooks` 装的 pre-commit 会用 aguard 自己扫你暂存的 skill。
