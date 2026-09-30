<!-- SPDX-License-Identifier: MIT -->
# E.1 语料与精确率/召回率 — 设计与实施

对应 `plan.zh-CN.md`(内部计划,已归档) 的 M1.3–M1.6。这份写清三件事：**这一层到底由什么组成、
要做什么、怎么做。**

> **分母只有一个来源**：`docs/rules.md` 头部的计数块，由 `make docs` 生成、CI 卡漂移。
> 本文件之外的任何地方写下的规则条数都是副本，而副本会无声地过期 —— 这正是这份文档的前身
> 把分母写成 57 长达三个月的原因。
>
> **而这一段自己犯过同一条（2026-09-22 修）**：它原本写着"分母是 61（计分规则）"，
> 就在"只有一个来源"这句话的同一句里。`docs/rules.md` 现在的计数块是 **73**。
> 一份警告别人别抄数字的文档，自己抄了一个并让它过期了三次发版。**这里不再写任何具体条数。**

---

## 现状（2026-09-22）

**这份文档是 2026-09-16 的设计稿，其中的排期部分已经被执行完毕，请按下表读：**

| 节 | 还成立吗 |
|---|---|
| §2 五个口径问题 · §3.0 分母纪律 · §3.5 污染图 · §3.6 数据质量陷阱 · §5 两个设计决定 | ✅ **一个字不用改**，这是本文最值钱的部分 |
| §3.1–§3.4、§3.7–§3.9 语料调研与推荐工作集 | 📄 **历史记录**。语料已建成:3,539 个样本在 `../agent-artifact-corpus`（300 恶意 / 3,220 良性 / 19 难负例），来源与许可以那边 `manifest/corpora.yaml` 为准 |
| §4.1 布局 · §4.2 schema · §4.3 打分器 | 📄 **历史记录**，都已在语料仓实现且与这里的设想不同（见 §4.3 上方的注） |
| §4.4 要发什么 · §4.5 阶段划分 · §4.6 CI | ⚠️ **逐节已更新**，见各节 |
| §6 依赖关系 | ⚠️ 指针已更新 |

**最新数字不在本文件里。** 在 ``plan.zh-CN.md` §3.1`(内部计划,已归档) 和提交进仓库的
`baselines/results/<工具>/<日期>/`（verdicts、覆盖账本、原样成绩单、`run.yaml`）。
本文件写口径,不写数字 —— 写了就会变成上面那段自己打自己脸的样子。

---

## 0. 先纠正一个前提：不是从零开始

`cmd/aguard/adversarial_test.go` 里的 `scanAttacks()` **已经是一份标注语料**——30 个场景，
每行带 `wantCaught` / `wantQuiet` / `wantNote` / `wantMissed`+`knownGap`，
`TestAdversarialCorpus_Coverage` 还强制"每行必须断言点什么"。标注语义、精度对照组、
已知空洞取反断言——这三件最难想清楚的事都已经做完了。

它缺的只有三样：

1. **规模**——30 个场景，其中良性精度对照组只有个位数；
2. **只能用 Go 表达**——`build func(t, home, root)`，非 Go 贡献者加不了样本，
   真实世界的 skill 也没法整棵树塞进函数体；
3. **只判定通过/失败，不做计数**——`t.Errorf` 回答"有没有回归"，不回答"命中率多少"。

所以 E.1 不是"造一个 benchmark"，是**把已有的断言式 harness 升级成计量式**，然后喂给它足够的数据。
这也决定了阶段划分：**先做度量口径和打分器（2–3 天），再收数据（数周）**。反过来做，
收回来的样本会因为标注字段不够而全部要重标。

---

## 1. 这一层由什么组成

四个可分离的部件，前三个是工程，第四个是时间。

| 部件 | 是什么 | 成本 |
|---|---|---|
| **① 标注词汇** | 一个"正例"精确指什么，一个"负例"允许出现什么 | 想清楚 1 天，是全局最关键的一步 |
| **② 语料格式** | 磁盘上的 fixture + `_label.yaml`，不再是 Go 函数 | 1 天 |
| **③ 打分器** | `make bench` → `docs/benchmark.md`，照 `hack/gen-rules` 的写法 | 2 天 |
| **④ 数据** | 恶意 ~40、良性 ~100+，每个都要能交代来源 | 3–5 周，不可加速 |

---

## 2. 五个必须先想清楚的口径问题

这一节是这份文档真正的内容。**大部分安全 benchmark 之所以没人信，都死在这五条上。**

### 2.1 "一个正例"有三种粒度，数字差得离谱，必须三个都发

| 粒度 | 问的问题 | 谁关心 |
|---|---|---|
| **按规则** | 标了 `INJ-001` 的样本，`INJ-001` 有没有响？ | 我们自己调规则 |
| **按样本** | 这个制品**有没有被拦下来**（≥ 阈值）？ | 用户。`hook` / `--fail-on` 的真实体验 |
| **按发现** | 全部语料上产出的 finding 里，有多少是对的？ | 噪音量，读报告的人 |

同一份语料，"按规则召回 90%"和"按样本召回 60%"可以同时成立（一个样本标了两条规则，
响了一条）。只发一个数就是在挑好看的那个。**头条数字必须是"按样本、在 `--fail-on high` 下"**——
因为那是门禁真正的行为。

### 2.2 "恶意"不等于"应该被我们检出"

一个 skill 可以以我们**明确声明不检测**的方式作恶（纯运行时行为、需要连 MCP server 才能看到的
工具投毒）。把这种算成召回失败，等于用一个我们写在 README 里的边界来惩罚自己，还会诱导我们
去填那个边界。

所以每个恶意样本必须带 `out_of_scope` 字段：非空即表示"确实恶意，但静态分析看不到，原因是——"。
这些样本**照样跑、照样计数，但不进召回分母**，单独一行报出来：

> 语料中 N 个恶意样本以静态分析无法观察的方式作恶（列出原因分布）。

这句话本身就是一个诚实且有信息量的结论，比把召回率做高更有说服力。

