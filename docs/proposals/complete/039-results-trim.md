<!-- SPDX-License-Identifier: MIT -->
# 039 — results 里六个只被完成提案引用的中间 run、6.2 MB 引用恶意原文的 raw、一条从未跑过的工具登记

- **来源**:开源前清理扫描(2026-09-30)第二节决定三;同事要求 results 保留,领导定"按建议精简"
- **依赖**:P-037(路径已脱敏,本条只管留多少)
- **分支**:`p/039-results-trim`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

`baselines/README.md` 自己的规则是:results 只留"一份基线,或一对被引用的前后对照;把每次实验都存一份的目录里没有基线"。
按这条读 2026-09-30 的 results:

| 什么 | 规模 | 违反在哪 |
|---|---|---|
| `aguard/2026-09-28`、`28b`、`29`、`29b`、`29c`、`29d` | 6 × 852 KB | 每个是一条已完成 proposal(P-024/025/026/028/029/030)的"after",没有活文档引用;`29d` 与 `29e` 的 verdicts 逐字节相同 |
| 四个判官 run 的 `raw/` | 6.2 MB、1,286 个文件 | 模型输出里引用了恶意样本原文:反弹 shell 28 处、`curl \| sh` 112 处、挖矿命令 1 处。公开仓托管这些会被 GitHub 和杀软的扫描标记;P-037 把它们当"唯一一手证据"留下,证据可以留在别处 |
| `tools.yaml` 的 heeler 条目 | 21 行 | 没有 adapter、没有结果、没有 proposal 文件,却登记着 `uploads_samples: true`,读者会以为量过。另有六处代码注释拿它当"下一个" |

## 初步方向

删六个中间 run;四个 raw 打成一个 tar.gz 作 release 资产,sha256 写进各 run.yaml 的 `raw_archive:`;删 heeler 条目和注释;
README 写明留哪两份静态 run(`2026-09-24` 是 v0.12–v0.15 规则工作的 before 和判官子集的母体,`2026-09-29e` 是当前基线)以及 raw 去了哪。
`-s3` 与 `-s3-votes` 两个目录**都留**:清理报告建议只留后者,但前者的 judge.jsonl 是 `samples: 3` 的原始每样本数据,后者的 compare.txt 只有汇总,
删了就少一手;去掉 raw 后两者各 0.6 MB,重复的三个小文件不值得打破"每个目录自足"。

## 完成的判据

- [ ] `ls baselines/results/aguard/` 只剩 `2026-09-24`、`2026-09-29e` 和四个 `-llm-` 目录;`find baselines/results -name raw` 为空。
- [ ] 四个判官 run.yaml 都有 `raw_archive:` 块,sha256 一致,且等于 `shasum -a 256` 对资产文件的结果。
- [ ] `grep -rn heeler baselines --include='*.go' --include='*.yaml' --include='*.md'`(results 除外)为 0。
- [ ] 反向断言:保留的 run 一个字节没动(`git diff --stat origin/dev -- baselines/results/aguard/2026-09-24 baselines/results/aguard/2026-09-29e baselines/results/ccaudit baselines/results/skill-scanner` 只有 run.yaml 的追加,其它为空);文档链接检查绿(三个进 results 的 markdown 链接都指向保留的目录)。
- [ ] `go test ./baselines/...`、`make verify` 绿。

## 不做什么

- 不动 `2026-09-24`、`2026-09-29e`、ccaudit、skill-scanner 两份,以及四个判官 run 除 raw 外的任何文件。
- 不截 scorecard(README 说它是打分器原样输出,永不改写)。
- 不删 `baselines/isolate/`:无使用者,但注释已改为"预留、无排期"。
- 不改引用了被删目录的已完成 proposal 正文(P-024 到 P-034):proposals 整体将移出仓库,历史记录里的路径失效是可接受的。

## 不能说什么

- 不说"raw 没了":raw 在 release 资产里,run.yaml 有名字和 sha256。
- 引用判官数字时仍按 P-021/P-025 的规矩带模型、`samples`、子集和日期。

## 工作项

| W | 一句话 | 提交信息 |
|---|---|---|
| 1 | 删六个中间 run 和四个 raw;raw 打包留 sha256;run.yaml 加 `raw_archive:`;.gitignore 去例外 | `baselines: results keep two static baselines and the four judge runs; raw/ becomes a release asset, six intermediate runs go (P-039)` |
| 2 | 删 heeler 登记和六处注释 | `baselines: drop the heeler entry nobody ever ran, and the comments that called it next (P-039)` |
| 3 | README、本文件、索引 | `baselines: say which static runs stay and where raw/ went (P-039)` |

无 W1 红测试:删除没有可钉的行为;反向断言是保留文件零变化和既有套件绿。

## 未决问题

1. **raw 资产挂在哪个 release?** 建议:新仓的首个 release。文件在 scratch 里等着,名字 `aguard-judge-raw-2026-09.tar.gz`,699 KB(压缩前 6.2 MB)。**已决(2026-09-30)**:按建议,由人上传。

## 完成

```
合入:PR(2026-09-30;sha 合入后用 git log --grep P-039 找)
发布:待发
证据:results/aguard 剩 2 静态 + 4 判官目录,find -name raw 为空;四个 run.yaml 的 raw_archive.sha256 = 10a7ba6eb7e70f336da4616cfb7ff02a0e59749f8868e7ffb1e84ea2faa4eafd;
     heeler 在 baselines 代码与登记里 0 命中;反向断言:保留 run 除 run.yaml 追加外零 diff;go test ./baselines/... 与 make verify 绿
```
