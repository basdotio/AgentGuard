<!-- SPDX-License-Identifier: MIT -->
# 005 — 我们专门为之写规则的那四个恶意样本，闸门一个都没拦住，还把它们记成了"已信任"

- **来源**：新发现（2026-09-16，`agent-guard-corpus` 接入 `trailofbits/overtly-malicious-skills` 的第一次实测）
- **依赖**：无。与 W-026（误报率）耦合，见「未决问题」
- **分支**：`p/005-gate-lets-the-evasion-corpus-through`

## 问题

`.claude/rules/detect.md`（原 `CLAUDE.md` 形状检查一节）写着 `shape.go` 的四条规则是对着
Trail of Bits 那四个样本写的，一条规则对一个样本，原文：

> 四条各对应 Trail of Bits `overtly-malicious-skills`(2026-09-08) 里一个 4/4 绕过逐行规则的样本

实测结果是：**四条规则全部按设计命中，四个 skill 也全部通过闸门。**

| 样本 | 分数 | 命中 | `check` 默认退出码 |
|---|---:|---|---|
| `context-loader` | 88 | `OBF-006`/medium | **0** |
| `csv-summarizer` | 83 | `EXFIL-004`/medium + `OBF-007`/low | **0** |
| `dev-env-setup` | 88 | `SUP-006`/medium | **0** |
| `simple-formatter` | 88 | `SUP-005`/medium | **0** |

四条发现全是 medium，闸门在 high 上拦。所以**报告层召回 4/4，闸门层召回 0/4**，
而我们一直只算过前一个数。

走真实闸门回路（`aguard hook` 吃 `PreToolUse[Skill]` 事件，不是 `check`）能看见后半截：

```
{"systemMessage":"AgentGuard: skill \"dev-env-setup\" 88/100 (Low) · no finding at or above
 the threshold · trusted from now on for content b79d52646053…"}
[exit=0]
```

**`trusted from now on` 是更坏的那一半。** 它不是放行一次，是按内容哈希**写进批准库**，
以后同样的字节连问都不会问。这和 W-001 的结局相同，成因相反：那次是扫描器没拿到字节，
这次是**拿到了、报对了、然后判它不值得拦**。

这个形状**既不是组 A 也不是组 F**。组 A 是没读到，组 F 是规则太窄认不出 —— 这里规则认出来了。
它是**检测与拦截之间的定级落差**，本仓库此前没有这一类。

复现（语料在 `../agent-guard-corpus`）：

```bash
cd ../agent-guard-corpus && make fetch E="trailofbits-overt"
cd ../agent-guard
for d in ../agent-guard-corpus/cache/trailofbits-overt/skills/*/; do
  ./bin/aguard check "$d" --no-reputation >/dev/null 2>&1; echo "$(basename $d) exit=$?"
done
```

## 完成的判据

- [x] ~~四个 ToB 样本在闸门回路下全部 `Blocking`~~ **改为（W1 测量后，2026-09-16）**：三个 `Blocking`（形状规则提 high），
      `csv-summarizer` 的 medium 是 `EXFIL-004`（组 C 的规则，不在本条范围）→ 放行但**不记信任**、每次重审（W2）。
      四个都**不写入 approvals**：断言批准库在四次调用后仍为空 ✅（`TestPassWithMediumFindingIsNotRemembered` 钉 medium 放行那一侧，
      Blocking 一侧本来就不写）
- [ ] **反向断言（硬约束）**：同一组本机 952 个真实 skill，修前修后 `--fail-on high` 拦下的**个数不得增加**。
      写个数不写比率：8012054 已撤回 12.9% 那个率（952 是一台机器的安装历史，不是语料，不能当分母）。
      任何把四个样本拦下来、同时让这个个数上升的改法都不算修好
- [ ] **批准库不给带 medium 及以上发现的内容写"信任"**：`aguard hook` 放行一个有 medium 发现的 skill 时不写 approvals，
      下次同样字节仍会问；零发现的仍照旧写。断言：对四个样本各调一次 hook 后 approvals 为空（无论它们当时是否被拦）
- [ ] **反向断言**：`anthropics/.../security-guidance`（Apache-2.0，注册成 hook 的防注入
      skill，实测 88/100 零 high）仍然不被拦
- [ ] 语料侧：四个样本在 `agent-guard-corpus` 的 `manifest/` 里（`trailofbits-overt`，
      **无 license，永不 vendoring**），第 1 层配最小复刻件与良性孪生，`make validate` 绿
