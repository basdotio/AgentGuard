<!-- SPDX-License-Identifier: MIT -->
# 018 — 导入行、插件名、树内条目名里的 token 经几条笔记原样进报告:那几处证据片段没经过脱敏

- **来源**:P-014(`docs/proposals/complete/014-hook-outside-snippet-redacted.md`)的「不做什么」与未决 3 点名、留给后续的那几处:
  collect 的笔记(`imports.go` 四条 note 的 `"@" + ref`、插件名、`err.Error()`、条目列表、`connectors.go`/`unowned.go`/`loaded.go` 的文件名)、
  `detect.unreadableNote`、`internal/gate/status.go` 的 `missing hook command: <cmd>`
- **依赖**:无
- **分支**:`p/018-collect-notes-redacted`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

不变量 #3 说"`detect.Redact` 是产出 snippet 的唯一途径"。有几处笔记把从文件正文或配置值里抄出来的字符串直接拼进证据片段或 `Why`,
一个字节都没过 `Redact`:

- `internal/collect/imports.go`:`EXFIL-005` 和三条 `COV-000`(凭据路径拒读、越出扫描边界、超过导入深度)的 snippet 都是 `"@" + ref`,
  `ref` 是指令文件里那行 `@…` 的原文
- `internal/collect/plugins.go`:`SCOPE-001`(插件安装路径越出 HOME)的 snippet 是 `"install path escapes HOME: " + name`,`name` 是
  `installed_plugins.json` 里的键
- `internal/collect/hooks.go`:`PARSE-000`(hook 条目读不懂)的 snippet 是 `"hooks." + event`,`event` 是 settings.json 里 `hooks` 下的键
- `internal/detect/detect.go` 的 `unreadableNote`:读不了的条目名原样进 `Why` 和 snippet;同一个文件里同形的 `nonRegularNote`、
  `skippedDirNote` 都是 `redactClip(list)`
- `internal/gate/status.go` 的 `DeadRegistrationNote`(`GATE-001`,`scan` 每次都会挂上):snippet 是 `"missing hook command: " + cmd`,
  `cmd` 是 settings.json 里注册的命令原文

复现(本仓 `main` fd28344 构建的二进制,fixture 在 `/tmp` 下,`HOME` 指向 fixture 里的 home,token 用 `ghp_` 加 36 位的明显假值;
`CLAUDE.md` 写四行 `@` 导入,分别指向 `~/vault/<token>/.env`、`~/.ssh/<token>/config`、HOME 外的 `…/<token>/notes.md`、一条第五跳落在
`d/<token>/d5.md` 的导入链;一个 skill 里放一个 `0111` 的目录 `<token>/`;`installed_plugins.json` 里一个 `<token>@market` 插件装在 HOME 外;
settings.json 里一个键为 `<token>` 的坏 hook 条目,和一条指向不存在的 `…/<token>/aguard hook` 的闸门注册;`scan --json --inbox off`):

- JSON 里 token 出现 **11 次**,逐条:`EXFIL-005` ×2、凭据路径 `COV-000` ×2、越界 `COV-000`、深度 `COV-000` 的 snippet 各带一份;
  `unreadableNote` 的 `Why` 和 snippet 各一份;`PARSE-000`、`SCOPE-001`、`GATE-001` 的 snippet 各一份
- `scan --md -`、`scan --verbose` 也是 11 次;默认终端报告 2 次
- `check <skill 目录> --md -`(写来贴 PR 评论的那条路)2 次 —— `unreadableNote` 的两份
- 同一份 fixture 里,被引擎规则命中的行、`HOOK-002` 的引用,token 都是 `<REDACTED>`:泄漏只在这几条笔记上

为什么一直没修:collect 不能 import detect(detect 依赖 collect),而 `Redact` 的实现在 detect 里,collect 想用也用不上;P-014 因此把
collect 那半留给了本条(其未决 3)。

后果:用户把 token 放进了路径(目录名、导入行、插件键),工具在规则命中的行里替他抹掉,在这几条笔记里替他原样印出来;报告越是被转贴
(PR 评论、SARIF 上传),这几处越不该是例外。

## 初步方向

