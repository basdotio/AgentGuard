<!-- SPDX-License-Identifier: MIT -->
# 024 — 教科书级反弹 shell 通过闸门:唯一认它的规则只认 shell 惯用法,而且只给 low

- **来源**:`docs/planning/work-items.zh-CN.md` W-027(2026-09-16 实测);plan §3.1 第 2 步"反弹 shell",执行序 1(P-023 已决 3)
- **依赖**:无
- **分支**:`p/024-reverse-shell-passes-gate`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

一个把交互 shell 交给远端的 skill,在 aguard 的闸门前是放行的。两个问题叠在一起:

1. **定级**:唯一认反弹 shell 的规则 `BD-003` 是 low + advisory。它在语料 300 个恶意样本里有 7 个
   "已经认出、却因为只是 low 而放行"(5 个 Cisco 服务端样本里的 `bash -i >& /dev/tcp/…`、
   skillsgoat 的 `nc -e /bin/sh`、一个 `.mcp.json` 的 `args` 里的 `bash -i >& /dev/tcp/…`)。
2. **覆盖**:`BD-003` 的正则是 `(reverse\s*shell|nc\s+-e|/dev/tcp/)`,只认 shell 惯用法。
   语言原生的写法——Python `socket` + `os.dup2(s.fileno(), 0/1/2)` + `/bin/sh -i`——它一个字都不认:
   W-027 的最小复现得 88/100,唯一的发现是 `EXEC-004` medium(`subprocess`)。语料里的 `revshell-python-dup2` 就是它。

同时 `BD-003` 的 `/dev/tcp/` 那一支会在**不是反弹 shell** 的地方响:语料 `sc-T8-env-aware-hard` 里的
`echo >/dev/tcp/8.8.8.8/53` 是探测网络通不通,真实世界里 `bash -c "</dev/tcp/localhost/5432"` 是等数据库端口的常见写法。
所以不能把 `BD-003` 整条提级。

后果:一个 agent 装上就会把机器交出去的 skill,`aguard check` 退出 0,闸门还会记一条"已信任"。

## 初步方向

新加一条 high 规则,只认"交互 shell 加标准输入输出落到网络套接字上"这个有确定语义的形状:
shell 一行式(`bash -i >& /dev/tcp/…`、`nc -e sh`、`socat … exec:sh`、`sh -i | nc`)用行规则;
语言原生形状(Python / Node / Go:套接字连接 + fd 落到 0/1/2 或把解释器的 stdio 接到套接字)用同文件共现的结构化检查。
`BD-003` 留着当关键词提示,但在新规则已经命中的行上不再重复报。动 `internal/detect`,加规则后跑 `make docs`。

**值得设计**:人 2026-09-28"直接按照顺序开始改进"(P-023 执行序 1)。

## 完成的判据

- [ ] `TestDetect_ReverseShellGatesAtHigh`(`internal/detect/revshell_test.go`)钉住必须响的形状,每条 `BD-004`、high、维度 7、`Advisory` 为真:
      `bash -i >& /dev/tcp/…`、`sh -i >& /dev/udp/…`、`/dev/tcp/… 0>&1`、`nc -e /bin/sh h p`、`nc h p -e /bin/bash`、`ncat --exec`、
      `socat … exec:bash`、`mkfifo` 管道里的 `/bin/sh -i 2>&1 | nc`;W-027 的 Python 最小复现(`socket` + `os.dup2(…, 0/1/2)` + `/bin/sh -i`);
      Python `pty.spawn`;Node `net.connect` + `spawn('/bin/sh')` + stdio `pipe`;Go `net.Dial` + `exec.Command` + `cmd.Stdin = conn`;
      `.mcp.json` 的 `args` 里的 `bash -i >& /dev/tcp/…`
- [ ] **反向断言**(同一测试的 quiet 表):普通 socket 客户端(connect + send + close)、`echo >/dev/tcp/8.8.8.8/53` 连通性探测、
      `</dev/tcp/localhost/5432` 等端口、同文件有 socket 又有与之无关的 `subprocess.run(['git', …])`、Node 连 socket 又 `exec('git status')`、
      Go `net.Dial` 又 `exec.Command("git")` 且不把 stdio 接到连接上——**全部不出 `BD-004`**
