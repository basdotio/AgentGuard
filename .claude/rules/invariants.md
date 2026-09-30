<!-- SPDX-License-Identifier: MIT -->
<!-- 常驻规则:没有 paths,会话开始即加载(P-003) -->
## 不变量 —— 不要削弱

这些是本工具可信的根据;其中多条在**不止一处**被强制执行,改动时必须**全部**一起改。

1. **绝不执行被扫描内容,绝不外连**(除显式开启的 LLM judge)。全程只读。
2. **符号链接边界收敛,出错即拒(fail-closed)。** `collect.withinDir` 与 `detect.inBoundary` 都会
   先解析符号链接再判断越界。skill *内部*文件不得指向 skill root 之外;skill 目录*本身*是符号链接
   属于合法的安装方式,但解析后必须落在 `$HOME` 之内(否则报 `SCOPE-001`)。解析失败一律拒绝。
   同一条边界还管着另外两处**配置里写死的路径**(它们同样由攻击者可控的文件提供):插件的
   `installPath`(`collect.collectPlugins`)和 hook command 里引用的脚本(`detect.hookUnits`)。
3. **脱敏只有一个收口。** `detect.Redact` 是产出 snippet 的唯一途径,并且在所有地方都**先脱敏再截断**
   (`boundedRedact`、`behaviorExcerpt`)—— 否则跨越字节上限的 secret 会以残片形式漏出去。报告与
   LLM judge 消费的是同一份已脱敏视图。
4. **两个分数:`Overall` 纯确定性,`OverallEffective` 含 LLM 单向升级**(spec v1.1 §5.2.1/§5.3,
   已实现)。铁律 #1 曾是"永不改分",那是个**双向**禁令 —— 而注入攻击想要的只有**降分**那一个方向,
   禁死升分等于自缚。现在放松为**单向**:LLM 只能把风险往上推,于是一次成功的注入只能让攻击者自己的
   artifact 更可疑,**攻击收益为负**。
   - **单向性由公式结构保证,不靠约定**:`score.Apply` 里 `OverallEffective = min(Overall, 升级分)`,
     artifact 级同理。配有 `TestApply_EffectiveNeverExceedsOverall` 恒成立断言。
   - **过滤谓词只有一份**:`score.Deterministic`(排除 `SrcLLM` 与 `Dimension==0`)和 `score.Escalating`
     (还要求 `Escalates`)都是导出函数,`score.Apply` 与 `report.HasAtLeast`/`HasAtLeastEffective`
     直接调用。**不要再复制一份条件然后靠注释同步** —— 漂移在这里是不可表达的,这正是当初把它们提出来
     的原因。
   - **升级资格是逐条 finding 的**:必须过**证据落地**(`judge.Ground`,回填真实 `file:line`,落不了地
     就丢弃并计入 `LLM-005`)**加 k-of-n 共识**(`llm.samples`,多数才算)。两道都过才置 `Escalates`。
   - **`llm.authority` 管的是闸门,不是分数**:`overall_effective` 永远照算照显示,`authority: escalate`
     只决定 `--fail-on-llm` 能否生效。(规格原来把它写成第三个"合格前提",那样默认档下两个分数恒等,
     "先看两个数再决定放不放权"就没有数可看 —— 规格已按实现修正。)
   - **铁律 #2 绝对不动**:judge 只能*新增* `Source=llm` 的发现和展示用的 `ArtifactReport.Advisory`
     标签,**永远不能删除、降级或重排一条静态发现**。`--fail-on` 只看确定性发现,任何模型输出都改不了
     它的答案。
   - `clampSeverity` 把模型自评上限压到 `high`;`clampLabel` 对未知值取 `likely-real`(安全侧);
     `LLM-007`(artifact 试图操纵分析器)的严重度**由工具定死,不采纳模型自评** —— 被劫持的模型当然
     会把自己评低。
5. **任何遗漏都不许静默。** 每一处覆盖缺口或抑制都要产出一条 dimension-0 note:
   `IO-000` `COV-000` `PARSE-000` `SCOPE-001` `IGN-000` `REP-GOOD` `LLM-000` `LLM-002`。抑制类 note
   (`IGN-000`、`REP-GOOD`)必须携带**被抑制项中的最高严重度**,否则"压掉了一个 critical"会读成一条
   低危脚注。
6. **并发不得改变输出。** `detect.Engine.Run` 按 artifact 并发,但结果按下标回写,因此 finding 顺序
   与串行执行完全一致。
7. **面向人的渲染器要清掉控制字符和 Unicode 方向/零宽字符**(`report.Sanitize`)—— finding 里带有攻击者可影响的文件名,以及
   (开 `--llm` 时)模型生成的文本。原来只清 C0/C1 控制符,U+202E 这类 bidi 覆盖原样通过:一个文件名叫 `pay<U+202E>gnp.sh`、
   内容是 `curl|bash` 的脚本,在终端里显示成 `pay2:hs.png`——带 high 发现的 `.sh` 把自己呈现成审阅者一眼跳过的 PNG(W-011,
   2026-09-15)。现在 Sanitize 复用 `detect.Invisible`(INJ-004 在文件**内容**里报的就是这张表,以前没有任何东西读文件**名**),
   替换成 U+FFFD 而不是删掉,让"清掉了"和"本来没有"分得开。**HTML 也要清**:`html/template` 只转义标记,不转义 bidi,所以
   HTML 从 `sanitizeResult` 的副本构建。**JSON/SARIF 不清**,那是给机器的,字节必须是磁盘上的字节。

