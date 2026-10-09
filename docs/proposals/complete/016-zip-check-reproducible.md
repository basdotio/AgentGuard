<!-- SPDX-License-Identifier: MIT -->
# 016 — 同一个 zip 查两遍,SARIF 不一样:随机解压目录名进了 uri、artifact 和指纹,Code Scanning 每跑一次开一批新告警

- **来源**:同一个 zip 查两遍,SARIF / JSON / 文本报告不一样:随机的解压目录名漏进 artifact 名、uri 和 `partialFingerprints`,
  于是 GitHub Code Scanning 每跑一次 CI 就开一批新告警;`aguard approve x.zip` 记下的是一个已经删掉的临时路径;
  root 形状的 zip 把共享的 `$TMPDIR` 当成 home 来读。移植自旧仓 agent-guard 的 P-048(私有仓)
- **依赖**:无
- **分支**:`p/016-zip-check-reproducible`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

`aguard check x.zip` 先把包解到 `os.MkdirTemp("", "aguard-inbox-")`(`internal/inbox/archive.go` `ExtractZip`),再把那个目录当
目标查(`cmd/aguard/main.go` `checkTarget`)。查完只把 `out.Root` 改回 zip 路径,其余字段**原样指向随机的临时目录**。
在 `main`(`dec64ca`,v0.18.0)上编出的二进制,同一个 zip 连跑两遍 `--sarif`,`diff` 出来:

| zip 形状 | SARIF 里每次都变的 | 原因 |
|---|---|---|
| `SKILL.md` 在包根(扁平) | `properties.artifact` = `skill:aguard-inbox-<随机>`;macOS 上还有 `uri` = `aguard-inbox-<随机>/install.sh` 和 `partialFingerprints["aguard/v1"]`(实测 3 行变) | artifact 名取解压目录的 base;macOS 的 `$TMPDIR` 在 `/var → /private/var` 软链下,`detect.relPath` 只解析了 root 的软链,`Rel` 失败,退回"最后两段",把随机目录名带进证据路径,指纹又由证据路径算 |
| `myskill/SKILL.md`(套一层目录) | `properties.artifact` = `directory:aguard-inbox-<随机>`(实测 1 行变) | 同上,artifact 名 |

后果:

- **GitHub Code Scanning 用 `partialFingerprints` 跨次匹配告警**。CI 里每次 check 同一个 zip,指纹全变 → 每次开一批新告警、
  旧的全部"已修复";审过、驳回过的告警下一次又冒出来。`sarif.go` 自己的注释说指纹"刻意不含行号,免得重开已经审过的告警"——
  这里重开的原因比行号还粗。
- **不只 SARIF**。同一个随机名还出现在:`--json` 的 artifact `name`/`path`、`locations` 的 "Config root"、扁平包的证据 `file`(macOS);
  终端报告的 "skill aguard-inbox-<随机> — curl piped to shell";`scan` 的 Downloads 一节 JSON 里的证据 `file`(同样走 `ExtractZip`)。
- **`aguard approve x.zip` 记下的名字和路径是一个已经删掉的临时目录**:实测批准库里 `name: aguard-inbox-1441416832`、
  `path: /var/folders/…/T/aguard-inbox-1441416832`,终端提示 ``Run `aguard check "/var/folders/…/T/aguard-inbox-1441416832"` `` ——
  照做必然 "no such file"。批准的**键**(内容树哈希)不受影响,两次相同。
- **同一个根因的另一面:结果依赖包外的文件**。包里有 `plugins/installed_plugins.json` 时,`CollectTarget` 把解压目录当 root 走
  `CollectAll`,`home` = 解压目录的父目录 = **共享的 `$TMPDIR`**。实测:在 `$TMPDIR/.claude.json` 里放一个 MCP server,
  `check root.zip` 就多出一个 `mcp:planted` artifact 和它的 `EXEC-001`,`locations` 里还列出 `$TMPDIR/.claude.json` 等三处——那些文件根本不在包里。
  Linux 上 `$TMPDIR` 通常是全机共享的 `/tmp`,同机任何用户都能往别人的 zip 检查里加发现。

## 初步方向

