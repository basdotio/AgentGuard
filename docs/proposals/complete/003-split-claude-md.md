<!-- SPDX-License-Identifier: MIT -->
# 003 — 拆 `CLAUDE.md`:防护点按包搬进 `.claude/rules/`,只在碰到那个包时加载

- **来源**:P-001 工作项 → 003;领导要求"doc 结构重新设计"(2026-09-15)
- **依赖**:P-002(已完成)
- **分支**:`p/003-split-claude-md`

## 问题

`CLAUDE.md` 90 KB、810 行,每次会话开始全量进入上下文。它的内容是**按包划分的防护点**
(检测引擎 171 行、流水线 145 行、闸门 79 行、npm 67 行……),但改 npm launcher 的会话照样加载检测引擎那 20 KB,
反过来也一样。文件越长,它最重要的那几句(module path 三处一致、`go` 指令不许被顶高)越容易被淹掉。

Claude Code 官方支持 `.claude/rules/*.md`:文件头部 `paths:` 列 glob,**只在读写匹配文件时加载**;
不带 `paths:` 的和 `CLAUDE.md` 一样会话开始就加载(来源:code.claude.com/docs/en/memory,2026-09-16 核实;
建议每文件 200 行以内)。这正是现在这份文件缺的机制。

## 目标结构

**内容零改动,只搬。** 每个 `##` 段原样进一个文件;没有三级标题,不再切细 —— 切细要编标题,那是改内容。

| `CLAUDE.md` 现有段 | 去处 | `paths:` | 行 |
|---|---|---|---|
| 头部(文档地图、module path 警告)、常用命令 | **留在 `CLAUDE.md`**,后接一张"规则文件在哪"的表 | 始终加载 | ~67 |
| 不变量 —— 不要削弱 | `.claude/rules/invariants.md` | 无(始终加载) | 47 |
| 约定 | `.claude/rules/conventions.md` | 无(始终加载) | 57 |
| 流水线 | `.claude/rules/pipeline.md` | `cmd/aguard/**`, `internal/**` | 145 |
| 加载时闸门 | `.claude/rules/gate.md` | `internal/gate/**`, `cmd/aguard/gate*.go`, `hack/pre-commit` | 79 |
| 评分 | `.claude/rules/score.md` | `internal/score/**` | 11 |
| 检测引擎 | `.claude/rules/detect.md` | `internal/detect/**`, `hack/gen-rules/**` | 171 |
| 声誉白名单 | `.claude/rules/reputation.md` | `internal/reputation/**`, `hack/reputation-refresh/**` | 63 |
| Canonical 哈希 | `.claude/rules/hash.md` | `internal/collect/**` | 20 |
| 终端报告的两种模式 | `.claude/rules/report.md` | `internal/report/**` | 37 |
| LLM judge | `.claude/rules/judge.md` | `internal/judge/**` | 35 |
| Claude Code 插件 | `.claude/rules/plugin.md` | `plugin/**`, `.claude-plugin/**` | 54 |
| npm 分发 | `.claude/rules/npm.md` | `npm/**`, `Makefile`, `.github/**` | 67 |

会话开始的常驻量从 90 KB 降到约 16 KB(`CLAUDE.md` 约 6 KB + 不变量 + 约定)。不变量和约定选常驻,
因为它们也管文档和提交,不只管 `.go`。

## 完成的判据

- [ ] `CLAUDE.md` < 10 KB;它的正文 = 原头部 + 原「常用命令」+ **唯一新增的**那张规则文件表
- [ ] **零改动可复算**:把 13 个规则文件去掉 frontmatter 后按原顺序拼起来,与原 `CLAUDE.md` 对应的段
      逐字节相同。核对命令写进「完成」一节
- [ ] 新增 `TestClaudeRulesAreScopedToExistingPaths`:每个 `.claude/rules/*.md` 的 frontmatter 可解析;
      有 `paths:` 的,每条 glob 至少匹配一个已跟踪文件(防止 scope 写错后规则永远不加载);每文件 ≤ 200 行
- [ ] **反向断言**:测试里一条匹配不到任何文件的 glob、一个 201 行的文件,都必须被抓住
- [ ] `TestDocsRelativeLinksResolve` 仍绿 —— 规则文件里指向 `docs/` 的链接要改成 `../../docs/…`(这是唯一允许的文本改动,
      与 002 同一性质)
- [ ] `make verify` 等价的五条全绿;`plugin/` 零改动
- [ ] **人工核对(检查点 2)**:在仓库里开一个新会话,`/memory` 列出 `CLAUDE.md` + 两个常驻规则;打开
      `internal/detect/` 下任一文件后 `detect.md` 出现在已加载列表