### 2.3 良性样本的期望不是"沉默"，是"不超过某个严重度"

**这是全篇最重要的一条。** 真实 skill 合法地 `curl`、读环境变量、调 `bash`、写文件。
一个好的扫描器对这些应该给 low / advisory，而**不是**什么都不说。

如果良性标注写成 `expect: nothing`，只有两个结局：要么我们为了刷分把规则挖空（真恶意也不响了），
要么我们宣布每个真实 skill 都是误报。两个都是灾难。

正确写法是 `max_severity: low` —— 允许出现 finding，**超过这个档才算误报**。
再配 `quiet: [具体规则]` 锁住那些"绝对不该在这个样本上响"的规则。

推论：**误报率必须按严重度分档报**。"良性集上产出 210 条 finding，其中 high 及以上 3 条" 是有用的；
"误报率 1.4%" 不是。

### 2.4 恶意样本的来源必须能交代，而且有三条红线

四种合法来源：

| 来源 | 做法 | 标注 |
|---|---|---|
| **promoted** | 现有 30 行 adversarial 转正 | 最省力，且已经是我们自己的 |
| **reconstruction** | 按公开披露的事件描述**重写**一个最小样本 | 必须注明是复刻、引原文链接、**不声称是原始 payload** |
| **synthetic** | 按 OWASP 十类各写最小可检样本 | 标 synthetic，不与真实事件混为一谈 |
| **real-world** | 公开仓库里确实存在的可疑样本 | 引 url+commit，且要能说明为何判恶意 |

三条红线：**不放可用的真实恶意载荷**（我们的样本必须是"形似而无害"——payload 指向
`evil.example` 这类保留域名，落地动作换成写标记文件）；**不抄别人的私有语料**；
**样本里任何像凭据的东西都必须是带固定假标记的假货**，否则我们自己的
`TestAdversarial_SecretNeverLeaksInSnippet` 会在自己的语料上炸。

### 2.5 三个会让数字失去意义的机制性陷阱

**过拟合。** 如果规则是调到语料通过为止的，语料就什么也没测。两条纪律：
新样本**先写标注、后跑扫描器**（`labeled_before_run: true`，写进 label 文件，
review 时看这个字段）；**样本永不因为失败而删除**——只能标 `out_of_scope` 并说明理由，
或者留在那里红着。

**规则集漂移。** P/R 随规则变，所以成绩单必须是**生成的**、头部钉住它测的是哪个版本。
~~CI 跑 `make bench` 并在数字变化时失败。~~ **CI 这一半已被否决（2026-09-22，见 §4.6）**，
纪律换了载体没换内容：成绩单是 `corpus score` 的原样字节、随基线提交，`run.yaml` 钉住
工具版本与语料 commit，改规则的 PR 必须贴前后两份 verdicts 的 diff。
让"数字悄悄过时"变成不可表达 —— 这一点和 `docs/rules.md` 的漂移检查仍是同一条纪律，
只是**看一眼**发生在 PR 上而不是 CI 上。

**假精度。** 40 个恶意样本的召回率，置信区间宽到 ±8 个百分点。所以**每个比率旁边必须写 n**，
并且**写分数不写百分比**：`34/40`，不是 `85.0%`。

---

## 3. 外部语料清单（2026-09-16 全面调研）

**五个 agent 并行调研，主会话核实了下列每一条标 ✅ 的许可与规模。**
取证分级：✅ 主会话核实 · ⚙️ agent 从 API/文件计数 · 📄 项目自述未独立验证。

### 3.0 先记住一件事：我们**还没有**误报率，只有一台机器上的一次冒烟

**这一节原来叫"我们自己的误报率是 11.0%"，后来改成 12.9%，两次都错在同一个地方：
把一台机器的安装历史当成了语料。**

全机 952 个 `SKILL.md` 不是 952 个独立样本，它们来自 **7 个来源**，而且高度集中：

| 来源 | n | high+ | 率 |
|---|---:|---:|---:|
| `gstack`（一个 bundle） | 418 | 65 | 15.6% |
| `everything-claude-code` | 412 | 50 | 12.1% |
| `personal`（本人手装） | 41 | 0 | **0.0%** |
| `claude-plugins-official` | 39 | 3 | 7.7% |
| `guard`（**我们自己**） | 18 | 0 | 0.0% |
| `nanoclaw-skills` | 13 | 4 | **30.8%** |
| `thedotmack` | 11 | 1 | 9.1% |
| **按样本加权** | **952** | **123** | **12.9%** |
| **按来源不加权** | **7 个来源** | | **10.7%** |

**两个来源占 87%。来源间极差 0.0% … 30.8%，比这个数字本身还宽。** 所以 12.9% 的真实含义
是"gstack 和 everything-claude-code 两位作者写作习惯的加权平均"，它测的不是 aguard。

**这正是本文档 W-022 那条纪律（source-disjoint 划分，Cisco 实测召回率 31.43% → 7.75%）。
我们给恶意语料写下了它，然后在自己的良性分母上原样犯了一遍。**

#### 分清两件事，否则会矫枉过正

| | 还成立吗 |
|---|---|
| **缺陷存在**：`SKILL.md` 里的配置示例被判 high | ✅ 逐条看过的真误报，与分母无关 |
| **误报率 = 12.9%** | ❌ n=7 的样本，不能发布 |
| **"闸门今天会拦下这台机器上 12.9% 的 skill"** | ✅ 成立，但只对这台机器、今天 |

**存在性证明和测量是两回事。** 本机扫描能做前者，做不了后者 —— 而这就是为什么第 1 层
必须接外部大规模良性语料（`SkillMD-138K` 的 20,556 个 repo、`clawhub` 的 41,743 条）。
不是"为了数字更好看"，是**因为 n=7 的样本上没有误报率可言**。

#### 本机扫描仍然要做，但它的角色是别的

- **回归**：同一台机器、同一批 952 个，修改前后跑两次，**差值**是有意义的（同一分母抵消了来源偏差）
- **定位**：分组差异本身是信息 —— 手装的 41 个 **0 条 high**，第三方分发的 911 个 **13.5%**，
  说明误报几乎全部落在**别人写给别人看**的教学型 skill 上
