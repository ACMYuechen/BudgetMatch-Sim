package eval

import (
	"fmt"
	"strings"
)

func RetrievalMarkdown(r RetrievalReport) string {
	var b strings.Builder
	m := r.Metadata
	fmt.Fprint(&b, "# Agent 检索排序回放对比\n\n合成排序输入、待人工复核；不是真实语义效果、Mall 搜索性能或推荐套装质量报告。默认线上策略不变。\n\n")
	fmt.Fprintf(&b, "- 数据版本：%s；SHA-256：`%s`。\n- 协议：%s；SHA-256：`%s`。\n- 代码标记：%s（调用者提供，不是签名认证）。\n",
		markdownText(m.DatasetVersion), m.DatasetSHA256, markdownText(m.Protocol), m.ProtocolSHA256, markdownText(m.CodeRevision))
	fmt.Fprintf(&b, "- TopK=%d，初始窗口=%d，最大窗口=%d，RRF 常量=%d，混合单次检索期限=%d ms。\n", m.TopK, m.InitialK, m.MaxK, m.RRFConstant, m.TimeoutMillis)
	fmt.Fprintf(&b, "- 环境：%s / %s，%s，GOMAXPROCS=%d；用例串行、混合支路并发，无预热、无远程缓存。\n\n", m.OS, m.Arch, m.GoVersion, m.GOMAXPROCS)
	fmt.Fprint(&b, "## 策略与共同边界\n\n- keyword_only / vector_only：生产 HybridProductProvider 的单路消融，另一支路固定为空且不计源读取；复用相同去重、过滤、扩召回，不是新增线上模式。\n- hybrid_rrf：生产混合 Provider，两路读取固定排序前缀。\n- vector_first：生产 RAGProductProvider，保留原有窗口下限 8、按 MaxItems 放大及错误/空结果回退；关键词回退读取整个 MaxK 有界快照，不模拟真实 Mall 分页。\n- 所有策略均经过候选归一化、单 SKU 预算过滤，再取同一 TopK；原始返回量和每次读取窗口逐例记录，不声称四种策略读取成本相同。\n- 只评估检索候选，不运行 Planner、最终核验、组合选择或真实模型；MaxItems 不是候选池大小上限。\n\n")
	fmt.Fprint(&b, "## 质量用例汇总\n\n| 策略 | 质量/故障用例 | Recall@K | Precision@K | nDCG@K | 无相关样本的非空率 | 质量源读取（词/向量） | 扩召回 |\n| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, s := range r.Strategies {
		q := s.Summary
		ndcg := "未适用"
		if q.NDCGAtK.Value != nil {
			ndcg = fmt.Sprintf("%.6f（%d 例）", *q.NDCGAtK.Value, q.NDCGAtK.Samples)
		}
		fmt.Fprintf(&b, "| %s | %d/%d | %s | %s | %s | %s | %d/%d | %d |\n", s.ID, q.QualityCases, q.FaultCases,
			formatRatio(q.RecallAtK), formatRatio(q.PrecisionAtK), ndcg, formatRatio(q.NegativeFalsePositiveRate), q.QualityWork.KeywordCalls, q.QualityWork.VectorCalls, q.QualityWork.Expansions)
	}
	fmt.Fprint(&b, "\n## 机制门禁与本机回放开销\n\n| 策略 | 预期终态 | 违规用例 | 全部源读取（词/向量） | 回退/成功降级 | 质量 P50/P95（ms） |\n| --- | --- | --- | --- | --- | --- |\n")
	for _, s := range r.Strategies {
		q := s.Summary
		fmt.Fprintf(&b, "| %s | %s | %d | %d/%d | %d/%d | %.4f/%.4f |\n", s.ID, formatRatio(q.OutcomeAccuracy), q.ViolationCases,
			q.AllWork.KeywordCalls, q.AllWork.VectorCalls, q.AllWork.Fallbacks, q.AllWork.DegradedSuccesses, q.ReplayP50MS, q.ReplayP95MS)
	}
	fmt.Fprintf(&b, "\n机制门禁：%t。只要求预期终态、独立目录事实及调用/窗口边界；不以 RRF 必须获胜作为通过条件。\n", r.GatePassed)
	fmt.Fprint(&b, "\n## RRF 相对对照的逐例 nDCG 变化\n\n| 对照 | 改善 | 退化 | 持平 |\n| --- | --- | --- | --- |\n")
	for _, c := range r.Comparisons {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", c.Baseline, strings.Join(c.Improved, ", "), strings.Join(c.Regressed, ", "), strings.Join(c.Tied, ", "))
	}
	fmt.Fprint(&b, "\n## 逐例结果\n\n| 用例 | 类型 | 策略 | 实际/预期 | TopK SKU | 相关命中 | 读取窗口 | 违规 |\n| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, s := range r.Strategies {
		for _, c := range s.Cases {
			var reads []string
			for _, read := range c.Reads {
				reads = append(reads, fmt.Sprintf("%s:%d", read.Lane, read.Window))
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s/%s | %s | %d/%d | %s | %s |\n", c.ID, c.Kind, s.ID, c.Outcome, c.Expected,
				markdownText(strings.Join(c.ReturnedIDs, ", ")), c.RelevantHits, c.RelevantTotal, strings.Join(reads, ", "), strings.Join(c.Violations, ", "))
		}
	}
	fmt.Fprint(&b, "\n## 指标和未运行项\n\n- 质量与故障注入用例分开统计；质量用例即使失败也不移出分母。Recall 为命中总数/相关标注总数；Precision 为命中总数/(K×质量用例数)，不足 K 不缩小分母。\n- nDCG 使用二值相关性：DCG=Σ hit/log2(rank+1)，除以理想前 min(K,相关数) 名的 DCG；无相关标注不进入 nDCG/Recall 分母，另报负例非空率。违规结果的质量分归零，不从聚合中删除。\n- 配对变化仅统计有相关标注的质量用例；共识噪声、漏召回和融合退化不删样本、不改标签。没有质量改善阈值或默认策略切换。\n- 源读取是内存适配器调用，包含失败尝试；不是 Mall RPC 次数、Embedding 请求、Token 或费用。向量分数只是名次倒数占位值，不是模型输出。\n- P50/P95 采用 nearest-rank，仅测质量用例的本机 Provider 调用，不含加载/校验/评估；小样本、固定执行顺序且未控制机器负载，不能证明生产延迟。超时故障仅注入错误码，不等待真实期限。\n- 真实 Mall、Embedding、模型、最终库存核验均为 not_run；Token/费用为 null。全部样本是公开的已知回归输入，无独立 holdout 或人工验收；报告来源标记不构成认证。\n")
	return b.String()
}
