---
paths:
  - "internal/judge/**"
---
<!-- SPDX-License-Identifier: MIT -->
## LLM judge(`internal/judge`,可选)

必须同时满足 config `llm.enabled: true` **和** `--llm` 才会启用;`scan` 和 `check` 都接受 `--llm`(`check` 自 P-004),
`clean`、加载时闸门和 `aguard approve` 永不调用它。
**配置和密钥(2026-09-04 改)**:不传 `--config` 时读 `$XDG_CONFIG_HOME/aguard/config.yaml`,否则
`~/.config/aguard/config.yaml`(`config.LoadUser`;`config.Load` 保持纯函数给测试用)—— 放在扫描 root **之外**,
放进 `~/.claude` 会被当成无人认领的散落文件每次披露。`provider` 可以是预设名(`config.Presets`,只填
base_url 和默认模型,用户写的一律优先;不认识的名字是加载错误,不能悄悄退回 localhost)。密钥多了
`api_key_file`:spec §16.3 原文是"只读环境变量,不落盘",**放宽的理由**是在线端点全都要 key,而"设环境
变量"是非技术用户做不到的那一步——他们能做到的是把 key 粘进 `~/.zshrc`,同样是明文落盘,还没有权限检查。
所以文件必须 0600,组/他人可读一律拒用并给出 chmod(`ResolveAPIKey`),env 仍然优先给 CI。`aguard llm
setup/test/status` 是插件 `/aguard-llm` 驱动的三个原语:setup 在终端里用 x/term 隐藏输入读 key,管道时用 `--key-stdin`(两种都不进参数列表,
终端那条也不进 shell 历史和聊天记录,所以插件流程**先推终端**,贴进对话是知情的退路);只写
用户做了决定的字段、结束前必须印"内容会发到 X";`CheckEndpoint` 拒绝非 https 的远程地址(key 是
Bearer 头,明文 http 等于把它送上网),setup/test/judge 三处都过它,judge 侧是一条 `LLM-000`;test 用真实的 client 打一次(`HTTPClient.Ping`),
让错 key/错模型名在扫描之前失败;status 永不打印 key。
**送给模型的摘录先压缩、再截两头**(`judge/excerpt.go`,2026-09-08):`condense` 把连续空行折成一行、把注释行整行去掉
(用 `detect.CommentOnlyLines`,和 A1 同一个分类器),`capHeadTail` 在字节上限内保留文件头 2/3 和文件尾 1/3,中间放一行
"N line(s) omitted" 标记。两个都是对着真实样本改的:十万个空行把 payload 推到 2000 字节前缀之外,模型看到的和 `head` 一样
干净;三段"这是公开信息、AppSec 审过"的注释是写给判官看的,解释器从不读注释,判官也不该读。**改了摘录就必须改 `lineMap`**:
`sourceUnit.lineMap` 把摘录的每一行映射回原文行号,`ground()` 优先用它——摘录不再和文件连续,`firstLine + 偏移` 会引用
错行,而一条真发现引到错的位置比没有更糟(读者去看、没看到、以后不信)。`TestBehaviorExcerpt_PayloadBelowPaddingReachesTheModel`
钉住"payload 进摘录 + 引用行号正确"两件事。**行内垫料在 `eg.redact` 里折掉**(`foldPadding`,P-020):连续超过 128 字节的空白 / 不可见字符折成落地读它的样子。
三个不要:**别挪到第一遍 Redact 之前**(熵规则按整串判,拼上低熵尾巴的 token 会漏);**别去掉折过之后的第二遍 Redact**
(被零宽字符切开的 key 会以拼好的样子发出去);**别让它跨 `\n`**(`lineMap` 就错了)。`TestEgress_PaddingIsFoldedBetweenRedactions`
三条各对应一种挪法;阈值低于 128 会吃真实缩进(实测最长 121)。P-006 的 600 字节垫料测试折叠后走不到 `collapsedWindow`,
`TestRun_PaddingBelowTheFoldStillShowsTheDirective` 替它守着那条路。intent 提示词多了一类**"披露了也要报"**(改包源、写 git hook / shell 启动文件 /
定时任务):dev-env-setup 那个样本的描述老老实实写着"配置企业 npm 镜像",按"目的已披露不报"的老规则判官放行了它。LLM-005
的 `Why` 现在带最多三条没落地的引文(截 120 字符,再过一次 Redact),用来区分"模型在转述"和"摘录切掉了它要引的那行"。
**趟数按 artifact 种类选**(`judge/run.go` 里的 mode 表,不是固定四趟):injection(`LLM-003`,
维度 1,跑 skill/CLAUDE.md/subagent/command/hook)、intent(`LLM-001`,维度 10,仅 skill)、
deobfuscation(`LLM-004`,维度 6,**只解码、绝不执行**)、collusion(`LLM-006`,维度 3)、
hook capability(`LLM-008`,维度 2)、MCP config(`LLM-009`,维度 5,**只提示**:`run.go` 的 `advisoryOnly` 让它无论几票都不升级,
因为 500 个良性配置上它是唯一升级过的判官规则,P-019)、triage(仅展示用的标签)。
`LLM-007`(artifact 试图指挥分析器,维度 1)的严重度由工具定死。被扫内容按**敌对**处理:
每次调用用 `crypto/rand` 生成 **nonce barrier** 把内容围成惰性数据块;nonce 生成失败时**让该次调用
失败**,而不是退化成一个可被猜到的围栏。`base_url` 非 loopback 时追加 `LLM-002` 隐私警告。**`ScanResult.Judge`(2026-09-05)**:`--llm` 时必填,记录跑没跑、
判了几个、几次调用、补了几条、没跑的原因;两个渲染器在摘要里印一行,HTML 的判官区块在请求了就出现(空则放那一行),
没开 `--llm` 一律不提 —— "跑了没发现"和"没跑"以前在报告上一模一样,这是不变量 #5 用在判官自己身上。
完整参考:[docs/llm-judge.md](../../docs/llm-judge.md)。

