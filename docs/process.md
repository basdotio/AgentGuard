<!-- SPDX-License-Identifier: MIT -->
# 开发流程守则

**一句话**:人提出问题、拍板、合入、发布;其余由 AI 按本文执行。状态只存在于 git 和文件里,
不引入任何外部工具。本文是守则的唯一版本;改它也走 proposal(见 §8)。

| 名词 | 含义 |
|---|---|
| **主干** | `dev`(2026-09-16 定,见 [proposals/complete/001](proposals/complete/001-adopt-process.md) 未决问题 1)。tag 从主干打 |
| **`main`** | 远端默认分支,**但不是开发主干**:落后 `dev` 两百多个提交,不在它上面开工。clone 后先 `git switch dev`。领导 2026-09-16 定:开发一直在 `dev`,将来一次性合入 `main` |
| **proposal** | 一份"要做什么"的记录,`docs/proposals/<draft|design|complete|rejected>/NNN-slug.md`,编号三位递增;**目录即状态**(§5) |
| **P-NNN** | proposal 的引用 ID,出现在分支名、提交信息、issues、changelog 里 |
| **闸门** | `make verify`,见 §4。全文所有"过闸门"都指这一条命令 |

---

## 1. 五个阶段,一个任务

| # | 阶段 | 谁 | 做什么 | 准出条件 |
|---|---|---|---|---|
| 1 | **提出** | 人 → AI → 人 | 人用一句话说问题;AI 先 `git fetch`,查 `issues/` 和 `proposals/` 有没有记过或重复,编号取 **`origin/<主干>` 索引**最大号 + 1(本地索引可能落后;撞了后推的改号,006 实测),从主干建 `p/NNN-slug`,在 `draft/` 里**只写问题**(现象、后果、来源、初步方向),提交到分支 | 人说值得设计。不值得 → 删分支,不留记录(源自 issue 的在 issue 里记一句) |
| 2 | **设计** | AI → 人 | `git mv` 到 `design/`,读够代码和规格,补判据、不做什么、不能说什么、工作项,**把不清楚的一次问完**,每条带建议答案;人改判据、划边界、答问题 | 未决问题全部 `已决` = 接受。**不合到主干**,接着做 |
| 3 | **实现** | AI | 同一分支接着做:**先写测试跑红,再改代码跑绿**;一个工作项一个提交;动了不变量或数据模型同步 `spec`;加/改规则跑 `make docs`;疑问攒进未决问题不打断;`make verify`;`collect`/`detect` 有改动跑真机扫描 | 闸门绿。AI 列判据对照证据,**问一句:交付还是废弃** |
| 4 | **交付** | AI → 人 | 交付:填「完成」(证据行;合入写 PR,不写 sha),`git mv design/ → complete/`,索引加行;`git fetch` 并 rebase 到主干,冲突自己解、披露;`make verify`;推分支;**开 PR 到 `dev`**(`gh` 不在就给比较链接),review 包在 PR 描述里:判据逐条对照证据、"不做什么"逐条给未改动的证明、**AI 自己不确定的点**。废弃:`git mv` 到 `rejected/`,正文写理由,同样开 PR。CI 在 PR 上独立跑同一道闸门 | 人在 GitHub 上 **Rebase and merge**(仓库设置只留这一种),分支自动删。**下一轮 AI 先 `git fetch` 看合没合**:没合就问一句;合了就**问要不要发版**,要 → 阶段 5,不要 → 结束 |
| 5 | **发版** | AI → 人 | 从阶段 4 末的"要发版"接进来,或人直接敲 `/release`。**不开 proposal**(领导 2026-09-20 定):AI 建 `p/release-x.y.z`,分支里**只有**两处版本号、ROADMAP 首段、各 proposal「完成」节的发布字段,和 `complete/NNN-release-x.y.z.md` 一份记录(包含哪些 P、changelog、改了哪里、证据行);changelog 从各 proposal「问题」一节拼出,不从提交信息猜;直接开 PR | 人在 PR 上看版本号和 changelog,合入、本地打 tag、push、发 npm 和 release(顺序按 `.claude/rules/npm.md`) |