把 `Redact` 的实现原样挪进一个 collect 和 detect 都能 import 的叶子包,`detect.Redact` 只委托、行为一字不改(现有脱敏测试不改一字仍绿);
上面五类笔记里**从文件正文或配置值抄来的那一段**先过它。逐处量:哪些位置的字符串同时也是该发现的 `Evidence.File`(那是 P-014 留下的
全引擎问题,不在本条)、哪些是应用自己生成的标识符(`Redact` 会把真实值全部抹掉),这些不动并写明理由。

## 逐处量过的划线

一条规则,三句话,按**字符串落在发现的哪个字段**划,而不是按它从哪来:

- (a) **进 `Evidence.Snippet` 的、不是工具自己写的文本**(从文件正文或配置值抄来的一段)一律过 `Redact` —— 不变量 #3 的原文;
- (b) 同一段文本**也进了 `Why`** 的,`Why` 里印同一份脱敏后的字节 —— 不许一半脱一半不脱(P-014 的 `SUP-006` 先例);
- (c) **只进 `Why` 的名字列表、以及就是该发现 `Evidence.File` 的路径**不在本条:那是 P-014 未决 2 留下的"`Evidence.File` 要不要脱敏"的全引擎问题,
  在这里替其中几处分,就是在 `Redact` 之外长出第二套判断。

真机量的是"`Redact` 会不会改动这处今天的真实值"(本机 `~/.claude` 与桌面版仓库,只计数,不印内容;合成值是常见安装形状):

| 位置 | 落在哪 | 真机量 | 结论 |
|---|---|---|---|
| `imports.go` 四条 note 的 `"@" + ref` | snippet | 本机 `~/work` 下 CLAUDE.md/AGENTS.md 里 18 个 `@` 引用,`Redact` 改 0 个;合成 8 个改 2 个(带数字的长路径) | 改(a) |
| `plugins.go` `SCOPE-001` 的插件键 | snippet | `installed_plugins.json` 8 个键改 0 个 | 改(a) |
| `hooks.go` `PARSE-000` 的 `hooks.<键>` | snippet | 本机唯一一条就是这条,`hooks.hooks`,不变;11 个标准事件名改 0 个 | 改(a)(同类排查找到的) |
| `detect.unreadableNote` 的条目列表 | snippet + Why | 本机 skill 树内(跳过扫描排除目录)1948 个树内相对路径改 80 个(4.1%,`browse/test/pair-agent-e2e.test.ts` 这类带数字的长路径被熵检测吃成 `<REDACTED>.test.ts`) | 改(a)(b),与 `nonRegularNote`/`skippedDirNote` 同形 |
| `gate/status.go` `GATE-001` 的命令 | snippet | 本机没有注册闸门;合成 11 种安装路径改 4 种(npm 全局、npx 缓存、带数字的 checkout、mise 的 go 版本目录) | 改(a);代价见未决 6 |
| `gate` `Describe`(`aguard hook status` 的终端输出) | 不是发现 | 同上 4/11;同一屏的 `this binary:` 行原样印同一条路径 | 不改,未决 5 |
| `collect.go` `unresolvedNote` 的条目列表 | 只在 Why(snippet 是固定串) | `skills/` 46 个条目改 0 个、插件键 0/8;桌面版仓库的条目是应用生成的 ID(`plugin_<id>` 3/3、账号 UUID 2/2 被改) | 不改(c) |
| `connectors.go` 会话文件名 | 只在 Why | 13 个 `local_<uuid>.json` 全被改成 `<REDACTED>.json` | 不改(c) |
| `unowned.go` 的条目名 | Why + 每个名字就是一条 `Evidence.File` | 顶层 30 个名字改 0 个 | 不改(c) |
| `collect.go` `ioNote` 的 `err.Error()` | snippet,但其中的路径就是该发现的 `Evidence.File` | 所有调用点的错误都是 `os.PathError` 或 `safeio` 的 `"<path>: …"`,路径 = `Evidence.File` | 不改(c):脱了 snippet,File 里照样是原文 |
| `loaded.go` 的 note | — | `Why`/snippet 里只有固定串、计数和 `TrashDir` 常量,路径只在 File | 无可改 |

## 完成的判据

