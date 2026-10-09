---
paths:
  - "internal/collect/**"
  - "internal/detect/contenthash*.go"
---
<!-- SPDX-License-Identifier: MIT -->
## Canonical 哈希(`internal/collect/hash.go`、`internal/detect/contenthash.go`)

skill 与 plugin 用树哈希(按相对路径排序 + 每个文件的 sha256);单文件用 sha256。它是信誉库的 key,所以必须
跨机器、跨 checkout 稳定 —— 这正是 `collect.ExcludeFromHash`(`.git`、`node_modules`、`dist` …)
要同时从**哈希遍历和扫描遍历**中排除的原因。**改动哈希逻辑会让
[internal/reputation/data/reputation.json](../../internal/reputation/data/reputation.json)
里的每一条记录全部失效**,以及**闸门里每一条已存的用户批准**(`internal/gate` 也按这个哈希建索引),
须用 `aguard hash` 重新生成。`TestHashGolden` 用两个字面常量钉住了当前定义 —— 现有的其他哈希测试
全都是"拿一个哈希和另一个同样算法算出来的哈希比",定义整体挪动时它们照样绿,只有字面常量会红。
`test/` 故意**不**排除 —— payload 会藏在那里。

**读文件必须流式,不能整读**(`sumFile`)。被哈希的字节是 artifact 作者选的,而 `os.ReadFile`
会照着那个文件的大小去申请内存 —— 往 skill 目录里丢一个多 GB 的文件,就能让扫描器 OOM 而不是给出
判决;**被扫对象能干掉扫描器的工具不是一道控制**。同一个问题有个更安静的孪生兄弟:`os.Open` 打开
一个 FIFO 会一直阻塞,所以 `sumFile` 先 `Stat` 掉非常规文件再开。**同一个守卫在内容侧是
`detect.regularFile`,而"上限由读强制"在内容侧是 `readCapped` 的 `io.LimitReader`** —— 这里修好了
不等于那边修好了:两条路各自开文件,而当年只修了这一条,于是一根管子照样能挂死 `scan`/`check`
和闸门,一个边读边长的文件照样能把 1 MiB 的帽子顶掉。`TestHashLargeFileStreams` 断言的是**分配的字节数**
而不是耗时或哈希值 —— 那才是退回整读时会红的那条性质。

### hook、MCP、permission 的内容哈希(`detect.ContentHashes`,P-009)

这三类在 collect 里是 `""`,而每个消费方都把 `""` 读成"没人审过"(`Approved`、`Match` 恒 false)。它们的哈希在
**detect 阶段**算(`Redact` 和脚本跟进在那里,collect 不能 import detect),由 `analyze()` 在 `Run` 之后**紧接着**填,
先于信誉、闸门 `SessionStart`、`aguard approve`、Downloads;`aguard hash` 调同一个函数。定义:
`hex(sha256(<域> 0x00 <规范 JSON>))`,域是 `aguard:hook:v1` / `aguard:mcp:v1` / `aguard:permission:v1` /
`aguard:settings-env:v1`。`TestContentHashGolden` 钉五个字面常量,**规范输入字节也钉**,可以 `printf … | shasum` 手算。

- **不进哈希的**:任何路径(配置文件、`OwnerRoot`、脚本解析后的位置)、artifact 名(`#n`、插件后缀、server 名都是标签)。
  同一份配置在两台机器上必须同一个身份,否则信誉条目只在录入那台机器上命中。root 先 `filepath.Abs`(尾部斜杠、`.` 都不能
  给出第二个身份)。
- **hook 和 MCP 哈希的是整个条目**:MCP 是 `mcpServers.<key>` 的对象,hook 是 collect 原样留下的 `Hook.Entry` 加 event/matcher
  —— 换 type、加 timeout、改 header 都是另一条 hook。只取 command/url 那四个字段时,`{"type":"prompt","command":"true"}`
  和 `{"command":"true"}` 同哈希(评审实测)。
- **hook 含它跟进的脚本**:与 `hookUnits` 同一套解析,读到 → `sha256:<FileHash>`;读不到按原因记 `unresolved` /
  `outside-home` / `unreadable` 三个**不同**的标记(与 `TreeHash` 的 `unreadableMark` 同一个理由:"没有 X"和"X 读不了"
  不能同 key)。HOME 外的脚本**不为哈希去读**(不变量 #2)。permission 同样带上 allow 条目引用的脚本 —— 那个 artifact 上的
  发现一部分就来自它们。
- **secret 先换掉再哈希,换的是 `Redact` 的凭据那一半(`redactCredentials`),不是全部。** 哈希会印进 JSON、存进
  approvals,低熵 secret 的摘要可以暴力还原(W-006 那条:任何 Hash 都不能是凭据的摘要)。高熵兜底**故意不用**:摘要泄露
  不了高熵串,而兜底会把 `echo <base64> | base64 -d | sh` 的载荷换成 `<REDACTED>` —— 换载荷哈希不变。
  **替换只许忘掉 secret,不许拿走结构**(`guardedView`):一次替换要抹掉结构字符就整个值原样进哈希。结构按读它的东西定 ——
  shell 值(hook command、MCP `command`/`args`、permission 条目 `Tool(…)` 括号里的模式)是 `` $`();|&<>\*?#[]{} ``,
  其余值是 URL 分隔符 `#?\`。凭据正则的值字符类(`[^\s'"]+`、`[^@/\s]+`)吞得下这些:`Bash(curl -u admin:*)` 与
  `…admin:hunter2)` 同哈希、`https://other.example:443#@good.example/` 与带密码的同哈希(都是评审实测),守卫之后都重键。
  **后果,有意为之:只改一个被换掉的 secret 不重键**;反过来,**改 `redactCredentials` 会让这三类全部重键**(多问一次,
  不会静默放行),改高熵兜底不会。动 `redact.go` 前先想清楚是哪一半。
- **剩下的口子要说实话**:被换掉的那一段如果本身被拿去解码或求值(配置在批准时就在"解码一个凭据名变量再执行"),换掉
  那段载荷哈希不变。`Redact` 认不出的 secret(`MYSQL_PASS=…`、`--db-password …`、`-p<pw>`)原样进摘要输入 —— 补它要改
  `redactCredentials`,即重键,另开。MCP 的哈希只覆盖配置条目,不覆盖 server 的代码。
- **规范 JSON 不是 RFC 8785**:`UseNumber` 保留数字原文、键按 UTF-8 字节排序、`SetEscapeHTML(false)`。谁要在别处重算这个哈希,
  照这里的定义,不照 JCS。
- **parse 失败的 artifact 保持 `""`**(`TestContentHash_ParseErrorArtifactsStayUnhashed`):它没被读过。
- **`TreeHash`/`FileHash` 不因此动一个字节**;`TestScan_OnlyConfigHashesChange` 把这一步关掉/打开各扫一次,三类的 `hash`
  置空后 JSON 逐字节相同。

