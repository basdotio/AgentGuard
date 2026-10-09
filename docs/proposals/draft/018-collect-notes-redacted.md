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