解压到**私有临时目录里一个以包文件名命名的子目录**(`<MkdirTemp>/x.zip/`,临时根先解析软链),于是 artifact 名 = 包名、
证据路径 = 包内路径、`home` = 一个只装着这个子目录的私有空目录;`checkTarget` 和 Downloads 那一路查完后把指向临时目录的
`path`/`locations` 改写成包路径。目录目标的输出一个字节不变。

## 设计

修在**数据**上,不修在 SARIF 写出器上:text / JSON / markdown / SARIF 读的是同一个 `model.ScanResult`,只改 `sarif.go` 的话
另外三个照漏。两处改动:

1. **`inbox.ExtractZip`**:`os.MkdirTemp("", "aguard-inbox-")` 先 `filepath.EvalSymlinks`,在里面建 `filepath.Base(包路径)` 这个子目录
   (0700),条目解到子目录里;返回子目录,`cleanup` 删整个临时根。三件事一起变好:
   - artifact 名 = 包的文件名(`skill:x.zip`),不再随机;
   - 临时根已解析软链,`detect.relPath` 的 `Rel` 在 macOS 上也成立,证据路径 = 包内路径(`install.sh`),和 Linux 一致;
   - root 形状的包走 `CollectAll` 时 `home` = 私有临时根,里面只有这个子目录 —— 不再读共享 `$TMPDIR` 下的 `.claude.json` / `.mcp.json` / 桌面版目录。
2. **`cmd/aguard` 新增 `archiveView`**(`checkTarget` 的 zip 分支和 Downloads 的 `checkCandidate` 共用,经 `checkExtracted`):查完把剩下仍指向
   临时根的绝对路径改写 —— artifact `path` → 包路径;`locations` 里 "Config root" → 包路径,`home` 推出来的几条(都在私有临时根里、按构造必然
   absent)去掉;绝对路径形式的证据 `file`(如 root 形状空包的 `COV-000`)→ 包内相对路径;片段与 `Why` 里引用的解压目录(I/O 错误原文)→ 包文件名。

**SARIF 的 uri 取包内路径,不取 `x.zip!/install.sh`**(未决 1):目录目标的 uri 本来就相对于被查的目标;zip 被当作"它解开后的文件夹"来查,
uri 跟着相对于包根,于是 `check x.zip` 与 `check x/`(同一棵树,路径不经软链)的 uri、指纹完全一样,在两者之间切换不会重开告警。
包名进指纹的话,`skill-1.2.zip → skill-1.3.zip` 每发一版就把全部告警重开一遍 —— 只是比现在慢一点的同一个病。
包是谁由 `properties.artifact`(`skill:x.zip`)和 run 级 `aguard/root` 说。

**哈希与批准不动**:zip 的 artifact 哈希是解出来那棵树的 `TreeHash`,按包内相对路径算,与目录名无关(两次相同);批准库以哈希为键。
改的只是批准记录里给人看的 `name` / `path`(从已删除的临时目录变成包名 / 包路径)。Downloads 条目的 `hash` 照旧是包本身的字节。

## 完成的判据

- [x] `TestCheckZip_TwiceIsByteIdentical`(`cmd/aguard/archive_test.go`,新):扁平包与套一层目录的包,各 `checkTarget` 两次 →
  SARIF、text **逐字节相同**,markdown 与 JSON 除扫描时间外相同(markdown 印到分钟);四种输出里都没有 `aguard-inbox-`。
  今天红:两种包的 `properties.artifact` 都带随机名(所有平台),扁平包在 macOS 上 uri 与指纹也变
- [x] `TestCheckZip_SameFindingsAsItsFolder`(同上):扁平包的 SARIF 结果与**同一棵树作为目录**查出来的逐条相同(uri `install.sh`、指纹、
  级别、消息),只有 `artifact` 不同(`skill:flat.zip` 对 `skill:<目录名>`);artifact 哈希相同 —— 规范哈希不受影响
- [x] `TestCheckZip_DoesNotReadTheSharedTempDir`(同上):`$TMPDIR/.claude.json` 里放一个 MCP server,查 root 形状的包 → 没有这个 artifact,
  `locations` 只剩包路径那一条。今天红