- **不能做**：任何形如"aguard 的误报率是 X%"的对外陈述

### 3.0.1 缺陷本身（证据，不是比率）

分布（`--no-reputation`，逐个 `check`，v0.9.0，无排除项）：
clean 766 · low 58 · medium 5 · **high 123**。导致 high 的规则（marketplaces 那 248 个里）：`EXFIL-001` ×23 · `EXEC-001` ×16 ·
`FS-003` ×10 · `EXFIL-003` ×5。证据**83 处里 64 处落在 `SKILL.md`**，几乎全是**教学文档里的配置示例**：

```
mcp-integration   SKILL.md:129   "url": "https://api.example.com/mcp",
                  SKILL.md:131   "Authorization": "Bearer ${API_TOKEN}",
plugin-structure  SKILL.md:247   "API_KEY": "${API_KEY}"
backend-patterns  SKILL.md:361   jwt.verify(token, process.env.JWT_SECRET!)
writing-rules     SKILL.md:274   pattern: rm -rf /tmp  # Only matches exact path
```

**结构性原因**：`roleDoc` 只跑维度 1，但 `SKILL.md` 是 `roleInstruction` 跑全部规则 ——
而它恰恰是最可能出现"如何配置某个 API"示例的那个文件。凭证腿 + 网络腿在一份**文档**里凑齐，
判成 high。

~~**这条先修。** 修完重跑这 952 个即可验证，不依赖任何外部语料。~~
**已完成（2026-09-21，P-012 + P-013）**，而且验证用的不是这 952 个，是语料上的 3,220 个真实良性样本：
`EXFIL-001` 在良性样本上的命中 **144 → 98 → 71**，良性池整体误拦 **219 → 176 → 147**（6.8% → 4.6%），
两步都是零回归。P-012 管 `SKILL.md` 里的 URL 字面量，P-013 管 `.mcp.json` 给自己服务器的鉴权头。

> **分母的纪律**（2026-09-16 吃过一次亏）：
> 第 0 层的分母是 **`find <root> -name SKILL.md` 的全部结果，不带任何排除项**。
> 第一版为了绕开一个性能缺陷用 `grep -v` 剔掉了 44%，而被剔掉的恰是最差的一组，
> 于是 12.9% 变成了 11.0%。**任何排除项都必须写进结论行，并同时给出带它和不带它的两个数。**

### 3.1 恶意语料

| 语料 | License | 恶意 | 良性 | 形态 | 取证 |
|---|---|---:|---:|---|---|
| **`DataDog/malicious-software-packages-dataset`** `samples/ai-skills/` | **Apache-2.0** ✅ | **204** ✅ | 0 | **真实目录树** | 唯一**在野 + 人工分诊**且许可干净的 skill 集 |
| `optimuslabs-io/skillsgoat` | **MIT** | 66 + 35 链 | 10 FP 诱饵 | 真实树 + hook + `settings.json` + MCP | 盲测、答案在树外、**错误算漏报** |
| `protectskills/MaliciousSkillBench` | 码 Apache-2.0 / 数据 CC-BY-4.0 ⚠️ | 7,505 ⚙️ | 2,235 ⚙️ | 树 + parquet | **聚合 13 个来源**，见 §3.5 |
| `cisco-ai-defense/mcp-scanner` `evals/` | **Apache-2.0** | 154 ⚙️ | 4 | 树（`.py`） | 目录即标签 |
| `Clay-HHK/skillcraft-audit` | **MIT** | 150 ⚙️ | 0 | 树 | **唯一**覆盖 hook 武器化 / 权限绕过 |
| `yoonholee/agent-skill-malware` | **MIT** | 124 ⚙️ | 223 | 文本 | 真实 ClawHub 战役 |
| `trailofbits/overtly-malicious-skills` | **无 license** ✅ | 4 | 0 | 树 | 逃逸试金石，**永不 vendoring** |
| `snyk-labs/toxicskills-goof` | **无 license** | ~7 | 1–2 | 树 + `.cursor/mcp.json` + zip | 见 §3.4 |
| `lxyeternal/MalSkillBench` | **无 license** ✅ | 3,944 | 4,000 | 树 | ⚠️ **标签泄漏**，见 §3.6 |
| HF `cuhk-zhuque/SkillTrustBench` | **CC-BY-NC-SA-4.0** ✅ | 2,863 + 1,014 | 1,643 | 树（37,721 文件） | 形态最好，许可最差。**是 HuggingFace 数据集，GitHub 上同名仓库 404**（2026-09-16 核实） |

### 3.2 良性语料 —— 我们最缺的那一半

**结论：良性不再是瓶颈。可及量是目标的 30 倍，真正的约束是许可与作者集中度。**

| 来源 | License | 规模 | 形态 | 良性依据 |
|---|---|---:|---|---|
| **HF `OpenClaw/clawhub-security-signals`** | **MIT** ✅ | **41,743 clean** | `skill_bundle_content` 可物化成真实树 | 银标准（gpt-5.5 自动判），**非人工** |
| **HF `FayeZC/SkillMD-138K`** | **CC-BY-4.0** ✅ | **138,133 / 20,556 repo** | 仅 `SKILL.md` 正文 | 真实世界分布，**最大的误报分母** |
| **awesome 清单链接目标并集** | 457/573 宽松 ⚙️ | **1,231 树 / 382 repo**（每 repo 上限 5） | **完整树含脚本** | **人写人收，无扫描器参与 —— 非循环** |
| `anthropics/claude-plugins-official` | **Apache-2.0** | 31 SKILL.md / 39 plugin | 完整树 | 第一方策展 |
| `NVIDIA/skills` | Apache-2.0 | 356 ⚙️ | 完整树，13.6 文件/skill | **唯一有密码学签名**（Sigstore，私有 PKI） |
| 厂商 repo（google/adobe/stripe/microsoft/forcedotcom…） | Apache-2.0 / MIT | 各 14–337 | 完整树 | 企业在自己命名空间下宽松发布 |
| `shenyimings/skillet` `benchmark/wild/` | **MIT** | 483 safe | 真实树，**repo-disjoint** | `label_source` 记录标签**怎么来的** |

