<!-- SPDX-License-Identifier: MIT -->
# 014 — OWASP Agentic ASI 编号与标题未经官方核对

| 项 | 值 |
|----|----|
| 类别 | 事实待核实 |
| 严重程度 | 中（编号本身几乎确定正确；标题可能是转述） |
| 状态 | 未修复（待人工核对官方 PDF） |

## 问题描述

`internal/detect/owasp.go` 把每条规则映射到 OWASP Top 10 for Agentic Applications (2026) 的
ASI-01..ASI-10。**这些编号与标题取自第三方发布的映射，不是官方文档。**

原因：OWASP 的这份清单以 **PDF 下载**形式分发，官方页面（`genai.owasp.org`）不列出十个类别的
编号与标题，抓不到。所以调研时用的是竞品 `agent-audit` 的 README 里那张映射表。

判断：**编号几乎确定正确**（多处引用一致）；**标题可能是转述**而非官方措辞。

## 影响

现在只影响三处显示：SARIF 的 `properties.owasp-agentic` 标签、终端报告的覆盖行、
规则的 `tags`。**不影响任何计分**——十个计分维度没动，`overall` 的契约不受影响。

但一旦这个映射出现在客户读作**合规声明**的地方（销售材料、审计报告、"我们覆盖 OWASP
Agentic Top 10 的 7 项"这类表述），措辞不对就是事实错误。

## 修复方向

1. 下载官方 PDF，逐条核对十个类别的**编号与标题**，更新 `asiCatalogue`。
2. 顺带核对**类别定义**——我们的映射里有几处是判断而非照抄，需要确认没有误读：
   - 外传（EXFIL-*）映射到 ASI-02 Tool Misuse，因为 Agentic 清单**没有**独立的信息泄露
     类别（那在 LLM Top 10 是 LLM06）。需要确认官方定义确实把"工具把数据带出去"归在这里。
   - `PERM-004`（缺 deny 兜底）映射到 ASI-03 Identity & Privilege，是"没有边界约束授权
     变成什么"的解读。
   - 三个刻意留空的族（OBF/RES/BD）应确认官方对 ASI-08/ASI-09/ASI-10 的定义确实是运行时
     行为——如果其中某一条也覆盖静态可见的迹象，我们就漏报了一个类别。
3. 核对后把本文件状态改为已核实，并在 `owasp.go` 的 PROVENANCE 段落里换掉"未经核对"的说明。

## 关联

- 调研来源：[awesome-auditable-ai](https://github.com/yzhao062/awesome-auditable-ai)
- 竞品映射：[agent-audit](https://github.com/HeadyZhang/agent-audit)（53 条规则覆盖全部 10 类）