fixture 都在 `t.TempDir()` 现搭;token 用 `ghp_` 加 36 位的明显假值(`redact_test.go` 已有同样写法),`Redact` 的已知前缀表认得它。

- [x] `TestImportNotes_SecretInReferenceIsRedacted`(`internal/collect/notes_redact_test.go`,新):`CLAUDE.md` 四行导入
  (`@~/vault/<token>/.env`、`@~/.ssh/<token>/config`、`@../../outside/<token>/notes.md`、第五跳 `@<token>/d5.md`)走 `CollectAll`,
  `EXFIL-005` ×2、凭据路径 / 越界 / 深度三种 `COV-000` 的 Title、Why、Evidence 里都没有 token,snippet 带 `<REDACTED>`、仍以 `@` 开头、
  仍以原来的尾巴结尾(`/.env is a credential path`、`/notes.md escapes the scan boundary`、`/d5.md beyond depth 4`…)。今天红
- [x] `TestConfigNamesInNotesAreRedacted`(同文件,新):`<token>@market` 插件装在 HOME 外 → `SCOPE-001`;settings.json `hooks` 下键为 `<token>`
  的坏条目 → `PARSE-000`:snippet 里没有 token、带 `<REDACTED>`、固定前缀不变。今天红
- [x] `TestUnreadableNote_SecretInEntryNameIsRedacted`(`internal/detect/note_redact_test.go`,新):skill 里 `0111` 的目录名为 token,走 `Engine.Run`,
  那条 `COV-000` 的 Why 和 snippet 里都没有 token、都带 `<REDACTED>`,且两处是同一份列表。今天红
- [x] `TestDeadRegistrationNote_SecretInCommandIsRedacted`(`internal/gate/status_redact_test.go`,新):注册命令 `/opt/<token>/aguard hook` 的
  `GATE-001` snippet 等于 `missing hook command: /opt/<REDACTED>/aguard hook`。今天红
- [x] `TestScan_NoteSecretsNeverReachARendering`(`cmd/aguard/note_redact_render_test.go`,新):上面全部放进一份 fixture 走 `scanEnv` 并像 `scan`
  命令那样挂上 `gateLivenessNote`,JSON(与 CLI 同样的缩进编码)、终端(普通与 `--verbose`)、markdown、SARIF、HTML 六种渲染里都没有 token,
  且 `EXFIL-005`、三种 import `COV-000`、`SCOPE-001`、`PARSE-000`、读不了条目的 `COV-000`、`GATE-001` 都还在;同一个 skill 走 `checkTarget` 的
  markdown(贴 PR 评论那条路)也没有 token。今天红
- [x] 反向断言:普通值**逐字不变** —— `TestImportNotes_OrdinaryReferenceUnchanged`、`TestConfigNamesInNotes_OrdinaryUnchanged`、
  `TestUnreadableNote_OrdinaryNamesUnchanged`、`TestDeadRegistrationNote_OrdinaryCommandUnchanged` 用字面值钉住(`@~/.env is a credential path`、
  `@../../outside/notes.md escapes the scan boundary`、`@h5.md beyond depth 4`、`install path escapes HOME: figma@claude-plugins-official`、
  `hooks.PreToolUse`、`unreadable: lib/helper.sh, sub`、超过 10 个名字的 `… (N more)`、`missing hook command: /usr/local/bin/aguard hook`、
  带空格加引号的 `"/Applications/Some Tool/aguard" hook`),并且每条都等于按今天的拼法算出的值;修前修后都绿
- [x] 反向断言:`aguard hook status` 仍按原样印注册的命令 —— `TestStatusDescribe_ShowsTheRegisteredCommandVerbatim`(同 gate 文件)用一条
  `Redact` 会改动的 npx 缓存形状路径,断言它逐字出现在 `Describe` 输出里;修前修后都绿(钉住未决 5)
- [x] 搬家不改行为:`internal/detect/redact_test.go`、`contenthash_test.go`、`snippet_redact_test.go`、`internal/judge/*_test.go`、
  `internal/permcheck/permcheck_test.go`、`cmd/aguard/redact_render_test.go` 一字不改仍绿(`git diff --stat origin/main` 对它们为空);
  挪走的模式、两遍函数和熵判定逐字节等于 `origin/main` 的 `internal/detect/redact.go` 里那一段(去掉包名与包注释后 `diff` 为空)