- [ ] `make verify` 绿

## W1 测量结果（2026-09-16）

工具：`hack/measure-rules`，吃 `aguard scan --json` / `check --json`，按规则数发现与 artifact，按阈值数被拦 artifact
（走 `score.Deterministic`，LLM 发现不算，和闸门同一个判据）。

**这台机器**（`~/.claude`，33 个 artifact，80 个 SKILL.md；**不是** 952 那台）：

```
artifacts 33 · blocked at >=high: 1 (aguard-selftest — 自测 fixture)
OBF-006 0 · OBF-007 0 · SUP-005 0 · SUP-006 0
```

**四个 ToB 样本**（clone 到会话临时目录，不进任何仓库）：

```
artifacts 4 · blocked at >=high: 0        blocked at >=medium: 4
OBF-006 1/1 · OBF-007 1/1 · SUP-005 1/1 · SUP-006 2/1
```

**W3 之后（同一批样本、同一台机器）**：

```
ToB: artifacts 4 · blocked at >=high: 3 (context-loader, dev-env-setup, simple-formatter)
     分数 88/83/88/88 → 75/83/75/75；check 默认阈值退出码：context-loader=1 csv-summarizer=0 dev-env-setup=1 simple-formatter=1
本机: artifacts 33 · blocked at >=high: 1（不变，仍只有自测 fixture）
```

两个结论改写了 W3 的范围：

1. **`csv-summarizer` 的 medium 是 `EXFIL-004`，不是形状规则**；它的形状命中 `OBF-007` 是 low。只提四条形状规则到 high，
   闸门层最多拦 **3/4**。第四个要么靠 `EXFIL-004` 定级（那是 W-008 修过误报的规则，不在本条范围），要么靠 W2
   （不记信任，每次重审）。W3 按 3/4 做，写明第四个的去向。
2. 952 那组样本在**另一台机器**（8012054 的作者机）。硬约束"拦下个数不增加"要在那台机器上用同一条命令跑修前修后：
   `aguard scan --root ~/.claude --json > before.json`，切到本分支重建后再跑一次，`go run ./hack/measure-rules before.json after.json`
   逐个比。这是检查点 2 要人做的一步。`SkillMD-138K` 本机没有，本条不等它。

## 不做什么

- **不单独把四条规则提到 high。** 那是三个可选杠杆里最直接的一个，但没量过误报面之前
  改它就是拿 W-026 换 W-028。证明"确实没动"：`git diff` 里 `rules_data.go` 与 `shape.go`
  的 severity 字段未变，或变了且附带 952 样本的前后拦截率
- **不单独把闸门阈值降到 medium。** 一行改动，会把 W-026 那 12.9% 的误报**全部变成拦截**
- **不动 W-026 的修法方向。** 组 C 正在同一个里程碑里往相反方向修，两边同时动会互相掩盖
- **不碰 approvals 的数据格式。** 本条只改"要不要记"，不改"怎么记"
- **不把 ToB 样本 vendoring 进任何仓库。** 无 license = 零授权

## 不能说什么

- **不要写成"提高了后门检出率"或"增强了形状检查"。** 检出率一直是 4/4，没有提高。
  要按这个写：**一套专门为某个语料写的规则，在那个语料上拦截率是 0/4，而我们以为是 4/4。**
- **不要说"现在覆盖了 Trail of Bits 的样本"** —— 在本条修完之前，这句话在报告层为真、
  在闸门层为假，而闸门层才是产品承诺的那一层
- 修完之后**也不要**说"闸门能拦住在野恶意 skill"。四个样本是逃逸试金石，不是分布

## 工作项

阶段 1 由 AI 拆出，一条一个提交：

| W | 一句话 | 提交信息（不写 sha） |
|---|---|---|
| 1 | 测量：四条形状规则各自在 952 本机样本（与可得的 SkillMD-138K 子集）上的命中数，脚本 + 结果表进 proposal | `hack(measure): per-rule hit counts for the four shape rules on the local 952 …` |
| 2 | 批准库写入策略：有 ≥ medium 发现的放行不记 approvals；测试先红 | `gate: a pass with medium findings is not recorded as trusted …` |
| 3 | 按 W1 的数：命中数≈0 的规则 medium → high，每条配反向断言（对抗语料 + 良性孪生）；命中数高的不动并写明 | `detect: … shape rules gate at high, measured …` |
| 4 | 语料侧：四个样本进 `agent-guard-corpus` manifest（不 vendoring），`make validate` 绿 | 语料仓提交，本仓库只记链接 |