**两个入口**:`/propose`(阶段 1–5 **是同一个任务**,一条命令走到 PR,合入后问要不要发版)和 `/release`(单独发版时用),
在 `.claude/skills/`,只由人触发;它们不复述本文,只按节号指回来。`/work` `/ship` 2026-09-20 撤掉:
同一个任务里多敲三次命令,是多记三个名字。

**主干上只有 `complete/` 和 `rejected/`。** draft 和 design 只存在于 `p/` 分支上,想知道在做什么,看
`git branch -r --list 'origin/p/*'` 和打开的 PR。阶段 3 里 AI 遇到的问题**攒到阶段 4 一起问**,不半路打断;
只有一种例外可以中断:继续做下去会越过「不做什么」的边界。

## 2. 人出现的地方(只有这几处)

| 检查点 | 在哪 | 为什么不能交给 AI |
|---|---|---|
| **值得设计吗** | 阶段 1 末,看 draft | "要不要做"是决定,AI 给意见不拍板 |
| **接受设计** | 阶段 2 末,答未决问题 | "做到哪"是决定。改判据、划边界是人在这条链上最值钱的一步,只在两头出现的人划不了边界 |
| **交付还是废弃** | 阶段 3 末,一句话 | 做完了不等于要 |
| **合入** | PR 页面 | AI 说"全绿"不等于对。本仓库已证明过**绿灯不构成证据**(`go` 指令那条),人看的是证据,不是结论。PR 上的 CI 绿勾不是 AI 说的,是独立跑出来的 |
| **要不要发版** | PR 合入后,AI fetch 确认合了再问,一句话 | 攒一批还是马上发,是取舍 |
| **发布** | 阶段 5 | npm 发出去不能撤,tag 推出去是公开承诺 |

## 3. 分支与提交

- **一个 proposal 一个分支**,`p/NNN-slug`;阶段 2 开始时从主干建,合入后删。不留长期分支。
- **一个工作项一个提交**,提交信息格式 `<scope>: <一句话,用后果说> (P-NNN)`。合入**不 squash**,
  用 rebase 保持线性历史 —— 提交信息里的 ID 和证据是回溯的载体。GitHub 上只允许 **Rebase and merge**
  (merge commit 和 squash 在仓库设置里关掉,页面上就点不错);它会重写 sha,所以「完成」里的合入 sha 合完之后从 `dev` 上取。
- **AI 只推 `p/` 分支,主干只由合并 PR 动。** PR 的 base 必须显式是 `dev`:GitHub 默认对着 `main` 开。
  状态仍只在目录里(§5);PR 是审的地方,不是状态。AI 不确定的点照样写进 proposal「未决问题」,不只留在 PR 上。
- **`p/` 分支上没有 `(P-NNN)` 的提交被 `hack/commit-msg` 拒绝**(`make hooks` 装;没装 hook 的机器拦不到,CI 不替它把关)。
- **工作项表里写提交信息,不写 sha。** rebase 一次 sha 全变(002 实测);找提交用 `git log --grep P-NNN`,最终 sha 只写进「完成」一节。
- **太大就拆 proposal,不拆分支。** 判据:能不能独立合入、独立回退。
- **并行**:每个分支各自的 worktree。有依赖的在 proposal 头部写 `依赖:P-NNN`,等它合入后再建分支。
- **例外**:同时满足 **不碰 `.go`、不碰 `spec`、不移动任何 proposal 文件** 三条的琐事(错别字、一行 CI)
  可以直接进主干。三条少一条就开 proposal。例外范围写死,不扩。
  「完成」一节在 PR 里填,合入不写 sha(GitHub 的 rebase 合并会重写它),写 PR 编号或链接;要 sha 用 `git log --grep P-NNN`。

## 4. 闸门:`make verify`

`make verify` 一条命令依次跑下面全部,任一失败即停;AI 和人用同一个判据:

```bash
make verify   # = go vet · go test -race -cover · golangci-lint · rules.md 无漂移 · plugin 自扫(--fail-on low)· go 指令 == 1.23.5
```