- [x] fixture 二进制前后(fd28344 vs 本分支,同一份 fixture,`--json --inbox off`):token 11 → 0,上面那几条发现与 note 条数不变,overall 不变
- [x] 真机 `scan --root ~/.claude`:修前修后 JSON 去掉 `scanned_at`、`tool_version` 后逐字节相同(本机唯一一条落在改动处的 note 是 `hooks.hooks`,
  `Redact` 不改它)
- [x] `.claude/rules/*.md` 都不超过 200 行;`make verify` 绿;`go version` 不切换工具链,`go.mod` 第二行仍是 `go 1.23.5`,不加依赖

## 不做什么

- **不改 `Redact` 匹配什么**:模式、熵检测字符类、阈值、两遍的先后,逐字节搬过去;不改 `redactClip`/`clip`;本条改到的笔记一处都不加截断
- **不改划线 (c) 的那几处**:`unresolvedNote` 与 `connectors.go` 只进 Why 的名字列表、`unowned.go` 的条目名、`ioNote` 的 `err.Error()`、
  所有 `Evidence.File`。理由见上表;它们是一个问题,要改就整体另开
- **不改 `aguard hook status`(`Describe`)的输出**(未决 5)
- **不改被跟进的导入 artifact 的名字** `"@" + ref`(`imports.go` 的 `artifact(...)` 那行):artifact 名是另一类(闸门、信誉库、报告都用它认东西),
  同属上面那个全引擎问题
- **不改 `hookOwnedNote`**(P-014 已决)、不改任何规则的严重度、维度、标题和固定的 Why 文案;`docs/rules.md` 不变
- **不动 canonical 哈希**(`internal/collect/hash.go`);内容哈希(`contenthash.go`)调用的凭据那一遍只换了位置,金样测试不改一字
- 不加依赖,`go.mod` 不动;新包只用标准库

## 不能说什么

- 不说"路径里的 token 不会再进报告":划线 (c) 的几处、artifact 名(含被跟进的 `@` 导入)、`hook status` 的输出都仍原样
- 不说 `Redact` 变强了:它认什么、认不出什么(`MYSQL_PASS=…` 这类)一字没变,仍是尽力而为
- 不说修后这几条笔记和以前一样好读:`Redact` 会误伤的普通名字(真机树内路径 4.1%、常见安装路径 4/11)在这几条里变成 `<REDACTED>`(未决 4、6)
- 不说 SARIF 指纹不变:snippet 被 `Redact` 改动的那些发现,`partialFingerprints` 会变;普通值的不变
- 不说真机上修了一处泄漏:本机落在改动处的 note 只有一条,`Redact` 不改它
- 不说判官收到过这些:collect 的笔记和 `GATE-001` 不进判官的请求;泄漏面是本机生成的报告,以及用户转贴、上传的副本

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 五个新测试文件(四个包)加反向断言,跑红 | `collect, detect, gate, cmd: tests — a token in an @import line, a plugin or hook key, an unreadable entry name or the gate's registered command reaches the report in clear (P-018)` |
| 2 | `Redact` 的实现原样搬进叶子包 `internal/redact`,`detect.Redact`/`redactCredentials` 只委托 | `redact: the redactor moves to a leaf package so collect can call the one implementation; detect delegates unchanged (P-018)` |
| 3 | collect:四条 import note、`SCOPE-001`、`PARSE-000` 拼进 snippet 的那段先过 `redact.Secrets` | `collect: import, plugin-install and hook-shape notes redact the text they copy from a file or a config key (P-018)` |
| 4 | detect:`unreadableNote` 的列表先过 `Redact`,Why 和 snippet 用同一份 | `detect: the unreadable-entries note redacts its list the way its non-regular sibling does (P-018)` |
| 5 | gate:`GATE-001` 的命令先过 `detect.Redact`;`Describe` 不动 | `gate: GATE-001 redacts the registered command it quotes; hook status still prints it as written (P-018)` |
| 6 | 文档:不变量 #3(`.claude/rules/invariants.md`、`docs/architecture*.md` 双语)、spec §16.3 与 §12、`hash.md` 里 `redact.go` 的位置 | `docs: invariant #3 — one redactor in internal/redact, detect delegates, collect and the gate call it (P-018)` |
| 7 | 本文件、索引 | `proposals: P-018 (P-018)` |

