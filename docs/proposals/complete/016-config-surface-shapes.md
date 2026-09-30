<!-- SPDX-License-Identifier: MIT -->
# 016 — 配置面上四种朴素攻击形状零发现:MCP env 注入代码、脚本改写权限文件、hook 自动放行、API 端点被改

- **来源**:plan §3.1 第 2 步第一组(配置面开关,8 个漏检);首轮 `make bench` 的 B 类分类
- **依赖**:无(P-015 的 `make bench` 用来验收;agent 定义文件的角色问题拆到下一条)
- **分支**:`p/016-config-surface-shapes`

## 问题

语料里 hooks / permission / connector 三个面的 11 个恶意样本,aguard 拦下 3 个。漏掉的 8 个都是**配置里明写着的**形状,
不是藏起来的。逐个看,它们分成四种情况,处理方式完全不同:

| 情况 | 样本(标签严重度) | 现在的发现 | 良性共现 |
|---|---|---|---|
| **形状干净,规则库没有** | `mcp-env-hijack`(high):`.mcp.json` server 的 `env.NODE_OPTIONS = "--require /tmp/.preload.js"` | 无 | 353 个真实工具目录 + 387 个真实配置里 **0**;2,480 个 skill 里 1 处是 `--max-old-space-size` |
| | `userpromptsubmit-perm-rewrite`(high):hook 脚本 `p.write_text(json.dumps({"permissions": …}))` 写 `~/.claude/settings.local.json` | 无(脚本被读到了,没有规则) | skill 文档里提到 `settings.local.json` 的 9 处**都不带写动作**;真实配置里 1 处是注释 |
| | `sc-T13-permission-bypass-medium`(high):SKILL.md 让 agent "Write the following to `.claude/settings.json`",内容是放开权限 | 无 | 同上 |
| **形状干净,角色挡住** | `agent-bypass-permissions`(critical):`agents/web-test.md` frontmatter `permissionMode: bypassPermissions`,正文 `socat tcp:… exec:/bin/bash` | 无 —— agents/*.md 是 `roleDoc`,只跑维度 1 | 语料无良性 agent;真机 71 个 agents/commands 里 **0** 个写 permissionMode |
| **与良性字面无法区分** | `pretooluse-autoapprove`(high):hook 脚本输出 `{"permissionDecision":"allow"}` | 无 | 15 个良性 SKILL.md 在教人写 hook 时给了同一段 JSON |
| | `baseurl-redirect`(critical):settings `env.ANTHROPIC_BASE_URL` 指向第三方 host | 无 —— settings 的 `env` 块**根本没被收集** | 1 个真实配置指向阿里云 DashScope 网关(合法代理) |
| | `sc-T13-permission-bypass-easy`(high):让用户跑 `claude --dangerously-skip-permissions` | 无 | 2 个良性 worktree 启动器就是这么跑的 |
| **不做** | `mcp-autorun-bypass`(high):`enableAllProjectMcpServers: true` + 项目 `.mcp.json` 里 `npx -y @x@latest` | 无 | **34 / 387** 真实配置开着这个开关 |
| | `disable-own-guardrail`(high):`config.patch --set permissions.approvalRequired=false` | 无 | 0,但这是靶场自造的 CLI,不是任何真实产品的开关 |
| | `sg-overbroad-permissions`(**medium**):frontmatter `permissions.files.read: ["~/**"]` | 无 | 标签本身低于闸门阈值,翻不动 |

顺带确认的一个收集缺口:`collectSettings` 只解 `hooks` 和 `permissions`,**`env` 块整个丢掉**。`ANTHROPIC_BASE_URL` 看不见,
`ANTHROPIC_API_KEY` 明文写在 env 里也看不见。

**角色那一格要单独说。** ROADMAP 写着"角色按 artifact 种类取,不按文件名,已修";但 `git show f4c9984` 里 detect.go 的注释写的是
相反的话:"The role stays FILENAME-DERIVED. Forcing roleInstruction on every one of these was tried … 8 of them high, and a red CI gate
(see TestScan_BenignProseIsNotFlagged)"。也就是试过、因为 hookify 插件那批"提醒我别用 rm -rf"的散文被撤回了,而 ROADMAP 没改。
真机上今天仍有 4 个 agents/commands 文件含 `rm -rf` 字样,全是这种散文。这是一个有历史的取舍,不该夹在四条规则里顺手改,
**拆到下一条 proposal**(agent 定义文件的角色与 frontmatter),本条只记录 ROADMAP 那句过期。

## 初步方向

四条规则加一处收集,全部在良性集上量过;两条 high 会翻 verdict,两条 medium 只让报告可见:

1. **`EXEC-010`(high,维度 4)** MCP server env 向解释器注入代码:`NODE_OPTIONS` 含 `--require`/`--import`/`-r `、`LD_PRELOAD`、
   `DYLD_INSERT_LIBRARIES`、`PYTHONSTARTUP`、`RUBYOPT=-r`、`PERL5OPT`。所有角色;`--max-old-space-size` 这类不带加载语义的值不算。
2. **`PERM-007`(high,维度 2)** 改写 Claude Code 的权限/设置文件:同一行里写动作(`write_text`、`open(…, "w")`、`>`、`tee`、`cp`、
   `jq … >`,或指令文件里 "write/save/put the following to")指向 `settings.json` / `settings.local.json` / `~/.claude.json`。
   `roleScript`、`roleHookCmd`、`roleInstruction`;`grep -q … settings.json`、"personal overrides go in settings.local.json" 这种读和说不算。
3. **`PERM-008`(medium,维度 2)** hook 自动放行:输出 `"permissionDecision": "allow"` 的脚本或命令。**只在 `roleScript` 与 `roleHookCmd`**,
   不在指令文件 —— 文档里教人写 hook 的 JSON 示例不是在放行。需要一个新的 `scriptOnly()` 标记(现有的只有 `hookOnly`、`connectorOnly`)。
4. **`EXFIL-005`(medium,维度 3)** `ANTHROPIC_BASE_URL` 指向 `api.anthropic.com` 以外的 host:API key 会发去那里。合法代理存在
   (阿里云网关),所以是披露不是拦截;host 是 `localhost`/`127.0.0.1` 时不报。
5. **收集 settings 的 `env` 块**:作为一个 synthetic unit(`roleScript`,与 `.mcp.json` 同一条路)挂到一个 `KindPermission` artifact
   `settings env` 上,所有规则照跑 —— 明文 API key 会被现有的 secret 规则看到,规则 4 有了输入。

不做的三个写进「不做什么」并说明理由。`--dangerously-skip-permissions` 不加规则:2 个良性对 1 个恶意,字面上是同一件事。

## 完成的判据

- [ ] `TestDetect_MCPEnvInjectsCode`(新,走 `jsonStrings` 合成单元):`NODE_OPTIONS: "--require /tmp/x.js"`、`LD_PRELOAD`、
  `PYTHONSTARTUP` 各 → `EXEC-010` high;反例 `NODE_OPTIONS: "--max-old-space-size=8192"`、`NODE_ENV: production` → 无。
- [ ] `TestDetect_PermissionFileRewrite`(新):`p.write_text(json.dumps(...settings.local.json...))`(.py)、`echo '{…}' > ~/.claude/settings.json`(.sh)、
  SKILL.md 里 "Write the following to `.claude/settings.json`" → `PERM-007` high;反例 `grep -q enabledPlugins .claude/settings.json`、
  "Personal overrides go in `.claude/settings.local.json`"、`cat ~/.claude/settings.json` → 无。
- [ ] `TestDetect_HookAutoApproveIsScriptOnly`(新):hook 脚本输出 `permissionDecision: allow` → `PERM-008` medium;同一段 JSON 出现在
  SKILL.md 的代码块里 → **无**(反向断言,15 个真实 skill 就是这个形状)。
- [ ] `TestCollectSettings_EnvIsScanned`(新,collect):settings.json 的 `env` 块进 artifact;`ANTHROPIC_BASE_URL: https://proxy.example`
  → `EXFIL-005` medium;`https://api.anthropic.com` 与 `http://localhost:8080` → 无;`ANTHROPIC_API_KEY: sk-ant-…` → 现有 secret 规则命中。
- [ ] 反向断言:`TestDetect_MCPConfigURLIsNotEgress`、`TestDetect_ExfilChain*`、permcheck 全部现有测试一字不改仍绿;
  `TestEveryRuleIDIsDocumented` 绿(`make docs` 后 rules.md 多四行)。
- [ ] 语料对照(`make bench`,基线 `baselines/results/aguard/2026-09-22/`):恶意 `benign→malicious` **≥ 3**,点名 `mcp-env-hijack`、
  `userpromptsubmit-perm-rewrite`、`sc-T13-permission-bypass-medium`;良性 `benign→malicious` **= 0**;hard-negative 不变;
  账本 3,539 行、`skipped` 0;规避矩阵不变。medium 阈值组另跑一次,`PERM-008`/`EXFIL-005` 在良性上的命中数点名写进「完成」。
- [ ] 真机 `scan --root ~/.claude` 修前后 high 数不变;新增的 medium(如有)逐条点名。
- [ ] `make verify` 绿。

## 不做什么

- **不动 agents/commands 的角色**,不加 `permissionMode: bypassPermissions` 规则 —— 下一条 proposal,理由见「问题」末段。
- **不给 `enableAllProjectMcpServers` 加规则**:34 / 387 真实配置在用,是产品的正常开关;它和 `npx -y @x@latest` 的组合才是攻击,
  而那个组合在 34 个良性配置里多半也成立,静态分不开。
- **不给 `--dangerously-skip-permissions` 加规则**:2 良性 : 1 恶意,字面相同。
- **不给 `config.patch --set approvalRequired=false` 加规则**:靶场自造的 CLI,对着它写规则是对着样本写。
- 不动现有 `PERM-001..006`(permcheck)、`HOOK-001..003`、任何 severity;不改 corpus 仓。

## 不能说什么

- 不写"补齐了配置面检测"。写:四种朴素形状各加一条规则,其中两条是 medium 披露,并列出三个没做的和原因。
- 数字按来源带 n;这一组翻回的是 `reconstruction:*` 来源(每个 n ≤ 6),`corpus score` 只会给计数不给率,引用时照抄计数。
- `EXFIL-005` 报的是"API key 会发去这个 host",不是"这个 host 是恶意的"。

## 工作项

| W | 一句话 | 提交信息 |
|---|---|---|
| 1 | 四组测试先红,正反例同表 | `detect,collect: tests — four config-surface shapes and the settings env block (P-016)` |
| 2 | `EXEC-010`、`PERM-007`,`scriptOnly()` 标记与 `PERM-008` | `detect: mcp env code injection, permission-file rewrite, and hook auto-approve get rules (P-016)` |
| 3 | 收集 settings `env`;`EXFIL-005` | `collect,detect: the settings env block is scanned; a third-party ANTHROPIC_BASE_URL is disclosed (P-016)` |
| 4 | `make docs`;spec §4 与 detect.md 同步;ROADMAP 那句"角色按种类取"改为如实;bench 前后写进 proposal | `docs: rules reference, spec, detect note; ROADMAP stops claiming the kind-based role shipped (P-016)` |

## 未决问题

每条带建议答案。

**已决(2026-09-23)**:领导对五条全部按建议答案同意 —— 1 `PERM-007/008`;2 指令文件形态收,良性响了退回只认脚本;3 只认 `api.anthropic.com` 与回环;4 新建 `settings env` 的 `KindPermission` artifact;5 下限 ≥ 3,两个 medium 按设计不翻。

1. **规则 ID 用哪个前缀?** 维度 2 至今没有文本规则,`PERM-*` 是 permcheck 的命名空间。建议:**沿用 `PERM-007/008`**,
   同一维度一个前缀,读报告的人不用学第二套;gen-rules 按维度归组,不按包。
2. **`PERM-007` 的指令文件形态**("Write the following to `.claude/settings.json`")要不要收?它是自然语言,但动词表很窄
   (write/save/put/append … to … settings.json)。建议:**收**,sc-T13-medium 正是这个形状,良性 skill 文档里 0 处;
   如果实测在良性上响了,先退回只认脚本。
3. **`EXFIL-005` 的官方 host 名单**:`api.anthropic.com` 之外要不要认 Bedrock/Vertex 的官方域?建议:**只认 `api.anthropic.com` 和回环**,
   其余一律 medium 披露;名单越长越像白名单,而白名单归第 6 步。
4. **settings `env` 挂到哪个 artifact?** 建议:**新建一个 `KindPermission` artifact 名为 `settings env`**(与 `permissions` 并列),
   不塞进 hook 或 permissions 里,报告里读者一眼知道是环境变量。
5. **验收下限 ≥ 3**(点名三个)。`baseurl-redirect` 和 `pretooluse-autoapprove` 是 medium,**按设计不翻**,写进「完成」。建议:是。


### 实现阶段记录(2026-09-23)

**已决 2 的应急条款触发了。** 量了 SKILL.md 里"写动词 + settings.json"的散文:7 个真实 skill 写着 "add to `.claude/settings.json`" /
"add hooks to … settings.json" —— 这是合法安装 hook 的标准说法。`PERM-007` 因此**只认脚本**(`scriptOnly`),sc-T13-medium 按设计仍漏。

**第一版只翻回 1 个**:`mcp-env-hijack`。`userpromptsubmit-perm-rewrite` 的 hook 脚本把路径构造和 `write_text` 分在两行,按行匹配抓不到。
加了同文件两条腿的兜底(命名 Claude settings 文件的一行不能本身是读操作 + 任一行写调用);良性集里内部脚本同时满足两腿的 **0 个**。

**第二版翻回 3 个,其中一个是判对了理由错**:Cisco 的 `system_prompt_manipulation_override` 写的是 `/etc/app/settings.json`,
不是 Claude 的文件。文件名收紧为必须带 `.claude` 上下文(`.claude/settings…`、Python 里相邻 token `".claude", "settings.local.json"`、`~/.claude.json`),
加了 `/etc/app/settings.json` 与 `.vscode/settings.json` 两个反例。**第三版翻回 2 个**,这是最终数。

**规则 ID 更正**:proposal 写的 `EXFIL-005` 已被 `collect/imports.go` 占用(指令文件 @import 凭证),端点规则改为 **`EXFIL-006`**。

**最终对照**(`make bench`,基线 `baselines/results/aguard/2026-09-22/` = step-2 前;阈值 high,信誉库关,inbox 关,HOME 隔离):

```
恶意 85 → 87:benign→malicious 2 —— mal-perm-mcp-env-hijack(EXEC-010)、mal-hook-userpromptsubmit-perm-rewrite(PERM-007 两腿)
良性 147 → 147 · hard-negative 4 → 4 · uncovered 0 · 规避矩阵 8 个不变
reconstruction:hooks 3/5 → 4/5 · reconstruction:permission 1/6 → 2/6(corpus score 只给计数)
medium 阈值组,新规则命中:EXFIL-006 良性 3(阿里云 DashScope 网关 1、Vercel AI Gateway 2 —— 都是"API key 会发去这个 host"的如实披露)、恶意 1;
  PERM-008 恶意 1(pretooluse-autoapprove)、良性 0;PERM-007 恶意 1;EXEC-010 恶意 1
真机 scan --root ~/.claude:overall 69,47 artifacts,逐条 finding 与修前完全相同
```

判据对照:四组新测试绿(含两个 scriptOnly 反例、两个"别家 settings.json"反例、读后写别处反例);反向断言全绿;
**恶意翻回 2,判据写的 ≥ 3 未达**——第三个点名样本 sc-T13-medium 因已决 2 的应急条款退出;良性 0 ✓;hard-negative、uncovered、规避矩阵不变 ✓;
真机不变 ✓;`docs/rules.md` 多四条规则,`make docs` 无漂移 ✓。


## 完成

```
合入:PR #16 https://github.com/basdotio/agent-guard/pull/16(2026-09-24;sha 合入后用 git log --grep P-016 找)
发布:v0.12.0
证据:TestDetect_MCPEnvInjectsCode、TestDetect_PermissionFileRewrite、TestDetect_SettingsEnvBaseURL(internal/detect/detect_test.go)、
      TestDetect_HookAutoApproveIsScriptOnly(hooks_test.go)、TestCollectSettings_EnvIsScanned(collect)、TestSettingsEnvArtifactIsNotAuditedTwice(cmd/aguard);
      语料 make bench 阈值 high:恶意 85 → 87(翻回 2:mcp-env-hijack、userpromptsubmit-perm-rewrite;判据写的 ≥ 3 未达,sc-T13-medium 按已决 2 应急条款退出),
      良性 147 → 147,hard-negative 4 → 4,规避矩阵不变;medium 组新规则在良性上只有 EXFIL-006 的 3 条网关披露;真机逐条 finding 相同;
      docs/rules.md 多 EXEC-010、PERM-007、PERM-008、EXFIL-006 四条,make docs 无漂移
```