工具:`golangci-lint` 用 CI 钉的版本(`.github/workflows/ci.yml`,现在 **v2.13.2**),`brew install golangci-lint` 后核对
`golangci-lint --version`,或 `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2`。**不写 `@latest`**:
v1→v2 那次配置格式不兼容,lint 步曾因此假红。缺工具时 `make verify` 会指到这一段。

漂移那步的含义是**已提交的** `rules.md` 与代码不符:它先重新生成再比对 index,所以只在工作树里改坏文件拦不到,
那不是漂移(004 做负向测试时踩过)。

**不进 make 的一条**:`collect`/`detect` 有改动时 `./bin/aguard scan --root ~/.claude` 必跑真机。
它们吃攻击者可控字节,table test 抓不到的东西真机抓到过(一次 panic、两处误报)。
review 包里要贴这次扫描的头部。

## 5. 状态:目录即状态(2026-09-20 起)

状态不写在文件里,文件在 `docs/proposals/` 的哪个子目录就是什么状态;变更是一次 `git mv`,文件名不变。
`hack/check-proposal` 拒绝直接放在 `docs/proposals/` 下的 proposal 和带 `状态:` 行的文件。

| 目录 | 含义 |
|---|---|
| `draft/` | AI 只写了问题,人还没说值不值得设计 |
| `design/` | 人说值得设计;AI 补齐六节并把未决问题一次问完。**未决问题全部已决 = 接受**,接着在同一分支上实现,实现期间也留在这里 |
| `complete/` | 合入主干,文末「完成」一节填好,带证据行 |
| `rejected/` | 人决定不做(正文必须写否决理由;如源自 issue,同步 issue 状态为 `不规划`),或与另一份是同一件事(正文写并入哪份) |

`draft → design → complete` 是唯一的正向路径;`rejected` 可从前两个进入。draft 和 design **只在 `p/NNN-slug` 分支上**,
主干只有 `complete/` 和 `rejected/`;文件是随最后那个 PR 一起到主干的。"进行中"不是目录:`p/` 分支存在就是。
2026-09-16 到 09-20 之间用的是文件里六个状态词(`草案 已接受 进行中 已完成 已否决 已合并`),001–007 正文里还能见到。

## 6. 证据行

proposal 进 `complete/`,或 issue 状态改为 `已修复`/`部分修复`,文件里必须有一行:

```
证据:<测试名>(<文件>);<可复算的数字前 → 后 = 差值的解释>;<反向断言所在行,如有>
```

没有证据行的状态变更不合入(proposal 那半由 `hack/check-proposal` 在提交时拦)。这条针对的是已经发生过两次的事故(见 `issues/README.md` 维护约定):
功能做完而状态停在旧值,或改了别处漏了自己。

**代码类 proposal 的判据至少两条,其中一条必须是反向断言**(修完之后,原本该响的仍然响)。
这是本仓库唯一有效的过度修复防护,从 `work-items` 沿用。

## 7. 与现有文档的分工

| 文档 | 本守则生效后 |
|---|---|
| `docs/proposals/` | **要做什么**。新工作的唯一入口 |
| `issues/` | 不变。缺陷、绕过、否决记录,proposal 的「来源」常指向这里 |
| `docs/planning/work-items.zh-CN.md` | **冻结**:不再新增 W;老条目做完一条删一条;工作项改写在 proposal 正文里 |
| `ROADMAP.md` | 从已完成 proposal 汇总,不再手工维护条目 |
| `docs/spec/spec.zh-CN.md` | 不变。代码的规格;本守则不进它 |
| `docs/decisions/audit-log.zh-CN.md` | 不变。append-only |

判据仍是那一句:**这条修完之后,这段文字还有没有价值?** 有 → `issues/`;没有 → 写在 proposal 里,
随 proposal 完成而封存。

## 8. 守则怎么改

改本文也开 proposal。守则先写短的,跑两个 proposal 再补;一开始写全的守则一定有没人遵守的条。
例外记录:§5「目录即状态」和 draft/design 两段式由领导 2026-09-20 直接定,未走 proposal。
