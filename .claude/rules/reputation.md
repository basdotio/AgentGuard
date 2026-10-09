---
paths:
  - "internal/reputation/**"
  - "hack/reputation-refresh/**"
---
<!-- SPDX-License-Identifier: MIT -->
## 声誉白名单(`internal/reputation`)

`good` 条目按 canonical 哈希命中后**压掉该 artifact 的全部计分发现**,只留一条 `REP-GOOD` note。
这是全工具里唯一一处"人的判断直接改分数"的地方,所以规矩比别处严:

- **只收两种来源:官方市场(`claude-plugins-official`)钉了 (url, sha) 的插件,和 Claude 桌面版自带的
  Anthropic skill(`source: claude-desktop`)。** 白名单是一份信任声明,范围越窄越站得住;第三方插件同样的
  形状照样报,那是对的。桌面版那一类是 2026-09-04 加的例外,理由是它们没有可钉的公开仓库(import-memory
  等六个不在 anthropics/skills 里),而它们装在每一台桌面版机器上,不压掉就是每个新用户第一次扫都看到
  69 分。**例外的边界**:`sha` 钉的是桌面版 manifest 里该 skill 的 `updatedAt`,`path` 是 `skills/<名>`,
  `publisher` 写 Anthropic;仍按哈希匹配、仍要 reviewed/reason/findings、续期仍是"同字节或同指纹"。
  `-add-desktop` 拒收 creatorType 不是 `anthropic` 的 skill——用户自己上传的归 `.aguardignore`,不进
  全员共享的名单。**弱在哪要说清**:续期只能在装了桌面版的 Mac 上跑,CI(ubuntu)对这类条目只报
  "not checkable"不算 stale;桌面版一升级、字节一变,这些条目就过期到有人本地跑 `make reputation-refresh`。
- **每条带 `source` 的条目必须同时带 `sha`、`reviewed`、`reason`、`findings`**
  (`TestCuratedEntriesCarryTheirReview`)。`reason` 要逐条说清被压掉的发现为什么无害
  (例:"server.cjs 里的 base64 是 RFC 6455 WebSocket accept key,不是出门前编码");`findings` 是
  这次审阅覆盖的指纹 —— 每条计分发现一行 `RULE-ID <file>`,排序,重复保留。`REP-GOOD` 的 `Why`
  会把 `reason` 印进报告,让"为什么信它"在报告里有答案,而不是在维护者的记忆里。
  **没有例外**:原来那条来历不明的种子条目(无 source、无 reason、也不在官方市场)已在
  2026-09-03 删除,理由正是"说不出为什么信它"。
- **续期是机械的,审阅不是。** [hack/reputation-refresh](../../hack/reputation-refresh/main.go)
  (`make reputation-refresh`)拉市场当前钉住的 commit,跑 `aguard check`,指纹**完全一致**才更新
  hash/sha/version;多一条、少一条、换了文件,都退出 1 并打印差异,条目原样留着等人看。
  它**永不**自己编 reason,也永不放宽 reason 的覆盖范围。每周一有 workflow 跑它并开 PR
  (`.github/workflows/reputation-refresh.yml`);那个任务变红就是"需要重新审阅"的信号,不是抖动。
  哈希没变的条目只更新 sha,不重扫 —— 同一份字节不需要第二次审阅。
- **两种钉法,一个模型。** 市场里的插件要么是独立仓库、用 (url, sha) 钉住(superpowers),要么是
  **市场仓库自己的子目录**(`"source": "./plugins/receipts"`,Anthropic 自己写的那批全是这样)。后者
  没有独立 commit,条目记 `source`=市场仓库、`sha`=市场 commit、`path`=子目录。市场一动 sha 就变,
  所以续期先比哈希:子目录字节没变就只更新 sha。`-marketplace` 可以给 URL、git checkout,或
  Claude Code 自己的镜像目录 `~/.claude/plugins/marketplaces/claude-plugins-official`—— 后者不是
  git 仓库,但 `.gcs-sha` 里就是 commit(实测与上游 HEAD 一致、子目录哈希相同),按目录名认出是官方
  市场后直接用本地文件,不联网。给纯 marketplace.json 文件时拿不到 commit,vendored 条目会被报
  "无法钉住"并跳过。
- **入库用 `-add <插件名>`,人只做审阅。** 它定位市场钉住的版本、拉下来、跑 check、算哈希和指纹,
  打印一份**审阅单**(每条发现的规则、级别、`file:line`、snippet)和一份 `reason` 留空的草稿条目;
  加 `-write` 会把草稿直接追加进 JSON,此时 `TestCuratedEntriesCarryTheirReview` 会红,**直到有人填上
  reason** —— 这是刻意的:一条没有理由的条目不允许以任何路径变绿。同名条目已存在时拒绝追加。
- **压制必须在默认视图里以"决定"的口吻出现,不能折进"覆盖不全"。** dimension-0 note 有两种:
  覆盖类("我没读 X")折成一行是好意;压制类(`REP-GOOD`/`IGN-000`,"我读到 18 条 high 然后
  按某人的决定从分数里拿掉了")说的是读者正在看的那个数字,以前被折进同一行并叫作
  "coverage is incomplete" —— 句子说反了。现在 [report/text.go](../../internal/report/text.go) 的
  `writeNotes` 把压制类单独成块、每条印条目和审阅的前几句(`clipSentences`),覆盖类照旧折叠;
  HTML 的 notes 也印 `Why`。`REP-GOOD` 的 `Why` 由 `entryLabel` 写出条目名、版本、publisher、
  审阅所用的仓库和 commit —— artifact 名里的 `@claude-plugins-official` 是用户从哪装的,
  这里写的是我们信了谁。命中的 artifact 还带 `reputation` 字段(JSON/HTML),让 100 分的
  "被信任"和"本来干净"在数据里分得开。
- **`.in_use/` 必须排除在哈希之外**([collect/skip.go](../../internal/collect/skip.go))。Claude Code 给
  每个正在被会话使用的已装插件写一个 `.in_use/<pid>` 文件,实测 figma 的 cache 和它来源 commit 的
  哈希不一致,差别**只有**这两个文件。不排除的话,任何声誉哈希都对不上运行中的机器 —— 而运行中是
  唯一有人会扫的时候。这是 `ExcludeFromHash` "冻结"规矩下唯一允许的追加:含 `.in_use/` 的树
  从来没有过可以被打破的稳定哈希。
- **指纹为什么不带行号**:版本一升所有行号都动,带行号的指纹永远对不上,续期就退化成"手动跑一遍
  然后照单全收"。rule + file 正是人写审阅结论时用的粒度,所以也是结论仍然成立的粒度。
- **它能保护到哪一步,要说实话**:续期的强度等于静态扫描本身 —— 上游改了代码但没动任何一条发现,
  会被不经阅读地接受;可这份代码不进白名单也一样是 100 分。白名单只会拿掉**人读过的**发现,
  从不给扫描器本来不会给的信任。
- **为什么不用变量解析替代它**:superpowers 剩下的 high 里有两类 —— URL 放在变量里
  (`http.get(url)`),以及一个真实的外部 URL 常量(品牌 logo 图片)。前者要跨语句追值,词法层
  做不到且每个近似都 fail-open;后者静态分析根本区分不了。两类都是"机器读不懂、人一眼看懂",
  正是白名单的适用范围。细节记在 ROADMAP「Known limitations」。