**良性可信度分级**（从强到弱）：密码学背书（仅 NVIDIA）→ 企业宽松发布 → 官方市场收录 →
市场自身"非可疑"标记 + 下载量 → ⚠️ **"没被扫描器扫出问题" = 对我们是循环论证，一条都不能用**。

> **这条也咬到 `anthropics/skills`** —— 其中 6 个桌面版 skill 已在我们自己的 `reputation.json` 里，
> 对本工具**不独立**。而且它**是专有的**：逐 skill 的 `LICENSE.txt` 写着 "All rights reserved"、
> 明文禁止 "Reproduce or copy" 与 "Distribute" —— **硬红线**。
> （这反过来印证了我们按哈希钉住而不是 vendoring 内容的设计是对的。）

### 3.3 Hard negative —— 文件形态的"良性但像攻击"

| 来源 | License | 规模 | 为什么 hard |
|---|---|---:|---|
| HF `automatelab/mcp-servers-tool-catalog` | **CC-BY-4.0** | **9,922 工具 / 359 server** | 真实工具说明合法写着 `IMPORTANT:`、`<placeholder>`、token、URL。**我们的 `MCP-001..004` 目前只在 44 个工具上验过** |
| `DataDog/guarddog` `tests/.../benign/` | **Apache-2.0** | 25 | **每个良性文件自带注释说明它修的是哪次真实误报** |
| `ossf/package-analysis` `detections/*_test.go` | **Apache-2.0** | ~191 | **Go 表驱动，同语言同写法**。URL fixture 内联标出了他们自己判错的，含十六进制转义与 IDN —— 正是同形字咬过 `loopback.go` 的那一面 |
| `NVIDIA/SkillSpector` `tests/fixtures/` | **Apache-2.0** | ~6 对 | **成对孪生**：每个恶意 fixture 配一个近乎相同的干净版。唯一能测"命中的是差异而不是话题"的结构 |
| `mcp-guardbench` `cases/benign/` | **MIT** | 20 | 故意设陷：西里尔散文、`ignore` flag 文档、AWS **示例** key |
| `Agent-Threat-Rule/atr-skill-benchmark` | **MIT** | 466 | 含 `evasive-stub`，显式为误报设计 |

### 3.4 "防御性文字被误判"—— 全世界只有一个语料标注了它

这是我们已确认的一类真实误报（W-009）：一份 skill **教 agent 拒绝注入**，被判成攻击。

> **`SkillTrustBench` 的 `injected_d8` 子集：119 条**，官方描述原文
> *"security tools or test fixtures that should not be labeled malicious by default"*，
> 其中 118 条标 `normal`。**🚩 CC-BY-NC-SA，只能本地测，不能 vendoring。**

**除它之外公开世界零命中**（四路搜索：GitHub repo/code、HF、学术）。

**但有四个许可干净、零成本的真实替代品：**

1. **`anthropics/claude-plugins-official/plugins/security-guidance/`（Apache-2.0）** ——
   攻击模式正则 + 警告散文，**且注册成 hook**，会落在真实加载路径上。
   **✅ 2026-09-16 实测：我们给它 88/100，零 high** —— 这一类我们目前是通过的，
   应做成常设回归以防未来放松 `roleHookCmd` 时倒退。
2. **已在良性语料里的安全主题 skill** —— 约 72 个，零许可成本，标成专门的 FP 切片。
3. **`fevziegeyurtsevenler/prompt-injection-corpus`（CC-BY-4.0）** ——
   5 个 markdown，正文密集写着 `ignore previous instructions` 字面量。**所有 `INJ-*` 命中都是误报。**
4. **我们自己的仓库树** —— `CLAUDE.md` 已记录整棵树扫出 0/100，且插件 skill 引过一句字面注入
   吃到真实 `INJ-001`。零成本、可复现。

⚠️ **不要拿 over-refusal 基准**（OR-Bench、XSTest、FalseReject）当答案 —— 它们测的是
**模型过度拒绝**，不是**扫描器过度告警**，且全是 prompt 串。

### 3.5 污染图 —— 这些数字不能相加

```
MaliciousSkillBench ⊃ SkillTrustBench + MalSkillBench + ATR + SkillFortifyBench + …
SkillTrustBench     ⊃ overtly-malicious-skills（4 例）
DataDog ai-skills   ⊃ overtly-malicious-skills（tjade273-* 即 simple-formatter）
                    且 ~26/204（13%）是别家扫描器的合成测试集，不是在野样本
snyk-labs ∩ pyxeroai = 同源于 NET_NiNjA
```

**DataDog 还有作者集中问题**：81/204（40%）含 "openclaw"，其中一家约 40 个近重复。
**按 canonical 哈希或按作者去重再报数**，否则 recall 等于"抓没抓住一个模板"。

### 3.6 数据质量陷阱

**① `MalSkillBench` 有严重标签泄漏** ⚙️：

```
良性 4,000 个，3,878 个（97%）带 _meta.json
恶意 3,944 个，    0 个（ 0%）带 _meta.json
```
**不读任何 skill 内容就能 97% 分开两类。** 用前必须删掉那个文件。
（"恶意目录是空的"那半传言是**假的** —— 3,944 个全都有 `SKILL.md`。）

**② `skillfortifybench` 有捷径特征**：恶意样本用 RFC-2606 保留域名，良性零命中。
一行正则拿满分、真实世界 0%。**它的后继版本给了我们一个该抄的机制**：
`metrics/leakage.py` 是机械发布闸门 —— 任何支持度 ≥8 的结构特征若能以 ≥95% 纯度预测标签，
**直接让构建失败**。

**③ 一批"良性"标签是循环的**：Smithery `verified` / Glama `qualityScore`（**付费位**）/
majiayu000 的信誉分 —— 全是流行度或厂商策展，**没有一个是安全评审**。
ClawHub 尤其危险：它按 **OWASP Agentic Top 10** 评级，而那正是我们规则的来源分类法 ——
**用它的"clean"筛良性 = 用一个共享分类法的扫描器来选我们的测试集**。