- [x] `TestApprove_ZipRecordsTheArchive`(同上):`approvePath(x.zip)` 记下的 `name` = `x.zip`、`path` = 包路径,`hash` 与直接查同一棵树的相同。今天红
- [x] `TestScanInbox_ZipEvidenceIsArchiveRelative`(同上):Downloads 里的扁平包,条目发现的证据 `file` = `install.sh`,JSON 里没有 `aguard-inbox-`。今天在 macOS 上红
- [x] `TestExtractZip_FolderNamedAfterTheArchive`(`internal/inbox/inbox_test.go`,新):返回目录的 base = 包文件名、已解析软链、
  父目录里只有它、父目录不是共享临时目录(两边都解析软链再比);`cleanup` 删掉父目录。今天红
- [x] `TestCheckZip_AbsoluteEvidenceNamesTheArchive`(同上):root 形状空包的 `COV-000` 证据 = `root.zip`,不是解压目录的绝对路径
- [x] `TestCheckZip_ErrorTextNamesTheArchive`(同上):root 形状包里 `installed_plugins.json` 是目录、或 `skills` 是文件 →
  `IO-000` 的片段引用 I/O 错误原文,原文里是解压目录的绝对路径,而片段进指纹。两次 SARIF 逐字节相同,`IO-000` 仍在、片段写成包内路径
- [x] **反向断言**:目录目标的 SARIF 不变 —— `TestCheckDir_SARIFUnchanged` 钉住 uri `install.sh`、`artifact` `skill:myskill`、指纹字面量
  (在 `main` 上取值,W1 时就绿,改完仍绿),`locations` 仍有 "User MCP config" 等四条,`root` = 传入路径
- [x] **反向断言**:原本该响的仍然响 —— 包里的 `EXEC-001` 仍是 `error` 级、`overall < 70`;现有 `TestCheckTarget_ZipIsCheckedAsItsFolder`、
  `TestExtractZip_RefusesWhatWouldEscapeOrBloat`、`TestScanInbox_*` 不改一字仍绿;名叫 `.claude.zip`、顶层只有 `install.sh` 的包仍被当成目录读到
  (`TestCheckZip_DotClaudeNameIsNotARoot`:防"子目录用去掉扩展名的包名"这种改法把它路由进 `CollectAll`,顶层文件就没人读了)
- [x] `make verify` 绿;复现步骤(构建 `bin/aguard`,同一 zip 查两遍 `--sarif` 再 `diff`)扁平包 3 行变动、套一层的包 1 行 → 都是 0 行

## 不做什么

- **不改 `internal/report/sarif.go`**:uri、指纹、`artifact` 属性的公式一个字不动;它们读到的数据对了,输出就对了
- **`detect.relPath` 的目录目标指纹不动**
- **目录、单文件目标的任何输出一个字节不变**(反向断言);`scanLocations` 对单目标也列父目录那几条的老问题不碰
- **不改哈希、批准的键、信誉库匹配**;不改 zip 的安全上限、拒绝规则、0700/0600 权限、查完即删
- **不碰 `internal/collect`、`internal/detect`、`internal/gate`、`internal/model`**;不加 SARIF `uriBaseId` / `originalUriBaseIds`

## 不能说什么

- 不说"`check` 的输出全部可逐字节复现":JSON 的 `scanned_at`、markdown 的 "scanned … UTC" 每次不同。说"SARIF 与终端报告逐字节相同,
  markdown 与 JSON 除扫描时间外相同"
- 不说"macOS 上目录目标的 uri 也稳定了":`relPath` 没动(见不做什么)
- 不说 zip 的告警"能在 GitHub 上链到源文件":包里的文件不在仓库里,uri 是包内路径
- `$TMPDIR` 那条按实际说:被放进去的文件**只能加发现、不能藏发现**(多一个 artifact、分数只降不升),最坏是误拦;不说成"可绕过闸门"

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 七条测试 + 反向断言,跑红 | `cmd, inbox: tests — the same zip checked twice gives different reports, and a root-shaped zip reads the shared temp dir (P-016)` |
| 2 | `ExtractZip` 解到私有、已解析软链的临时根里以包名命名的子目录 | `inbox: a zip unpacks into a folder named after it inside a private temp dir, so its name and paths stop being random (P-016)` |
| 3 | `archiveView`:`check` 与 Downloads 把剩下指向临时根的路径改写成包;收紧临时根位置检查、加绝对证据测试 | `cmd: a checked zip is reported as the archive — path, locations and evidence no longer name the extraction dir (P-016)` |
| 4 | spec §4.1 第 3 条、§8 `ArtifactReport.Path` 注释;`.claude/rules/pipeline.md` 下载目录那条 | `docs: spec says a zip is reported by its own name and its member paths (P-016)` |
| 5 | `ExtractZip` 里的变量 `real` 改名 `resolved`(它遮住了内建函数 `real`) | `inbox: the resolved temp root no longer shadows the builtin real (P-016)` |
| 6 | 测试:I/O 错误原文里的解压目录;比较前去掉扫描时间;临时根检查两边都解析,跑红 | `cmd, inbox: tests — an I/O error quoted from inside a zip still names the extraction dir; compare without the scan time, resolve both sides of the temp-dir check (P-016)` |
| 7 | `archiveEvidence` 把片段与 `Why` 里的解压目录换成包文件名 | `cmd: an I/O error quoted from inside a zip names the archive, so that note's fingerprint stops changing (P-016)` |
| 8 | spec 措辞:只有 SARIF 与终端报告逐字节相同;引用的 I/O 错误写包名 | `docs: spec — markdown carries the scan time, so only SARIF and the terminal report are byte-identical; quoted I/O errors name the archive (P-016)` |
| 9 | 本文件、索引 | `proposals: P-016 (P-016)` |