## 不做什么

- 不改写、不去重、不重排任何一段;搬的时候发现过时或矛盾的句子,**记下来开 issue 或 proposal**,不顺手改
- 不动 `plugin/`、`docs/` 正文(只改 `docs/README.md` 和 `process.md` 里指向 `CLAUDE.md` 的那一行,说明规则文件的存在)
- 不加 `.claude/skills/`(005)、不加 `.claude/settings.json`
- 不处理工作区那份 `../CLAUDE.md`

## 不能说什么

不适用于报告文案。对本条:`CLAUDE.md` 新增的那张表只写"哪个文件管哪个包",**不写"完整覆盖"** ——
按 glob 加载意味着改一个没列进任何 glob 的文件时,只有常驻的两份在场。

## 工作项

| W | 一句话 | 提交 |
|---|---|---|
| 1 | 规则文件的 scope 测试(含反向断言),对当前树跑:`.claude/rules/` 不存在时应当直接通过,不算红 —— 本条的"红"是 W2 搬完后 glob 写错时 | `test(rules): every .claude/rules file scopes to existing paths …` |
| 2 | 13 个段原样搬出,加 frontmatter;`CLAUDE.md` 留头部 + 常用命令 + 规则表 | `docs(claude): move per-package guardrails into .claude/rules, no content change` |
| 3 | 规则文件内的相对链接改到新深度;`docs/README.md`、`process.md` 指向更新;`.gitignore` 加 `.claude/settings.local.json` | `docs(claude): repoint links from the rules files …` |

## 未决问题

1. **`invariants.md` 和 `conventions.md` 常驻还是限定 `**/*.go`?** 建议常驻:「约定」里有提交信息、双语文档、
   SPDX 的规则,不只管 Go 文件。**已决(2026-09-16):常驻。**
2. **`pipeline.md` 的 `paths:` 是 `internal/**`**,等于改任何内部包都加载它 17 KB。它讲的是阶段先后顺序,
   改任何一个阶段都该知道,所以宽是有意的。想收窄就只留 `cmd/aguard/**`。**已决(2026-09-16):保持 `internal/**`。**
3. 搬的过程中如果发现段落之间互相引用("见上面 npm 那段"),引用文字**不改**,只在被引用处文件名可推断。
   真有读不通的,记下来,不在本条修。
4. **`docs/architecture.md` 双语对子各改了一句**("不变量列表在 CLAUDE.md 里"→ 在 `invariants.md` 里),
   `work-items` §0 那句"读 CLAUDE.md(80 KB)"也改了指向。这三处不在「不做什么」列的白名单里,但都是指向搬走内容的
   指针,和链接同性质;不改就是三处过时说明。在此披露。
5. **常驻两份没有 frontmatter,只加了一行 HTML 注释**说明"没有 paths 是有意的",否则读者会以为漏写。
6. **文件名用英文**(`detect.md`、`npm.md`),和目录下其他文件名一致;段标题原样保留在文件第一行。

## 完成

```
合入:0444568(2026-09-16,ff 合入 dev;分支已删)
发布:不随版本发布
证据:TestClaudeRulesAreScopedToExistingPaths + TestClaudeRulesProblemsAreCaught(cmd/aguard/claude_rules_test.go);
      CLAUDE.md 90019 → 7213 B,会话常驻 17198 B;12 段拼接与拆分前逐字节相同(下面的命令在合入后的树上打印 True;
      第一版命令漏了归一化链接前缀,在合入树上打印 False,已修正);make test 20 包 ok、lint 0、rules 无漂移、自扫 exit 0
零改动核对命令(在合入后的树上跑,应打印 True):
  python3 -c "
import subprocess,re
o=subprocess.check_output(['git','show','8b4e269~1:CLAUDE.md']).decode()  # 拆分提交的父提交
cut=o.index('\\n## 流水线')+1
order=['pipeline','invariants','gate','score','detect','reputation','hash','report','judge','plugin','npm','conventions']
b=''
for f in order:
    t=open('.claude/rules/%s.md'%f,encoding='utf-8').read()
    t=t[t.index('\\n---\\n',4)+5:] if t.startswith('---') else t[t.index('\\n')+1:]
    b+=t.replace('](../../','](')   # 唯一允许的文本改动:链接上移两级,归一化后再比
n=open('CLAUDE.md',encoding='utf-8').read(); n=n[:n.index('\\n## 各包的防护点在')+1]
print(o[:cut].rstrip()==n.rstrip() and o[cut:]==b)"
```