### 3.7 许可红线

| 类别 | 项目 | 能 vendoring | 能本地测 + 发聚合数字 |
|---|---|---|---|
| **无 license** | ToB · snyk-labs · MalSkillBench · MCPTox · zast-ai · in_page · `anthropics/skills`(repo 级) | ❌ **零授权** | ✅ |
| **NC / SA** | SkillTrustBench(NC-SA) · obaydata(NC) · awesome-claude-code(**NC-ND**) | ❌ SA 传染 + NC 禁商用 | ✅ |
| **Copyleft** | OWASP Benchmark(GPL) · TruffleHog(**AGPL**) · thedotmack(AGPL) | ❌ 与"刻意规避 copyleft"同一条纪律 | ✅ |
| **专有** | `anthropics/skills` 逐 skill LICENSE：明文禁止复制、派生、分发 | ❌ **硬红线** | ✅ |
| **可用** | DataDog · SkillMD-138K · clawhub-security-signals · skillsgoat · guarddog · package-analysis · NVIDIA · 厂商 repo · awesome 链接目标（宽松那 457 个） | ✅ | ✅ |

**一条通用规律**：**无 license 在 vendoring 上比 NC/SA 更严**（零授权 vs 有条件授权），
但**在测量上两者相同** —— 本地跑 + 发聚合数字，落在所有这些许可的适用范围之外。

### 3.8 推荐工作集

| 角色 | 语料 | vendoring |
|---|---|---|
| ~~**第 0 层基线（现在就做）**~~ **已被取代** | ~~本机 952 个真实 skill，12.9% FP~~ —— **不要再引用这一行**：§3.0 刚论证过 12.9% 不是误报率（n=7 个来源），这里却把它列进"FP"列，是在推荐本文档自己否决的口径。真正的误报分母是语料里 4 个来源的 **3,220** 个真实良性样本 | 已在语料仓 |
| 在野恶意 | `DataDog/ai-skills`（204，Apache-2.0），按作者去重 | ✅ |
| 误报分母 | `SkillMD-138K`（138K，CC-BY-4.0）+ `clawhub-security-signals` clean（41,743，MIT） | ✅ |
| 完整树良性 | awesome 链接目标（1,231 树 / 382 repo，每 repo 上限 5） | ✅ |
| 能力探针 | `skillsgoat`（112，MIT） | ✅ |
| 逃逸试金石 | ToB（4）+ `nedlir`（1） | ❌ 现下现跑 |
| 防御性文字 FP | `security-guidance` + fevzi 5 个 md + 我们自己的仓库 | ✅ |
| 规则级微 fixture | `ossf/package-analysis` 的 ~191 条 Go 表驱动 | ✅ 照抄写法 |
| 外部验证 | SkillTrustBench（`coverage` 指标对上不变量 #5） | ❌ **NC-SA，永不 vendoring** |
| 该抄的机制 | `skillfortify` 的 `metrics/leakage.py` 泄漏闸门 | 抄想法 |

### 3.9 自己构建还是用已有的 —— 按**问题**分，不按语料分

这是 2026-09-16 定下的。问的是"我们应该自己构建还是用已经有的"，而这个问题
**问错了粒度**：它假设语料是一个东西。实际上我们要回答三个独立的问题，每个问题的
答案不同，混在一起就没有一个是对的。

| 问题 | 用什么 | 为什么不能反过来 |
|---|---|---|
| **误报率是多少** | **只能用已有的** | 自己写的"良性样本"是**想象中的良性**。真实 skill 的作者不知道我们的规则存在，所以他们会自然地在 `SKILL.md` 里贴一段 `curl` 配置示例 —— 而这正是 W-026 那 12.9% 的来源。自己写良性样本时，你**会下意识避开自己知道会响的形状**，于是测出来的误报率必然偏低，且偏低的幅度不可知 |
| **召回率是多少** | **已有的为主 + 自建补两处空白** | 见下面那张"我们独有的面到底有没有语料"的表。简版：恶意侧**有但很薄**（hook/权限/MCP 合计约 370 个样本，而 skill 侧是数千），良性侧**基本没有** —— 而**良性侧才是决定误报的那一半** |
| **改动有没有让事情变坏** | **只能自建** | 回归测试要的是**稳定、最小、意图明确**的样本。外部语料会更新、会下架、会重新标注，把回归门禁架在别人的仓库上，红灯就分不清是我们改坏了还是上游动了 |

**判据一句话**：**分母来自世界，分子来自我们，门禁来自我们自己。**

#### 我们独有的面到底有没有语料

**这张表是勘误的产物。** 本节初稿写的是"公开语料全部只覆盖 skill，这四类没有任何公开语料"
—— **那句话是错的，而且被同一份文件的 §3.1 直接反驳**（`skillsgoat` 的形态列写着
"真实树 + hook + `settings.json` + MCP"，`skillcraft-audit` 写着"**唯一**覆盖 hook 武器化 /
权限绕过"，两个都是 MIT）。写下它时我没有回头读自己十分钟前整理的清单。
**留着这段，是因为下一个人会犯同一个错** —— §3.9 的结论读起来比 §3.1 的表格省力，
而省力的那一份会被当成事实。

| 我们独有的面 | 恶意样本 | 良性样本 | 结论 |
|---|---|---|---|
| **hook**（`settings.json` 注册的 command） | `skillcraft-audit` 150（MIT，唯一专门覆盖武器化）· `skillsgoat` 部分 | ❌ 无 | 恶意够起步，**良性得自建** |
| **permission allow 条目** | `skillcraft-audit` 的"权限绕过"一类 | ❌ 无 | 同上。`PERM-006` 的可逃逸二进制表有 28 条，**一条都没有公开语料对应** |
| **MCP server 配置** | `cisco-ai-defense/mcp-scanner` evals 154（Apache-2.0）· `snyk-labs` 的 `.cursor/mcp.json` | 4（cisco） | 恶意可用，**良性 4 个不够做分母** |
| **connector 工具说明** | `MCPTox`（**无 license**，不可 vendoring） | `automatelab` **9,922 工具 / 359 server**（CC-BY-4.0）✅ | **唯一反过来的一格**：良性极充足，恶意拿不到 |

