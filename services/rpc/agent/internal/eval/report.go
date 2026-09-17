package eval

import (
	"fmt"
	"sort"
	"strings"
)

func formatRatio(r Ratio) string {
	if r.Value == nil {
		return "未适用（0 个有效样本）"
	}
	return fmt.Sprintf("%d/%d（%.2f%%）", r.Numerator, r.Denominator, *r.Value*100)
}

func markdownText(value string) string {
	return strings.NewReplacer("\r", " ", "\n", " ", "|", "\\|", "`", "\\`", "<", "&lt;", ">", "&gt;").Replace(value)
}

// Markdown 与 JSON 使用同一 Report，不另跑一次评测或重新计算样本。
func Markdown(r Report) string {
	var b strings.Builder
	fmt.Fprint(&b, "# Agent 离线规则基线\n\n")
	fmt.Fprint(&b, "合成快照与标注，待人工复核；这是代码行为基线，不是真实模型/商城效果或生产性能结论。\n\n")
	fmt.Fprintf(&b, "- 快照：%s；策略：%s；代码标记：%s（调用者提供）。\n", markdownText(r.Metadata.SnapshotVersion), markdownText(r.Metadata.StrategyVersion), markdownText(r.Metadata.CodeRevision))
	fmt.Fprintf(&b, "- 快照 SHA-256：`%s`。\n- 用例 SHA-256：`%s`。\n", r.Metadata.SnapshotSHA256, r.Metadata.CasesSHA256)
	fmt.Fprintf(&b, "- 环境：%s / %s，%s，%d 逻辑 CPU；并发 %d，TopK=%d，划分=%s。\n", r.Metadata.OS, r.Metadata.Arch, r.Metadata.GoVersion, r.Metadata.LogicalCPUs, r.Metadata.Concurrency, r.Metadata.TopK, r.Metadata.Split)
	fmt.Fprint(&b, "- 每个用例新建内存会话；进程复用、无显式预热、无外部缓存；历史准备和重放不计入本轮推荐延迟。\n\n")
	fmt.Fprintln(&b, "## 对照运行状态\n\n| 基线 | 状态 | 范围 |\n| --- | --- | --- |")
	for _, baseline := range r.Baselines {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", baseline.ID, baseline.Status, markdownText(baseline.Reason))
	}
	fmt.Fprintln(&b, "\n## 汇总\n\n| 指标 | 结果 |\n| --- | --- |")
	s := r.Summary
	for _, m := range []struct {
		name  string
		value Ratio
	}{
		{"完成结果硬约束违规率", s.HardViolationRate}, {"预期终态/错误类别命中", s.OutcomeAccuracy},
		{"可满足任务成功率", s.SatisfiableSuccess}, {"需求覆盖率（含不可满足需求）", s.RequirementCoverage},
		{"Recall@K（micro）", s.RecallAtK}, {"完成结果事实一致性", s.FactConsistency},
		{"规则兜底率（含显式故障注入）", s.FallbackRate}, {"成功轮次重放通过率", s.ReplaySuccess}, {"持久化轮次数符合预期", s.PersistenceSuccess},
	} {
		fmt.Fprintf(&b, "| %s | %s |\n", m.name, formatRatio(m.value))
	}
	fmt.Fprintf(&b, "\n总请求 %d，完成 %d，违规请求 %d，无相关 SKU 标注 %d；商品查询 %d 次，故障替身调用 %d 次，模型调用 %d 次。Token/费用、真实向量召回、解释文本评审均未测量。\n", s.Requests, s.Completed, s.HardViolationRequests, s.NoRelevantCases, s.ProviderCalls, s.FaultPrimaryCalls, s.ModelCalls)
	fmt.Fprintf(&b, "\n本地端到端 P50=%.4f ms，P95=%.4f ms（nearest-rank，小样本且无外部依赖，仅作本机参考）。安全/终态/幂等门禁：%t；该门禁不代表推荐质量达标。\n", s.LatencyP50MS, s.LatencyP95MS, s.GatePassed)
	fmt.Fprintln(&b, "\n## 固定划分\n\n| 划分 | 用例数 | 可满足成功 | 覆盖率 | Recall@K |\n| --- | --- | --- | --- | --- |")
	for _, split := range []string{"dev", "holdout"} {
		if m, ok := r.BySplit[split]; ok {
			fmt.Fprintf(&b, "| %s | %d | %s | %s | %s |\n", split, m.Requests, formatRatio(m.SatisfiableSuccess), formatRatio(m.RequirementCoverage), formatRatio(m.RecallAtK))
		}
	}
	fmt.Fprintln(&b, "\n## 场景\n\n| 场景 | 用例数 | 可满足成功 | 终态命中 |\n| --- | --- | --- | --- |")
	names := make([]string, 0, len(r.ByScenario))
	for name := range r.ByScenario {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		m := r.ByScenario[name]
		fmt.Fprintf(&b, "| %s | %d | %s | %s |\n", name, m.Requests, formatRatio(m.SatisfiableSuccess), formatRatio(m.OutcomeAccuracy))
	}
	fmt.Fprintln(&b, "\n## 未通过任务或门禁的样本\n\n| 用例 | 划分 | 终态 | 覆盖 | 选择 SKU | 硬约束问题 |\n| --- | --- | --- | --- | --- | --- |")
	failed := 0
	for _, c := range r.Cases {
		if c.OutcomeMatched && len(c.Violations) == 0 && c.PersistenceOK && (!c.ReplayChecked || c.ReplayOK) && (c.Feasibility != "satisfiable" || c.TaskSuccess) {
			continue
		}
		failed++
		fmt.Fprintf(&b, "| %s | %s | %s | %d/%d | %s | %s |\n", c.ID, c.Split, c.Status, c.RequirementsMet, c.RequirementsTotal, strings.Join(c.SelectedIDs, ", "), strings.Join(c.Violations, ", "))
	}
	if failed == 0 {
		fmt.Fprintln(&b, "| 无 | — | — | — | — | — |")
	}
	fmt.Fprintln(&b, "\n## 口径与限制\n\n- 可满足成功必须非空、满足全部必需需求、仅含允许 SKU，且通过独立快照/预算/件数校验。\n- Recall@K 以当前轮过滤后的 TopK 为分子来源，跨用例求命中数/相关 SKU 数；无相关 SKU 不进分母。\n- 需求覆盖率包含标注不可满足的需求，因此它的上限未必为 100%。\n- 错误/拒绝必须不新增完成轮次；成功重放必须同结果、零新增轮次/商品查询/故障替身调用。\n- dev 用于未来调参；holdout 按用例族固定隔离，不凭本次结果移动划分或修改答案。\n- 故障 primary 是确定性 Agent 替身，不是 Fake Model；真实向量、ReAct、模型注入抵抗和 Token/费用仍未运行。\n- 查询、商品描述不包含真实个人数据；注入用例仅验证此规则路径的边界，不能外推到真实 LLM。")
	return b.String()
}