## 未决问题

1. **实现放哪:叶子包,还是由 detect 在合并 collect 笔记时统一过一遍?**(P-014 未决 3 给的两条路)
   **建议**:叶子包 `internal/redact`。统一过一遍要改 note 的产出路径(collect 的笔记经 `main.analyze`、`inbox`、`check` 几条路汇合,挂在 artifact
   上的 `EXFIL-005` 又走 `Engine.Run`),还会把工具自己写的 Why 散文整段喂给 `Redact` —— `looseAssignRE` 认"凭据键名 + 空格 + 12 个字符",
   英文散文就会被改(实测 `the secret configuration is here` → `the secret <REDACTED> is here`);叶子包只在拼接点调用,其余字节不经过它。
   **已决(2026-10-09)**:按建议。
2. **新包的 API 叫什么,`detect.Redact` 留不留?**
   **建议**:`redact.Secrets`(两遍)与 `redact.Credentials`(只凭据那一遍,内容哈希用);`detect.Redact`、`detect.redactCredentials` 留作一行委托,
   judge、permcheck、hygiene、clean 的十几处调用和 `redact_test.go` 不改一字。gate 调 `detect.Redact`:gate 经 report 早已传递依赖 detect,
   不新增依赖方向;只有 collect(在 detect 之下)直接调新包。"只有一个实现"由"模式和两遍函数只在 `internal/redact` 里出现"保证。
   **已决(2026-10-09)**:按建议。
3. **划线 (c) 的几处为什么不一起改?**
   **建议**:不改,写进不做什么,要改另开。它们与 `Evidence.File` 是同一个问题:名字是扫描器自己在磁盘上列出来的、或者就是那条发现的位置;
   只脱 Why 而 File 照印,是 P-014 修掉的"一半一半"倒过来;而桌面版仓库与会话缓存里的名字是应用生成的 ID,`Redact` 把真实值全部抹掉
   (13/13、3/3、2/2),等于为一个应用不会写出的形状删掉每一个真实值。
   **已决(2026-10-09)**:按建议。