**所以"自建"的范围比初稿小得多，但方向也变了**：要自建的主要是**良性**样本，不是恶意样本。
这和 §3.9 开头那条"分母来自世界"看似矛盾，实际是它的例外并且必须说清 ——
**世界上没有人收集正常的 hook 和 permission 配置**，因为除了我们没人扫它们。
这一格只能自建，**而它恰恰是最容易把误报率做成一个好看数字的地方**，所以它的取样规则
要写死在 `testdata/corpus/README.md` 里：良性 hook/permission 样本**只能从真实机器上采**
（本机 + 同事机器 + 公开 dotfiles 仓库），**不许手写**。手写的良性 hook 和手写的良性 skill
是同一个毛病。

#### 这个决定不是推理出来的，是当场测出来的

写下这一节的当天，把 `aguard` 指向 `DataDog/malicious-software-packages-dataset`
的 `samples/ai-skills/`(204 个在野恶意 skill)，**第一个样本**就暴露了一个我们自己的
30 个对抗场景从来没有的形状：一个教科书级反弹 shell 得 **88/100**，`--fail-on high` 放行。



**这件事本身就是论证**：我们自己的对抗语料是**写规则的同一批人**写的，所以它系统性地
只包含我们已经想到的形状。这不是疏忽，是**结构性的** —— 任何自建语料都有这个性质，
再仔细也消不掉。外部语料唯一的、不可替代的价值就是它**不是我们写的**。

#### 三层，按这个顺序建

| 层 | 内容 | 回答什么 | 前置 |
|---|---|---|---|
| **1** | 本机 952 个真实 skill + `SkillMD-138K` + `clawhub` clean | 误报率 | 无，现在就能跑 |
| **2** | `DataDog/ai-skills` + `skillsgoat` + ToB/nedlir 逃逸样本 | 召回率(skill 那一半) | 第 1 层的口径 |
| **3** | `skillcraft-audit` + cisco evals（恶意侧，现成）+ **自建良性侧**：真实机器上采的 hook / permission / MCP 配置 + 每条已确认误报的良性钉子 | 召回率（我们独有那一半）+ 回归门禁 | 第 1、2 层暴露的缺口清单 |

**第 1 层必须先做完**，理由和 §4.5 阶段 0 同一条：它的产出(哪些规则从没被测过、
哪些误报是真的)就是第 3 层的采样清单。反过来先自建，一定建错方向 —— W-027 就是证据。

#### 一条纪律

**不允许"因为外部语料里没有，所以我们的规则没问题"。** 公开语料的覆盖面是它自己的
选题范围，不是威胁面。第 3 层存在的全部理由就是这个 —— 用它补面，**不是**用它自证。

---

## 4. 具体怎么做

### 4.1 语料布局

> **2026-09-16 起，语料在独立仓库 `agent-artifact-corpus`**（本机路径 `../agent-artifact-corpus`，
> 2026-09-21 前这里写的旧名 `agent-guard-corpus` 已过期），不在本仓库的 `testdata/corpus/`。拆出去的首要理由是许可：
> 第 1 层要 vendoring Apache-2.0 和 CC-BY-4.0 的内容，不该混进 MIT 的工具仓库。那边的 `docs/classification.md`
> 定义了 8 个测量类别，`docs/design.md` 是这一节的英文实施版，`docs/label-schema.md` 是下面 §4.2 的最终形态。
> **打分器也搬过去了**（`corpus score`，工具中立的算术，见那边 `docs/using-the-corpus.md`），和 §4.3 当初的设想不同；
> 留在本仓库的只有**驱动与结果**：`baselines/`（P-015）。`baselines/adapter/aguard` 负责把样本铺成
> aguard 认的布局、exec 二进制、按闸门谓词折叠成一个词；`baselines/cmd/baseline` 串起来，
> `make bench` 仍是入口。**它替代并删除了 P-011 的 `hack/corpus-runner`** —— 那个 runner 在摆放失败时
> 在执行二进制之前就返回，于是 127 个测试点从未被交给扫描器；现在每个点都被调用，落进
> `ledger.jsonl` 的 `scored` / `no-verdict` / `error` 三者之一，**没有 `skipped`**。
> 每次被引用的运行提交在 `baselines/results/<工具>/<日期>/`（verdicts、账本、原样成绩单、`run.yaml`）。
> 首轮真机结果见 P-010、P-011；分母从 173 变成 300 的原因见 P-015 与 `plan` §3.1。
> 下面的布局描述保留作设计依据。

```
testdata/corpus/
  README.md                     # 来源、如何加样本、三条红线
  manifest.yaml                 # 第二层：外部样本的 url+commit+sha256+标注
  benign/
    skills/real-gstack/         # 一整棵 skill 树
      _label.yaml
      SKILL.md
      scripts/…
    mcp/local-stdio-01/_label.yaml + .mcp.json
    instruction/claude-md-01/…
  malicious/
    skills/inj-hidden-frontmatter/
      _label.yaml
      SKILL.md
    hooks/… mcp/… permission/…
```

**两层设计（关于许可与离线）**：把第三方 skill 整棵 vendored 进仓库有许可问题，而
"下载后再测"又破坏离线。折中：

- **第一层 `benign/` `malicious/`** —— 我们自己写的 + 许可宽松的，**vendored，CI 离线跑**，
  发布的数字来自这一层；
- **第二层 `manifest.yaml`** —— 只存 url+commit+sha256+标注，`make bench-fetch` 拉到
  gitignore 的缓存里，**按需跑**。

`docs/benchmark.md` 必须写明每个数字出自哪一层。

### 4.2 `_label.yaml` schema

