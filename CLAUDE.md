<!-- SPDX-License-Identifier: MIT -->
# CLAUDE.md

本文件为 Claude Code (claude.ai/code) 在本仓库中工作时提供指引。

本仓库是 `aguard` 的 Go 实现(module `github.com/basdotio/AgentGuard`),**所有代码都在这里**。

> **module path 有三处必须始终一致**:`go.mod` 的 module 指令、每个 `.go` 文件的 import、
> 以及 `hack/github-action.yml` 里那条 `go install`(它会被拷到分发仓库,所以那边也要跟着动)。
> 少改一处的表现不是测试变红,是**编译不过**。
> 改 module path 之后**必须确认 `go.mod` 第二行仍是 `go 1.23.5`** —— 见下面「约定」里那条:
> 任何让 `go mod tidy` 顶高 go 指令的东西都会让 CI 编译失败,而 `make test` 在自动切换后的
> 工具链上照样全绿,绿灯在这里不构成证据。规格源头是本仓库的 [docs/spec/spec.zh-CN.md](docs/spec/spec.zh-CN.md) ——
源码注释里的 "spec §N" 全部指向它;改动带不变量的代码前,先去读被引用的那一节。

**本文件与其余文档分工不同,别混。找东西按这张表走:**

| 文档 | 写给谁 | 内容 |
|-----|-------|------|
| 本文件(`CLAUDE.md`) | 改这些代码的人(含 agent) | **不要怎么改**:防护点、历史上错过的地方、削弱哪条会重新打开哪个假阴性 |
| [docs/architecture.md](docs/architecture.md) | 新加入的同事 | as-built 地图:流水线、包划分、不变量、当前状态。双语对子 |
| [docs/rules.md](docs/rules.md) | 报告的读者 | 规则参考,**由 `make docs` 从代码生成**,CI 校验不漂移。**规则条数以它头部的计数块为准** —— 本仓库别处写下的条数都是副本 |
| [ROADMAP.md](ROADMAP.md) | 想知道做过什么的人 | 已发布的、已记录但未排期的、以及愿意公开承认的限制 |
| [issues/](issues/README.md) | 要动某块代码的人 | **确认存在的缺陷、绕过、覆盖缺口,以及否决记录**(试过什么、为什么失败、代价多少)。带封闭的状态取值集 |
| [docs/spec/spec.zh-CN.md](docs/spec/spec.zh-CN.md) | 改带不变量的代码的人 | **规格源头**:什么必须成立;源码注释 `spec §N` 的目标。改了不变量或数据模型要同步它 |
| [docs/corpus-benchmark.zh-CN.md](docs/corpus-benchmark.zh-CN.md) | 做语料的人 | benchmark 的口径与 schema 设计。**样本和打分器都在独立仓库 `../agent-artifact-corpus`**（许可隔离、工具中立）；本仓库只有驱动:`baselines/`(P-015;`baselines/cmd/baseline` 替代了 P-011 的 `hack/corpus-runner`),`make bench` 串起三步,**每次运行的账本与成绩单提交在 `baselines/results/`** |

改动本文件描述的结构时,顺手看一眼架构文档要不要跟(它是手写的;规则表是 `make docs` 生成的)。

**新发现的东西进哪份**:有复现但修法未知、或试过被否决 → `issues/`;有复现有修法 → 直接修,修法写在改动里;
推迟的方向 → `ROADMAP.md`。判据一句话:**这条修完之后,这段文字还有没有价值?** 有 → `issues/`,没有 → 不留档。

## 常用命令

```bash
make build                       # -> bin/aguard(CGO_ENABLED=0,version/commit/date 由 -ldflags 注入)
make test                        # go test -race -cover ./...
make lint                        # golangci-lint run ./...(govet staticcheck errcheck ineffassign unused gofmt goimports misspell)
make docs                        # 从代码重新生成 docs/rules.md(加/改规则后必跑,CI 会校验)
make verify                      # 一条命令跑完整个闸门(vet test lint docs漂移 plugin自扫 go指令),交付前必过
make hooks                       # 装 pre-commit 到 .git/hooks/(clone 后跑一次;没装的机器拦不到)
make dist                        # 交叉编译到 dist/(darwin/linux;windows 见下)
go vet ./...                     # CI 也会跑

go test -run TestFailGate ./cmd/aguard/                # 跑单个测试
go test -race ./internal/detect/                       # 跑单个包

./bin/aguard scan --root ~/.claude          # 全量扫描
./bin/aguard scan --html /tmp/r.html --json
./bin/aguard check ./some-skill --md -      # markdown 版报告到 stdout,贴 PR 评论用
./bin/aguard check ./some-skill             # 单个 artifact 闸门(默认 --fail-on high)
./bin/aguard hash ./some-skill              # 打印 canonical 信誉哈希

./bin/aguard hook install --dry-run         # 加载时闸门:看它会往 settings.json 写什么
./bin/aguard approvals                      # 闸门已经信任了哪些内容(key 是 canonical 哈希)
echo '{"hook_event_name":"PreToolUse","tool_name":"Skill","tool_input":{"skill":"x"}}' \
  | ./bin/aguard hook                       # 手动喂一个 hook 事件(调闸门时最快的回路)
```

退出码:`0` 低于阈值 · `1` 有 ≥ `--fail-on` 的发现 · `2` 运行错误 · `3` 仅 `clean`:部分执行 ·
`4` 仅 `--fail-on-llm`:判官没跑或跑短了(有调用失败/没发出),闸门无法评估(P-026)。`scan` 默认不设
`--fail-on`(仅告知);`check` 默认 `high`(闸门)。


## 各包的防护点在 `.claude/rules/`

本文件只留文档地图和常用命令。**不要怎么改**那些段按包放在 `.claude/rules/`,
带 `paths:` 的只在读写匹配文件时加载,不带的会话开始即加载。

| 文件 | 原段 | 何时加载 |
|---|---|---|
| [pipeline.md](.claude/rules/pipeline.md) | 流水线 | `cmd/aguard/**`、`internal/**` |
| [invariants.md](.claude/rules/invariants.md) | 不变量 —— 不要削弱 | 始终 |
| [gate.md](.claude/rules/gate.md) | 加载时闸门(`internal/gate`) | `internal/gate/**`、`cmd/aguard/gate*.go`、`hack/pre-commit` |
| [score.md](.claude/rules/score.md) | 评分(`internal/score`) | `internal/score/**` |
| [detect.md](.claude/rules/detect.md) | 检测引擎(`internal/detect`) | `internal/detect/**`、`hack/gen-rules/**` |
| [reputation.md](.claude/rules/reputation.md) | 声誉白名单(`internal/reputation`) | `internal/reputation/**`、`hack/reputation-refresh/**` |
| [hash.md](.claude/rules/hash.md) | Canonical 哈希(`internal/collect/hash.go`) | `internal/collect/**`、`internal/detect/contenthash*.go` |
| [report.md](.claude/rules/report.md) | 终端报告的两种模式(`internal/report/text.go`) | `internal/report/**` |
| [judge.md](.claude/rules/judge.md) | LLM judge(`internal/judge`,可选) | `internal/judge/**` |
| [plugin.md](.claude/rules/plugin.md) | Claude Code 插件(`plugin/`) | `plugin/**`、`.claude-plugin/**` |
| [npm.md](.claude/rules/npm.md) | npm 分发(`npm/`,`make npm-dist`) | `npm/**`、`Makefile`、`.github/**` |
| [conventions.md](.claude/rules/conventions.md) | 约定 | 始终 |
