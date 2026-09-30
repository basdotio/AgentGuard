<!-- SPDX-License-Identifier: MIT -->
# 002 — 合并 `doc/` 与 `docs/`:一个文档树、目录名即分类、全仓库无断链

- **来源**:P-001 工作项 → 002;领导要求"doc 结构重新设计"(2026-09-15)
- **依赖**:P-001(已完成)
- **分支**:`p/002-merge-doc-dirs`

## 问题

两个文档目录并存,分类靠 `CLAUDE.md` 里一张表维护:

| 目录 | 现在装什么 | 谁在引用 |
|---|---|---|
| `doc/`(18 个文件) | 中文规划、规格、审计日志、clean 手册、两份测量、7 月归档、竞品调研 | `CLAUDE.md` 10 处、`ROADMAP` 6 处、issues 若干、**5 处 Go 注释、1 处用户可见字符串**(`main.go:670` 打印 `doc/clean-guide.md`) |
| `docs/`(12 个文件) | 双语面向用户的架构 / 安装闸门 / LLM 裁判、生成的 `rules.md`、守则、proposals | `Makefile`、CI、`hack/gen-rules` 写 `docs/rules.md`;**plugin 的绝对 URL** 指 `docs/rules.md` |

新同事分不清 `doc/clean-guide.md` 和 `docs/install-gate.md` 为什么不在一起;`doc/` 里规格、规划、历史归档、
测量数据平铺在一层,一份"已被取代仅作历史保留"的 `design.md` 和唯一规格源头 `spec.zh-CN.md` 是邻居。
另外已存在的断链:`clean-internals.md` 4 处指向 `../prd-selective-clean.md`,文件不在本仓库。

## 目标结构

**只搬 `doc/`,`docs/` 顶层既有文件一个不动。** 理由:代码、CI、Makefile、生成器、plugin 里的绝对 URL
全部指着 `docs/rules.md`、`docs/install-gate.md`、`docs/architecture.md`;搬它们只换来整齐,代价是
已安装插件里的链接断掉。子目录只给从 `doc/` 搬来的东西用。

| 现在 | 之后 | 分类一句话 |
|---|---|---|
| `doc/spec.zh-CN.md` | `docs/spec/spec.zh-CN.md` | 规格源头,唯一一份 |
| `doc/{direction,plan,work-items,corpus-benchmark}.zh-CN.md` | `docs/planning/` | 为什么、做什么、怎么修、怎么量 |
| `doc/audit-log.zh-CN.md`、`doc/competitor-research-2026-09.zh-CN.md` | `docs/decisions/` | 证据与调研,append-only |
| `doc/design.md`、`doc/archive/design-2026-07/` | `docs/decisions/archive/` | 历史,不是源头 |
| `doc/clean-guide.md` | `docs/clean-guide.md` | 面向用户,与 install-gate / llm-judge 并列 |
| `doc/clean-internals.md`、`doc/measurement-*.md` | `docs/internals/` | 给改代码的人的实现手册与测量 |
| `docs/{architecture,install-gate,llm-judge,rules}.md`、`img/`、`process.md`、`proposals/` | **不动** | |

新增一份 `docs/README.md`:一张表,每个子目录一行"装什么、写给谁"。它接管 `CLAUDE.md` 那张表的
**路径**部分;表本身 003 再动。

## 完成的判据

- [ ] `doc/` 目录不存在;每个搬走的文件 `git log --follow` 能追到搬家前的历史
- [ ] 搬家提交只含 rename:`git show --stat --format= <sha> | grep -v ' => ' ` 为空(除新增 `docs/README.md` 那个提交)
- [ ] 新增 `TestDocsRelativeLinksResolve`:遍历仓库所有 `.md` 的相对链接(`](path.md` 与 `](path.md#anchor)`),
      每个目标文件存在。**先写,对搬家前的树应变红**(4 处 `prd-selective-clean.md`),搬完修完变绿
- [ ] **反向断言**:测试里故意注入一条断链的用例必须被抓住 —— 防止链接检查被写成永远通过
- [ ] `go build ./...` 通过(`main.go` 那条用户可见字符串改为新路径);`make test lint`;
      `make docs && git diff --quiet docs/rules.md`;`./bin/aguard check plugin --fail-on low` exit 0
- [ ] `plugin/` 下任何文件 **零改动**(`git diff --stat <base> -- plugin` 为空)
- [ ] `docs/planning/work-items.zh-CN.md` 顶部有冻结横幅:不再新增 W,新工作走 `docs/proposals/`,
      未开始的 W-017–W-027 各自开工时转成 proposal 并写明原 W 号