```yaml
id: mal-inj-004
class: malicious            # malicious | benign
kind: skill                 # 对应 model.ArtifactKind
entry: .                    # check 的目标；scan 类样本给出要铺进 root 的相对布局
origin:
  type: reconstruction      # promoted | reconstruction | synthetic | real-world
  source: "https://…"       # 披露文章 / 仓库 url+commit
  note: "按描述复刻的最小样本，非原始 payload"
  added: 2026-09-15
  labeled_before_run: true  # 先标注后跑，防过拟合
expect:
  rules: [INJ-001, EXFIL-001]   # 必须响
  min_severity: high            # 样本整体必须达到这一档（门禁视角）
  quiet: [PERM-006]             # 必须不响（精度对照）
out_of_scope: ""            # 非空 = 恶意但静态不可见，退出召回分母，单独报出
```

良性样本：

```yaml
class: benign
expect:
  max_severity: low         # 允许 low/advisory；超过即误报
  quiet: [EXEC-001, EXFIL-001]
```

### 4.3 打分器

> **历史记录（2026-09-22）。** 下面这套两段式**没有按这个样子实现**：打分器搬去了语料仓
> （`corpus score`，工具中立的算术），本仓库留的是驱动 `baselines/cmd/baseline`。
> 第 3 条"CI 跑 `make bench` 并在 `git diff` 非空时失败"**已被否决**，见 §4.6。
> 保留原文是因为它的**理由**仍然成立："生成的文档才可能是真的"——
> 那条纪律活在 `scorecard.txt` 是 `corpus score` 的原样字节这件事上。

照 `hack/gen-rules` 的两段式，理由相同（**生成的文档才可能是真的**）：

1. `cmd/aguard/benchmark_test.go` —— 复用现成的 `scanEnv` / `checkTarget`（adversarial harness
   已经在用），遍历语料，把每个样本的实测 finding 集合 + 标注写进 `testdata/corpus/results.json`；
2. `hack/gen-bench` —— 读 `results.json`，渲染 `docs/benchmark.md`；
3. ~~`make bench` 跑这两步；CI 跑 `make bench` 并在 `git diff` 非空时失败。~~ → 见 §4.6：`make bench` 保留为入口，**CI 不卡**。

**范围**：只统计**计分 finding**。dimension-0 的 `COV-000` / `IGN-000` 是覆盖披露不是检测，
`hygiene` 不是安全判定——两者都排除，并在文档里写明排除了。
LLM judge 单独测、单独报，作为 delta，**绝不与确定性数字混在一起**（它非确定性，需要多轮方差，
排在最后，可选）。

### 4.4 要发的东西

> **载体变了（2026-09-22）**：下面这个示例块是 2026-09-16 画的 `docs/benchmark.md` 草样，
> **那个文件从未生成，`hack/gen-bench` 也从未写**。P-015 用另一个形态实现了同一个目的：
> `make bench` 把 `corpus score` 的**原样输出**写成 `baselines/results/<工具>/<日期>/scorecard.txt`
> 并提交。原样是要点 —— 打分器内建了一串"让不诚实读法变难"的拒绝（区间太宽就印计数、
> collected 与 constructed 不合并、未覆盖数写在头部），人一转述就全丢了。
>
> **下面块里的具体数字全部过期**（`tool_version: v0.8.1`、"61 条计分规则"、34/40、2/103），
> 留着只为说明**要发哪几种粒度**。**规则条数以 `docs/rules.md` 头部为准，别信这里。**

```
rules_version: <sha256 前 12 位>     tool_version: v0.8.1     语料层: vendored

① 门禁视角（--fail-on high，按样本）—— 头条
   恶意样本被拦下:            34/40
   良性样本被误拦:            2/103
   静态不可见（退出分母）:    6      原因分布: 运行时行为 4 / 需连接 MCP 2

② 按规则
   规则       | 有样本 | 正确响 | 错误响 | 漏
   INJ-001    |   7    |   7    |   0    |  0
   EXEC-001   |   5    |   4    |   1    |  1
   …
   61 条计分规则中 N 条至少有一个样本，M 条未被测量  ← 必须写出来
   （分母取 docs/rules.md 头部计数块的「score」数，不要手写）

③ 良性集噪音（按严重度）
   critical 0 · high 2 · medium 11 · low 187

④ 语料构成
   promoted 30 · reconstruction 12 · synthetic 18 · real-world 68
```

第 ② 表最后那行是关键：**未被测量的规则要点名**。这和 `COV-000` 是同一条纪律——
一个自己声明出来的空洞是操作者可以决策的信息，一个藏起来的空洞是谎言。

### 4.5 阶段划分

> **执行状态（2026-09-22）**：阶段 0–2 **已完成**（在语料仓与 P-011/P-015 里），
> 阶段 3、4 **未做**。下表保留原文，右边一列是实际结果。

| 阶段 | 内容 | 工期 | 结束时能说什么 |
|---|---|---|---|
| **0** | schema + 打分器 + 30 行 adversarial 转正 | 2–3 天 | 有真实数字（小样本），且知道 61 条计分规则里有多少条根本没被测 |
| **1** | 良性集：本机 952 个真实 skill + 外部大规模良性（见 §3.9 第 1 层） | 1–2 周 | **误报率开始有意义**，门禁能不能进 CI 有了依据 |
| **2** | 恶意集到 ~40，铺满 10 个维度 | 1–2 周 | 召回率可发布 |
| **3** | 进 README、CI 卡回归（不卡绝对值）、提 awesome 清单 | 2 天 | 对外可引用 |
| **4**（可选） | LLM judge 的 delta + 多轮方差 | 1 周 | judge 到底加了多少 |

**阶段 0 必须先做完再收数据。** 它便宜（2–3 天），并且它的产出——"哪些规则没有样本"——
正好是阶段 1、2 的采样清单。反过来先收数据，标注字段一定不够，全部要重标。

**实际发生了什么：**