## 未决问题

以下六条在旧仓已由 AI 按建议定下,移植时不变;合入前任何一条都可以推翻。

1. **SARIF uri 用包内路径(`install.sh`)还是 `x.zip!/install.sh`?**
   **建议:包内路径。** 与目录目标同一个约定(相对于被查的东西);`check x.zip` 与 `check x/` 指纹相同;包名换版本不重开告警;
   SARIF 没有 `!/` 语法,GitHub 会把它当一个字面路径。代价:同一个 Code Scanning category 里查两个不同的包、命中同一行同一规则会撞指纹
   —— 与今天查两个目录目标一样,用 SARIF category 分开。
   **已决(2026-10-08)**:按建议。
2. **artifact 名用包文件名(`x.zip`)还是去掉扩展名(`x`)?**
   **建议:`x.zip`。** 名字就是人传进来的那个文件;而且 `collect.looksLikeRoot` 认目录名 `.claude`,去掉扩展名的话 `.claude.zip` 会被路由进
   `CollectAll`,顶层的 `install.sh` 没人读 —— 一个靠改文件名的绕过。带扩展名的名字永远不等于 `.claude`(`IsZip` 要求 `.zip` 结尾)。
   **已决(2026-10-08)**:按建议。
3. **root 形状包里的 artifact,`path` 写包路径还是 `x.zip/skills/foo` 这种拼出来的路径?**
   **建议:包路径。** `path` 回答"在磁盘上哪儿能找到它",包里的东西答案就是包;拼出来的路径看着像真路径,闸门提示 ``aguard check "<path>"`` 照做会失败。
   包内位置在 `name` 和证据里。
   **已决(2026-10-08)**:按建议。
4. **`locations` 里 `home` 推出来的几条(User MCP config、Desktop store 等)怎么办?**
   **建议:去掉。** 它们现在落在私有临时根里,按构造为空、查完即删;改写成包路径是谎话,保留是噪音加随机路径。"Config root" 改成包路径保留。
   **已决(2026-10-08)**:按建议。
5. **顺手修 `detect.relPath`(把文件路径的软链也解析了)?**
   **建议:不。** 它改的是目录目标在 macOS 软链前缀下的 uri 和指纹,本条明确要求目录目标不变;动 `detect` 还要真机扫描。
   **已决(2026-10-08)**:按建议。
6. **长包名 + 深路径撞路径上限,要不要处理?** 多了一层以包名命名的目录后,很长的包名加上很深的条目路径会撞上 macOS 1024 字节的路径上限,
   `check` 退出 2,Downloads 那一项变成 "archive could not be opened"。
   **建议:不处理,记为已知限制。** 失败方向是关的(报错,不放行);攻击者本来就能只靠深路径条目让解压失败,新增的只是"又长名又深路径的良性包";
   为它截断目录名会让 artifact 名和包名对不上,还要再改写一遍名字。真有人报了再说,届时的做法是"目录名超过 N 字节时用短名,`archiveView` 再把 artifact 名改回包名"。
   **已决(2026-10-08)**:按建议。

**实现与 review 中攒下(旧仓 2026-10-08 攒下,本仓库重测)**