- [ ] `docs/README.md` 存在,每个子目录一行

## 不做什么

- **不改任何文档正文一个字**,包括把 `design.md` 这类"已被取代"的合并或删除。搬家和改写分开,否则 review 看不出真改动
- 不动 `docs/` 顶层既有文件和 `img/`
- 不动 `CLAUDE.md` 除了路径字符串;那张文档表和 90 KB 拆分是 003
- 不动 `plugin/`
- 不处理 `main` 分支
- 不加 `make verify` 等强制装置(004);本条的链接测试是普通单元测试,进 `make test`

## 不能说什么

不适用于报告文案。对本条:`docs/README.md` 只描述目录装什么,**不写"完整"、"权威"之类的词** ——
文档分类是约定不是保证。

## 工作项

| W | 一句话 | 提交 |
|---|---|---|
| 1 | 链接解析测试(含反向断言用例),对当前树跑红 | `test(docs): every relative markdown link resolves …` |
| 2 | 纯 `git mv`,零内容改动 | `docs: move doc/ into docs/ by category, no content change` |
| 3 | 修链接:md 相对链接、`CLAUDE.md`/`README*`/`ROADMAP`/`issues` 路径、5 处 Go 注释、1 处 `main.go` 字符串、`contract_test.go` 注释;测试转绿 | `docs: repoint every reference to the moved files …` |
| 4 | `docs/README.md` 目录表 + `work-items` 冻结横幅 | `docs: index the tree, freeze work-items, 002 in progress` |

## 未决问题

1. **`prd-selective-clean.md` 不在本仓库**(4 处引用)。它可能在已停维护的 agent-guard-design 仓。
   选项:a)找到它搬进 `docs/decisions/archive/`;b)链接改为纯文字"见 design 仓归档"。
   **建议 a**,找不到再 b。这是本条唯一可能碰到"改正文"的地方,所以先问。
   **已决(2026-09-16):按 a,找不到按 b。**
2. **另一会话仍在往 `work-items` 加 W**(W-021–W-027 是 2026-09-16 加的,作者 stopWarByWar)。
   冻结横幅挂上之后,那边要停。冻结生效日按合入日算。**已决(2026-09-16):冻结;另一会话停止新增 W。**
3. `doc/clean-guide.md` 搬到 `docs/` 顶层是把它当面向用户的指南。如果你觉得 `clean` 还没到公开推荐的程度,
   可以改进 `docs/internals/`。**已决(2026-09-16):放 `docs/` 顶层。**

4. **`audit-log` 里两处 `doc/gap-analysis.zh-CN.md`、`doc/spec-corpus-benchmark.zh-CN.md` 未改。** 它们是引文,
   指向从未以这些名字入库的文件;audit-log 是 append-only,条目写下即冻结,所以原样保留。
5. **W1 那个提交在历史上是红的**(测试先于修复,按守则)。`git bisect` 落到 W1 那个提交会看到一次已知的红。
6. **工作区那份 `../CLAUDE.md`(不在任何 git 仓库)里两处路径也改了**,否则下次会话按旧路径找 spec。不在本条范围,
   但不改会立刻伤到流程自身,所以顺手做了,在此披露。
7. `internal/collect/loaded.go` 与 `internal/clean/clean.go` 只改了注释里的路径,行为无变化;真机扫描仍跑了一次
   (`~/.claude`,69/100,与 W 表在 origin/dev 上记录的结果一致)。
8. **提交 sha 不该写进 proposal 的工作项表。** 第一版写了,合入前 rebase 到 `origin/dev` 一次,四个 sha 全变。
   现在表里只写提交信息,`git log --grep 'P-002'` 就能找到;最终 sha 只在合入后写进「完成」一节。
   这是对 `process.md` 的第一条修订意见,按 §8 攒到下一个 proposal 一起改。

## 完成

```
合入:fc83409(2026-09-16,ff 合入 dev;分支 p/002-merge-doc-dirs 已删)
发布:不随版本发布
证据:TestDocsRelativeLinksResolve + TestBrokenMarkdownLinksAreCaught(cmd/aguard/doclinks_test.go);
      W1 提交时 9 条断链 → W3 后 0 条;搬家提交 --stat 过滤 "=>" 后为空;plugin/ 对 origin/dev 零 diff;
      make test 20 包 ok、lint 0 issues、rules.md 无漂移、plugin 自扫 exit 0
```