| 阶段 | 结果 |
|---|---|
| 0 schema + 打分器 | ✅ 在语料仓完成，且打分器也搬了过去（与 §4.3 的设想不同） |
| 1 良性集 | ✅ 远超原计划：**3,220 个真实良性，4 个来源**（skillmd-138k 1996 · skillet-wild 474 · harvested 387 · automatelab 353）。**"误报率开始有意义"这件事已经发生**：4.6%，按来源 2–6% |
| 2 恶意集 | ✅ **300 个**，原计划 ~40 |
| 3 进 README、CI 卡回归、提清单 | ❌ 未做。CI 卡回归这一条**已被否决**，见 §4.6；README 与清单是 M1.6 / M2.5 |
| 4 judge delta | ❌ 未做，排在 `plan` §3.1 第 5 步 |

### 4.6 CI 里怎么卡 —— **不卡。2026-09-22 定。**

原文是：`make bench` 与提交的 `docs/benchmark.md` 不一致就失败，数字变差失败，
**数字变好也失败**（因为文件漂了）。目的写得很清楚：**让每次 P/R 变动都必须有人看一眼。**

**那个目的保留，这个实现否决。** 三条理由：

1. **语料是兄弟 checkout，拆出去正是为了许可隔离**（第 1 层混着 Apache-2.0 与 CC-BY-4.0）。
   让 CI 卡 `make bench` 等于把语料变成 CI 的必需依赖，把刚拆开的耦合又接回去。
2. **第 2 层（`cache/`，约 874 MB，含无许可与 NonCommercial 语料）要 `make fetch` 才在。**
   在 CI 上处理它是另一个量级的问题，而且正好踩 `licensing.md` 那条"测量不是分发"的边界。
3. **CI 的绿灯会取决于一个不在本仓库版本控制下的目录当天是什么样子。** 那种绿灯不构成证据 ——
   而"绿灯不构成证据"是本仓库反复吃过亏的那条（见 `CLAUDE.md` 关于 go 指令自动切换工具链那段）。

**换来的装置**（P-015 已落地）：

- 基线**提交**进 `baselines/results/<工具>/<日期>/`：verdicts、覆盖账本、原样成绩单、`run.yaml`；
- 改规则的 PR 里贴**前后两份 verdicts 的 diff** 和两个数，这是 `plan` §3.1 八步的硬规则；
- 覆盖账本自带穷尽性检查（`scored + no-verdict + error == 全部测试点`，且没有 `skipped` 这个下场），
  所以"某批样本悄悄不再被测"这件事会当场失败，而不是表现为分母变小。

人看一眼这件事照样发生，只是发生在 **PR 上**而不是 CI 上。
`Makefile` 的 bench 目标注释写着同一句话：
"the corpus is ... **never a CI dependency**; this target is for a maintainer's machine"。

---

## 5. 两个不能丢的设计决定

### 5.1 Go harness 与磁盘 fixture **并存**，不是替换

这份文档的前身说"把 `build func(t, home, root)` 换成磁盘 fixture + `_label.yaml`"。
**那是拿掉一个能力去换规模。**

**git 不携带目录权限位**，`tar` 也不可靠（实测 BSD tar 造不出 `0111`）。所以下面这整类
**文件系统级规避**只能在测试 setup 里用代码构造：

- 只可遍历目录（`chmod 0111`）—— agent 能按路径执行，`WalkDir` 读不了
- FIFO 形态的 `SKILL.md` / `settings.json` / `.mcp.json`
- 软链逃逸与成环

这三类恰好是 2026-09-11 审计里最严重的几条缺陷的触发方式。**磁盘 fixture 表达不了它们。**

分工：**磁盘 fixture 收规模和非 Go 贡献者，Go harness 保留文件系统级规避这一类。**

### 5.2 每一条已确认的误报都必须有一个良性样本钉住

2026-09-11 的第三方核验确认了四类误报（当时的内部审计记录已归档，不在本仓库）：
`.xsd` 开头的 UTF-8 BOM、`os.environ.copy()` 传给子进程、**教 agent 拒绝注入的防御性文字**、
以及同一个 `atob(` 被两条规则各报一次。

**每一条都要配一个 `_label.yaml`**，`class: benign`，`quiet:` 里写死对应规则 ID。

理由：压误报如果只是改一次正则，下一轮规则调整会把它们放回来，而且没人会发现。
把它变成语料，"这条不该响"就从一次性修补变成回归可测的性质。

**反向约束同样必须写进去**：修完之后，**真正的注入文本仍然必须响** —— 否则"压误报"就是
把规则挖空。每一条 `quiet:` 旁边要有一条 `expect.rules:` 的恶意样本配对。

---

## 6. 与其他项的依赖关系

- **E.2（`rules_version` / `aguard verify`）应该先做或同期做**——benchmark 头部要钉规则集哈希，
  否则数字无法归因；
- **A.1（AST 检测）必须排在 E.1 之后**。正则精度是不是真问题，只有**逐规则在良性样本上响了几次**
  那一列能回答 —— 它现在由 `make bench` 直接打印，并随基线提交（2026-09-22 实测：
  `EXFIL-001` 71 · `EXEC-001` 21 · `MCP-001` 20 · `FS-001` 14 …）。现在做 AST 是为一个未经测量的假设付大额依赖成本；
- **E.10（提 awesome 清单）以阶段 2 完成为前置**——清单把"failure labels 缺乏 ground truth"
  列为该领域头号 open gap，带着数字去提和空手去提是两回事；
- **公开仓库**是这一切的前提：语料的价值一半在于**别人可以往里加样本、可以复算你的数字**。
  私有仓库里的 benchmark 只是一份自测报告。

## 7. 三句话总结

第一，**不是从零开始**——30 行标注语料和取反断言的纪律已经在 `adversarial_test.go` 里，
要做的是把断言换成计数、把 Go 函数换成磁盘 fixture。

第二，**最难的不是收样本，是口径**：三种粒度都要发、恶意但静态不可见的要退出分母并点名、
良性期望必须是"不超过 low"而不是"沉默"、每个比率旁边写 n 且写分数不写百分比。

第三，**先做 2–3 天的阶段 0**，它会告诉你 61 条计分规则里有多少条根本没被测量——那份名单就是
后面几周的采样清单。