4. **`unreadableNote` 要不要像 `nonRegularNote` 那样再截到 200 字节?**
   **建议**:不截,只 `Redact`。它今天不截,加截断会改普通的长列表(最多 10 个名字),判据要普通值逐字不变;只脱敏不截断不存在先后问题
   (不变量 #3 管的是两步都做时的顺序;P-014 未决 4 同理)。
   **已决(2026-10-09)**:按建议。
5. **`aguard hook status` 的输出要不要也脱敏?**
   **建议**:不。它不是发现、不进报告,是运维在自己的终端里问"注册的是哪条命令";`⚠ … THAT FILE DOES NOT EXIST` 那一行,路径就是答案,
   而 `Redact` 会把 4/11 种常见安装路径改掉,包括 `ephemeralExeWarning` 预言会失效的 npx 缓存路径;同一屏的 `this binary:` 行原样印同一条路径,
   只脱一行就是"一半一半"。会被转贴的那份(`scan` 报告里的 `GATE-001`)本条脱敏。
   **已决(2026-10-09)**:按建议。
6. **`GATE-001` 脱敏后,npx 缓存这类路径在报告里读不全,接受吗?**
   **建议**:接受。报告是会被转贴、上传的那份;`Why` 给的修法(`aguard hook install`)不需要旧路径;旧路径在 `aguard hook status` 里原样可查。
   替 `Redact` 分辨"安装路径"和"secret",就是第二个出口(P-014 未决 1 同理)。
   **已决(2026-10-09)**:按建议。

## 完成

```
合入:PR 待开(2026-10-09;sha 合入后用 git log --grep P-018 找)
发布:待发
证据:W1 在本仓 main(fd28344)上红,原因与判据一致 —— TestImportNotes_SecretInReferenceIsRedacted 六条 snippet 都是原样 token(如 @~/vault/ghp_<36 位>/.env is a credential path);TestConfigNamesInNotesAreRedacted 两条(install path escapes HOME: ghp_<36 位>@market、hooks.ghp_<36 位>);TestUnreadableNote_SecretInEntryNameIsRedacted 的 Why 与 snippet(unreadable: ghp_<36 位>);TestDeadRegistrationNote_SecretInCommandIsRedacted(missing hook command: /opt/ghp_<36 位>/aguard hook);TestScan_NoteSecretsNeverReachARendering 七个子测试全红 → W5 后全绿
证据:TestScan_NoteSecretsNeverReachARendering 逐步(token 次数,json/text/verbose/markdown/sarif/html/check --md):W1 11/2/11/11/4/3/2 → W2(只搬家)不变 → W3(collect)3/0/3/3/1/1/2 → W4(unreadableNote)1/0/1/1/0/0/0 → W5(GATE-001)全 0;八类发现与 note 的条数每一步都不变
证据:反向断言 TestImportNotes_OrdinaryReferenceUnchanged、TestConfigNamesInNotes_OrdinaryUnchanged、TestUnreadableNote_OrdinaryNamesUnchanged(三个子测试,Why 与旧公式逐字相等)、TestDeadRegistrationNote_OrdinaryCommandUnchanged(四种安装命令)、TestStatusDescribe_ShowsTheRegisteredCommandVerbatim(npx 缓存路径,先断言 Redact 会改它)W1 时就绿,修后不改一字仍绿
证据:搬家不改行为 —— W2 之后 internal/detect、judge、permcheck、hygiene、clean 全绿,只剩 W1 的那条红;git diff --stat origin/main -- internal/detect/redact_test.go internal/detect/contenthash_test.go internal/detect/contenthash.go internal/detect/snippet_redact_test.go internal/judge internal/permcheck internal/hygiene internal/clean cmd/aguard/redact_render_test.go 为空;origin/main 的 internal/detect/redact.go 第 18–213 行与 internal/redact/redact.go 对应段 diff:只差 Redact→Secrets、redactCredentials→Credentials 两个函数头及其注释,和留在 detect 的 redactClip;每个模式、熵判定、两遍函数体逐字节相同
证据:二进制前后(fd28344 vs 本分支,同一份 fixture,HOME 指向 fixture,scan --json --inbox off):token 11 → 0;JSON 299 行里只差 11 行,恰是那 11 处;各规则条数不变(EXFIL-005 2、COV-000 6、SCOPE-001 1、PARSE-000 1、GATE-001 1),overall 69 → 69。HOME 外那两条路径(/tmp/ag-scratch/018/…,带数字的长路径)整段被熵检测吃成 <REDACTED><REDACTED>/notes.md、<REDACTED><REDACTED>/aguard hook —— 未决 4、6 接受的代价
证据:真机 ~/.claude(带 Downloads):修前修后 overall 69 / artifact 180 / 发现 806 / note 10;JSON 去掉 scanned_at、tool_version 后 15730 行里只差 3 行,都是两个桌面版 connector artifact 的 path 和一条证据 file —— 会话缓存里"最新的那份工具清单"在两次运行之间换了文件(桌面版在本会话中持续写会话文件;connectors.go 本条未动),把这两个字段遮住后逐字节相同。再背靠背各跑一次(--inbox off):去掉 scanned_at、tool_version 后 15724 行逐字节相同
证据:不做什么 —— git diff --stat origin/main -- internal/detect/hooks.go internal/report internal/collect/hash.go internal/collect/connectors.go internal/collect/unowned.go internal/collect/loaded.go internal/collect/desktop.go docs/rules.md go.mod go.sum 为空;internal/collect/collect.go 只有包注释一个 hunk(unresolvedNote、ioNote 未动);internal/gate/status.go 三个 hunk:import、DeadRegistrationNote 的注释、snippet 那一行,Describe 未动;internal/detect/detect.go 两个 hunk 都在 unreadableNote
证据:.claude/rules/invariants.md 84 → 90 行、hash.md 62 → 63 行,detect.md 200 行未动;TestClaudeRulesAreScopedToExistingPaths 绿
证据:make verify: all gates passed;go version go1.23.5(无工具链切换),go.mod 第二行 go 1.23.5,无新依赖
```
