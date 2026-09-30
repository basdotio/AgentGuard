<!-- SPDX-License-Identifier: MIT -->
# 038 — 四处没人调用的代码在仓里占位:预留接口、兼容别名、只有测试用的导出、一次性工具

- **来源**:开源前清理扫描(2026-09-30),代码卫生一节
- **依赖**:无
- **分支**:`p/038-dead-code`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

按"导出标识符在非测试代码里没有调用方"全仓扫过一遍,四处成立,每处都在文档里被当成活的东西描述:

| 什么 | 在哪 | 状态 | 文档怎么说它 |
|---|---|---|---|
| `Sink` 接口 + `NoopSink` | `internal/sink/`(35 行) | 零调用方,只有自己的测试;注释写"P0 只有 Noop,P1/P2 实现" | spec §10 整节、architecture 两份的包表和"未发布"清单、ROADMAP 两处、conventions.md |
| `GeneratedDir` / `IsGeneratedDir` | `internal/collect/skip.go:77-84` | 拆成 `ExcludeFromHash` / `ExcludeFromScan` 时留的别名,零调用 | architecture 两份、hash.md、detect.md、pipeline.md、ROADMAP、issues/003、issues/019 都写 `collect.GeneratedDir` |
| `(*Matcher).SuppressesItem` | `internal/ignore/ignore.go:163` | 只有 `ignore_test.go` 用;生产代码走 `ApplyItems` | — |
| `hack/measure-rules` | 187 行 | Makefile 和 CI 都不引用,P-005 时的一次性测量;量测台的 per-rule 诊断已覆盖它 | detect.md 一处、一个测试注释、adapter 一处注释 |

它们不坏,但公开仓里每一处"预留"都是读者要去理解的东西,而这四处理解完的答案是"没用"。别名尤其误导:文档教人看 `collect.GeneratedDir`,
而代码里真正回答问题的是两个名字不同的集合。

**没动的一处**:LLM 提供方预设表(`config.go` 的 deepseek / openai / qwen)。扫描把它列为候选,但 `aguard llm setup --provider deepseek`
写在 README、docs/llm-judge 和插件命令里,是用户在用的入口;删它是产品决定,不是清理,另议。

## 初步方向

删前两处和第四处,第三处改为非导出;所有提到它们的文档改成真名或删掉那句;spec §10 不删原文,顶上加"已移除"说明,保留设计意图。

## 完成的判据

- [ ] `go build ./... && go vet ./...` 绿;`internal/collect`、`internal/ignore`、`internal/model`、`internal/detect` 测试绿(反向断言:删掉零调用的东西,没有一个既有测试变红)。
- [ ] `grep -rn 'internal/sink\|NoopSink\|GeneratedDir\|IsGeneratedDir\|SuppressesItem\|measure-rules'` 在 `.go` 与非历史 `.md` 里只剩三类命中:spec §10 的"已移除"说明与原文、ROADMAP D.11 的解释、issues/003 的删除记录。proposals / decisions / planning 里的历史提及不改。
- [ ] `make verify` 绿(含文档链接检查:删掉的目录不再被任何活链接指向)。

## 不做什么

- 不动 LLM 预设表(见上)。
- 不删 `baselines/isolate/`(无使用者,但它是 heeler / 容器化的预留,归 results 清理那条一起定)。
- 不改注释里的 P-NNN / W-NNN 引用(另一条)。
- 不动 `internal/` 的任何行为;删除的四处没有一处在生产路径上。

## 不能说什么

不适用:没有对外数字。

## 工作项

| W | 一句话 | 提交信息 |
|---|---|---|
| 1 | 删 `internal/sink`、`hack/measure-rules`、别名;`SuppressesItem` 改非导出;三处注释 | `collect,ignore,model: remove the sink placeholder, the GeneratedDir aliases and a test-only export; drop hack/measure-rules (P-038)` |
| 2 | spec、architecture 两份、ROADMAP、四份 rules、issues/003、issues/019 改真名或删句;本文件;索引 | `docs: the removed placeholders leave the documents too — real names for the exclude sets, a removal note in spec §10 (P-038)` |

本条没有 W1 红测试:删除没有可钉的行为,反向断言就是既有套件不变红。

## 未决问题

无。四处的"零调用"都用 grep 复核过(见「问题」表)。

## 完成

```
合入:PR(2026-09-30;sha 合入后用 git log --grep P-038 找)
发布:待发
证据:go build / go vet / 四个包测试绿;grep 残留只剩 spec §10 说明、ROADMAP D.11、issues/003 三类;make verify 绿(链接检查过);
     反向断言 = 既有测试零变红(internal/collect、internal/ignore、internal/model、internal/detect)
```