- [ ] **`BD-003` 仍然响**:关键词行(`# Reverse shell` 散文、`echo >/dev/tcp/8.8.8.8/53`)照旧出 `BD-003` low;
      在 `BD-004` 已命中的那一行上**不再**重复出 `BD-003`(同一件事报两遍会读成两个问题)
- [ ] `make bench`:恶意 87 → **95**(7 个已被 `BD-003` 认出的 + `revshell-python-dup2`),良性 147 不变,hard negative 4/19 不变,
      ledger 3,539 个全部 scored;新拦下的 8 个在 verdicts 里报 `backdoor` 维度
- [ ] `make docs` 后 `docs/rules.md` 含 `BD-004`;`TestEveryRuleIDIsDocumented`、OWASP 映射测试绿
- [ ] `make verify` 绿;`./bin/aguard scan --root ~/.claude` 真机头部贴进未决问题,新增 `BD-004` 为 0 或逐条说明

## 不做什么

- **不改 `BD-003` 的正则与定级**:它仍是关键词提示;唯一改动是一个 `exceptWhen`,在 `BD-004` 的行形状上让位
- **不把 socket 加进 `networkRE`**,不碰 `EXFIL-*` 链(W-027 修法第 3 条:那会让任何正常网络代码半条外泄链)
- **不改 `EXEC-004` 的定级**(Python 执行提级已由 plan 否决)
- **不做 Perl / Ruby / PHP 的一行式**:语料里没有样本,每一种都是一个新的误报面;需要时另开
- **不改 `plan.zh-CN.md` 里第 2 步的格子**,等 P-023(PR #27)合入后在交付时 rebase 再标完成——两条 PR 改同一格会冲突
- **不删 `baselines/results/aguard/2026-09-24/`**:benchmark-review、P-023 和 plan 都引用它;新运行另起目录

## 不能说什么

- **不写"增强了后门检测"**(W-027 原话):写"一条已有的规则只认 shell 惯用法、而且只给 low,于是一个教科书级反弹 shell 通过了闸门"
- **不写"覆盖所有反弹 shell"**:只覆盖上面判据列出的形状;Perl/Ruby/PHP、编码后的载荷、跨文件拆开的连接与交付都不在内
- 召回数字带分母:"300 个恶意里 87 → 95",不写百分比提升

## 工作项

| W | 一句话 | 提交信息 |
|---|---|---|
| 1 | `revshell_test.go`:必须响、必须沉默、`BD-003` 让位三组;先红 | `detect: tests for a shell handed to a socket — red until BD-004 exists (P-024)` |
| 2 | `BD-004` 行规则(shell 一行式),`BD-003` 加 `exceptWhen`,OWASP 映射 | `detect: an interactive shell whose stdio lands on a network socket is high, not a low keyword (P-024)` |
| 3 | `BD-004` 结构化一半:同文件套接字连接 + fd 落到 0/1/2 / 解释器 stdio 接到连接(Python/Node/Go) | `detect: the language-native reverse shell — socket plus fd handoff in one file — gates too (P-024)` |
| 4 | `make docs`;work-items W-027 按 §7 结案(正文删、§9 留一行) | `docs: BD-004 in the rule reference; W-027 closed (P-024)` |
| 5 | `make bench` → `baselines/results/aguard/2026-09-28/`,与 2026-09-24 的 verdicts 做 diff | `baselines: aguard after BD-004 — 95 of 300, benign unchanged (P-024)` |

## 未决问题

人 2026-09-28 说"直接按照顺序开始改进",所以下面每条**先按建议答案做了**,交付时一起确认,推翻哪条就改哪条。

1. **新规则放哪个维度?** plan 与 W-027 写的是维度 4(代码执行)、非 advisory。**建议并已按此做:维度 7(后门)、high、保留 advisory 标签。**
   理由两条:(a) 规格 §16 不变量 6 要求维度 7 的静态命中一律标"仅提示,非确认",非 advisory 的维度 7 规则违反它;
   (b) 语料里这些样本的真值维度是 `backdoor`,而 adapter 只把**过了阈值**的发现的维度报给评分器——放维度 4,新拦下的 8 个会全部报成
   `execution`,attribution 从 51/87 掉到约 51/95。advisory 在 aguard 里只是报告标签:`score.Deterministic` 不排除它,闸门照拦。
   代价:一个会拦的 high 在报告里写着"未确认",读起来别扭,但它说的是真话——静态看到了形状,没看到它跑。
2. **一个 ID 还是两个?** **建议并已按此做:一个 `BD-004`**,行规则与同文件结构化检查两半共用(`PERM-007` 的先例);结构化那一半在同文件
   行规则已命中时不再出。
3. **在哪些文件角色上跑?** **建议并已按此做:和 `BD-003` 相同**(脚本、SKILL.md、hook command、MCP 配置;不跑随包文档和工具说明)。
   SKILL.md 里教 agent 执行 `bash -i >& /dev/tcp/…` 正是闸门该拦的;渗透测试速查类 skill 会被拦,这一点在真机和良性池上量,量到再说。
   结构化那一半只跑真实脚本文件(`roleScript` 且非合成单元)。
4. **新基线目录?** **建议并已按此做:新增 `2026-09-28/`,保留 `2026-09-24/`**(被三份文档引用)。

**实现中攒下(2026-09-28)**

- **实测 +11 而不是估的 +8,全部真阳性。** 除设计列的 8 个,多出三个 `BD-003` 从没抓到的形状:
  两个 `nc -l 4444 -e /bin/bash` 绑定 shell(`conditional_behavior_privileged_users`、`goal_subversion_objective_replacement`)、
  一个 `socat tcp:… exec:/bin/bash`(`perm-allowed-tools-backtick`)。良性仍 147,hard negative 仍 4/19,回归 0。
- **判据 10 那条真机扫描抓到一个假阳性,已修。** `BD-004` 的行规则经 `rule()` 加了 `(?i)`,于是 `Nc` 匹配 `nc`;
  加上分支之间 `[^\n]*?` 不限距,一条 37KB 的 minified React bundle(`viewer-bundle.js`)里 `Nc … -e … shiftKey` 凑成一次命中。
  修法:`nc`/`ncat`/`socat`/`sh -i | nc` 三支要求在命令位置(行首或 `|;&(` 反引号 `$` 之后)、工具名后必须有空格、
  分支间距上界收紧。语料判决不变(仍 98/147),真机 `BD-004` 归零。配了一条 quiet 回归用例(`revshell_test.go`)。
  **这正是 §10 那条判据存在的理由:语料没有 minified 前端 bundle,只有真机扫描能撞到。**
- **BD-004 是否真阳性由人复核过**:11 个全是把交互 shell 交给套接字的构造,不是关键词误伤。

## 完成

```
合入:PR(2026-09-28;sha 合入后用 git log --grep P-024 找)
发布:v0.13.0
证据:TestDetect_ReverseShellGatesAtHigh(internal/detect/revshell_test.go)——15 个必响、8 个必静、BD-003 让位/仍提示;
     W1 在 W2 前红("BD-004 missing" ×15),W2 后 shell 一行式绿、W3 后语言原生绿;
     make bench(corpus 4f964622):malicious 87 → 98、benign 147 不变、hard-negative 4/19、3539 全 scored、fixtures 与 2026-09-24 逐字节同 —— baselines/results/aguard/2026-09-28/;
     反向断言 = quiet 表(普通 socket 客户端、echo >/dev/tcp 探测、端口等待、socket 旁无关 subprocess ×3 语言、nc -z、散文、minified bundle)全部不出 BD-004;
     真机 ./bin/aguard scan --root ~/.claude:修 minified-bundle 假阳性后 BD-004 = 0;
     make verify: all gates passed
```