- **W1 的临时根位置检查原来比的是未解析的 `TMPDIR` 前缀**:W2 之后解压路径已解析软链(macOS 上是 `/private/var/…`),W1 那句断言就抓不到
  仍在临时根里的位置。W3 的提交里收紧(两种写法都比,并断言只剩包路径那一条),同时加 `TestCheckZip_AbsoluteEvidenceNamesTheArchive`
  (root 形状空包的 `COV-000` 证据原来是解压目录的绝对路径)。本仓库的变异检验:把 `archiveView` 换成只改 `Root` 的直通,
  位置检查报出四条("Config root" "User MCP config" "Claude Desktop store" "Desktop session cache"),`COV-000`、批准、两次比较三条也红;还原后全绿。
- **Linux 上改前 uri 与指纹本来就稳**(`/tmp` 不是软链),每次变的只有 `artifact` 属性和 JSON 的名字 / 路径;新测试在 Linux 上靠这几处照样红,CI 跑的就是 Linux。
- **W5**:`ExtractZip` 里的变量 `real` 遮住了内建函数 `real`,lint 没开这一项,改名 `resolved`。
- **(review)解压目录还从片段里漏**:`collect.ioNote` 把 `err.Error()` 原样当片段,I/O 错误原文带绝对路径,片段进指纹。于是 root 形状包里
  `skills` 是文件、或 `installed_plugins.json` 是目录时,同一个包两次的指纹仍然不同。W6 先写 `TestCheckZip_ErrorTextNamesTheArchive` 跑红,
  W7 让 `archiveEvidence` 把片段和 `Why` 里的解压目录换成包文件名(`fdopendir skillsfile.zip/skills: not a directory`);只换工具自己拼的那段前缀,
  不引入包里的内容,脱敏早已跑过。
- **(review)markdown 印 "scanned … UTC" 到分钟**,而 `renderAll` 原来只在 JSON 前把时间清零,跨分钟会假红。W6 一并修,并且这说明判据第一条原来写错了 ——
  markdown 不是逐字节相同;判据、"不能说什么"、spec 的措辞(W8)一起改成"SARIF 与终端报告逐字节相同"。
- **(review)`TestExtractZip_FolderNamedAfterTheArchive` 里"父目录不是共享临时目录"那句在 macOS 上原来永远不会响**(一边解析了软链、一边没有)。W6 两边都解析。
- **(review)长包名 + 深路径撞路径上限**:见未决 6,本仓库实测 200 字节的包名 + 816 字节的条目路径,`main` 的二进制退出 0,本分支退出 2
  (`mkdir …/aguard-inbox-<随机>/<包名>/…: file name too long`)。按未决 6 记为已知限制,不修。

## 完成

红的证据全部在本仓库测得:W1 的测试打在 `main` 的 `dec64ca`(v0.18.0)上;W6 的测试打在 W5 上(W1–W5 已应用)。
旧仓 8 个提交(module path 已换成 AgentGuard、P 号已换成 P-016)按顺序 `git am -3`,**无冲突、无手工改动**;移植时只把 W1 测试注释里的
"Taken on origin/dev" 改成 "Taken on main"。本仓库自导出以来没有动过 `internal/inbox`、`cmd/aguard/inbox.go`、`cmd/aguard/main.go` 的 zip 路径,
没有哪一部分已经被修过。