## 未决问题

阶段 2 攒下、阶段 4 一起问的。**但这一条必须在阶段 1 就答**，因为它决定做什么：

**三个杠杆选哪个（或哪几个）？**

1. **规则定级**：`OBF-006`（扩展名与魔数不符）、`OBF-007`（空行填充）、`SUP-005`（随包 `.pyc`）、
   `SUP-006`（改包源）medium → high。理由是这四条是**结构性判据不是启发式共现** ——
   随包 `.pyc`、`.txt` 里装 ELF，都不是正常 skill 的形态。风险：误报面未量。
2. **组合升级**：单条 medium 不拦，**两条不同维度的 medium 共现**才拦。
   风险：四个样本里三个只有一条 medium，**这个方向单独不够**。
3. **闸门阈值**：`cfg.Gate.FailOn` 本来就独立于 `check` 的默认值。
   风险：见「不做什么」。

**已决（2026-09-16）**：杠杆 1，先量再提（W1 → W3）；杠杆 2 不单独做；杠杆 3 不做。
外加一条三个杠杆之外的：批准库不给带 medium 的内容写"信任"（W2），它和定级无关，先落地。

原建议是 **1，且必须先量误报面**：先在 952 本机样本 + `SkillMD-138K` 上测这四条规则各自的
命中率，命中率接近 0 的那几条提到 high 几乎无代价；命中率高的那条说明它本来就不适合 gate。
**这个测量本身是 proposal 的第一个工作项，不是前置调研。**

## 交付前要人知道的事（2026-09-16，AI 填）

4. **W4（语料仓 manifest）没做**：`agent-guard-corpus` 不在这台机器上（`ls ..` 没有），本条在本仓库只记链接的承诺兑现不了。
   留给有语料仓的那台机器，作为合入后的独立提交；不阻塞本条合入，因为本仓库的四个判据都不依赖它。
5. **三条判据要在 952 那台机器上跑**：(a) 硬约束"拦下个数不增加"——`aguard scan --root ~/.claude --json` 修前修后各一份，
   `go run ./hack/measure-rules before.json after.json`；(b) `security-guidance` skill 仍不被拦（本机没装）；
   (c) 四条规则在 952 上的命中数（本机 80 个 skill 上全零）。**这三条是检查点 2 的一部分**，我在这台机器上验不了。
6. `OBF-007` 不提级、`EXFIL-004` 不动，是 W1 量出来之后的决定，不是原草稿的："闸门层 4/4" 改成 "3/4 + 第四个不记信任"。
   如果你想要 4/4，路径是 `EXFIL-004` 定级，那要另开一条并带 W-008 那次误报修复的反向断言。
7. `make verify` 在 W3 提交前报过一次 rules.md 漂移：`make docs` 生成的文件在工作树里没暂存，verify 比的是 index。
   `git add` 后在已提交的树上重跑为绿。漂移检查本身没错，是我的顺序错。
8. 前面一次循环里打印的 `exit=0` 是 shell 里 `$?` 被命令替换覆盖，不是真值；重新单独跑：
   context-loader=1 csv-summarizer=0 dev-env-setup=1 simple-formatter=1。
9. 分数从 88 到 75 是三条 high 的副作用，报告层不该拿它说"检出率提高"——检出率一直是 4/4，见「不能说什么」。

## 完成

```
合入：634ff6d（2026-09-16，ff 合入 dev；分支已删）
发布：v0.10.0
证据：TestPassWithMediumFindingIsNotRemembered + 三条邻测（internal/gate/memory_test.go）；
      TestShapeRulesGateAtHigh + TestPaddingStaysLow（internal/detect/shape_severity_test.go）；
      hack/measure-rules 的两条测试；ToB 四样本 blocked@high 0 → 3，分数 88/83/88/88 → 75/83/75/75，
      check 退出码 1/0/1/1；本机 33 artifact blocked@high 1 → 1，四条规则命中 0 → 0；make verify 绿
未验证（本机做不了，等 952 那台机器）：硬约束"拦下个数不增加"；security-guidance 不被拦；四条规则在 952 上的命中数
未做：W4 语料仓 manifest（语料仓不在本机）
```