```
合入:PR 待开(2026-10-09;sha 合入后用 git log --grep P-016 找)
发布:待发
证据:W1 打在 main(dec64ca)上红 6 条、反向断言绿 5 条:TestCheckZip_TwiceIsByteIdentical(flat:uri aguard-inbox-<随机>/install.sh、artifact skill:aguard-inbox-<随机>,四种输出都含 aguard-inbox;nested:artifact directory:aguard-inbox-<随机>)、TestCheckZip_SameFindingsAsItsFolder(zip uri aguard-inbox-<随机>/install.sh 对目录 install.sh)、TestCheckZip_DoesNotReadTheSharedTempDir(收进 $TMPDIR/.claude.json 里的 mcp:planted,四条位置指向临时目录)、TestApprove_ZipRecordsTheArchive(name aguard-inbox-<随机>、path 已删除的临时目录)、TestScanInbox_ZipEvidenceIsArchiveRelative(证据 aguard-inbox-<随机>/install.sh)、TestExtractZip_FolderNamedAfterTheArchive(目录名随机、未解析软链、直接在共享临时目录里)
证据:W2 后 SARIF 已两次相同、TestCheckZip_SameFindingsAsItsFolder / DoesNotReadTheSharedTempDir / ScanInbox_ZipEvidenceIsArchiveRelative / ExtractZip_FolderNamedAfterTheArchive 转绿;TestCheckZip_TwiceIsByteIdentical 仍因 text / markdown / JSON 红,TestApprove_ZipRecordsTheArchive 仍因 path 是 <临时根>/flat.zip 红;W3 后全绿
证据:TestCheckZip_SameFindingsAsItsFolder(cmd/aguard/archive_test.go);zip 与同一棵树作目录:uri install.sh、指纹 77848c2e390ecd00、级别与消息逐条相同,只差 artifact(skill:flat.zip 对 skill:myskill);artifact 哈希相同
证据:TestCheckZip_DoesNotReadTheSharedTempDir;位置只剩包路径一条。变异(archiveView 换成只改 Root 的直通)报出四条位置,TestCheckZip_AbsoluteEvidenceNamesTheArchive、TestApprove_ZipRecordsTheArchive、TestCheckZip_TwiceIsByteIdentical 同时红;已还原
证据:TestCheckZip_ErrorTextNamesTheArchive;W6 打在 W5 上红(manifest_is_a_directory、skills_is_a_file 两例:IO-000 的 aguard/v1 指纹两次不同,text / markdown / JSON 含 aguard-inbox)→ W7 后绿,IO-000 仍在
证据:TestExtractZip_FolderNamedAfterTheArchive(internal/inbox/inbox_test.go);W6 版测试对着 main 的 ExtractZip 跑,三句都响(名字随机、未解析软链、"sits directly in the shared temp dir")
证据:反向断言 TestCheckDir_SARIFUnchanged —— 指纹字面量 77848c2e390ecd00 在 main 上绿,改完仍绿;TestCheckZip_DotClaudeNameIsNotARoot、TestCheckTarget_ZipIsCheckedAsItsFolder、TestExtractZip_RefusesWhatWouldEscapeOrBloat、TestScanInbox_ChecksDownloadsWithoutTouchingTheScore 不改一字前后都绿
证据:复现(main 与本分支同一组 -ldflags 各编一个二进制,同一 zip 查两遍 --sarif 再 diff):扁平包 3 行变动 → 0,套一层的包 1 行 → 0;十种包形状(扁平、套一层、root 形状空包、root 形状带 skill、插件、单个 CLAUDE.md、.claude.zip、带被拒条目、installed_plugins.json 是目录、skills 是文件)× 四种输出(--json、--sarif、--md -、--verbose):aguard-inbox 出现 136 次 → 0 次,两次 SARIF 变动 22 行 → 0 行,退出码逐个相同
证据:$TMPDIR 指向放了 .claude.json(含一个 MCP server)的目录,check root.zip:main 收进 mcp:planted + EXEC-001、列出四条位置 → 本分支 0 个 artifact、位置只有包路径;approve flat.zip:main 记 name aguard-inbox-<随机>、path /var/folders/…/aguard-inbox-<随机> → 本分支 flat.zip / 包路径,哈希键 43db58ad… 前后相同
证据:反向断言 目录与单文件目标逐字节不变 —— 5 个目标(扁平目录、skill 目录、套一层的目录、单个 install.sh、root 形状目录)× 4 种输出(文本含退出码、--json 去掉 scanned_at、--sarif、--md - 去掉 scanned 行),main 与本分支 20 例 0 处不同
证据:真机 scan --root ~/.claude --json:69/100、180 个 artifact,main 与本分支除 scanned_at / tool_version 外完全相同(这台机器 ~/Downloads 里没有候选,Downloads 一路的证据只来自测试和形状扫描);internal/collect、internal/detect 未动,这一条只为 Downloads 走 ExtractZip
证据:不做什么 —— git diff --stat origin/main -- internal/report internal/collect internal/detect internal/gate internal/model .claude/rules/invariants.md go.mod go.sum 为空
证据:make verify: all gates passed;go version go1.23.5(无工具链切换);go.mod 第二行 go 1.23.5,无新依赖;cmd/aguard 覆盖率 51.4%、internal/inbox 74.0%
```
