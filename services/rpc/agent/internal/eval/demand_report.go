package eval

import (
	"fmt"
	"strings"
)

func DemandMarkdown(r DemandReport) string {
	var b strings.Builder
	m := r.Metadata
	fmt.Fprint(&b, "# Agent 需求快照选品与核验回放\n\n合成事实、公开回归样例、待独立人工复核；不是线上 A/B、真实分类质量或性能验收。\n\n")
	fmt.Fprintf(&b, "- 数据：%s；SHA-256：`%s`。\n- 协议：%s；SHA-256：`%s`。\n- 代码标记：%s（调用者提供，未认证）。\n",
		markdownText(m.DatasetVersion), m.DatasetSHA256, markdownText(m.Protocol), m.ProtocolSHA256, markdownText(m.CodeRevision))
	fmt.Fprintf(&b, "- 环境：%s / %s，%s，GOMAXPROCS=%d；并发 %d，无显式预热、固定策略/用例顺序。\n\n", m.OS, m.Arch, m.GoVersion, m.GOMAXPROCS, m.Concurrency)
	fmt.Fprint(&b, "## 同快照选品\n\n旧贪心是直接调用 BundleSelector 的算法对照，不是旧 Recommend/API 已支持需求：只传它支持的同一预算/件数投影，再用完整需求统一评估；生产 Demand 拒绝保护不变。所有策略读取相同完整候选快照，窗口裁剪是被测策略的一部分；不做检索、复核、规划或模型调用。独立穷举最多 8 SKU / 255 个非空子集，仅证明该快照的可行性与文档化目标，不证明真实全库最优。\n\n")
	fmt.Fprint(&b, "| 策略 | 可满足任务成功 | 非空结果硬约束违规 | 必需覆盖 | 可选覆盖 | 可行样例穷举最优命中 | 受限漏解/全部漏解 |\n| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, s := range r.Selection {
		q := s.Summary
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %d/%d |\n", s.Strategy.ID, formatRatio(q.SatisfiableSuccess),
			formatRatio(q.HardViolationRate), formatRatio(q.RequirementCoverage), formatRatio(q.OptionalCoverage), formatRatio(q.OracleOptimumMatch), q.LimitedMisses, q.Misses)
	}
	fmt.Fprint(&b, "\n| 策略 | 窗口/宽度/展开上限/ms | 输入快照数 | 展开尝试总数 | 本机 P50/P95（ms） |\n| --- | --- | --- | --- | --- |\n")
	for _, s := range r.Selection {
		q, p := s.Summary, s.Strategy
		work, bounds := "未插桩（null）", "不适用"
		if q.SearchExpansions != nil {
			work = fmt.Sprint(*q.SearchExpansions)
			bounds = fmt.Sprintf("%d/%d/%d/%d", p.MaxCandidates, p.BeamWidth, p.MaxExpansions, p.TimeBudgetMS)
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %s | %.4f/%.4f |\n", p.ID, bounds, q.InputSnapshots, work, q.ReplayP50MS, q.ReplayP95MS)
	}
	fmt.Fprint(&b, "\n### 保留的失败与非最优样例\n\n| 用例 | 策略 | 有解 | 所选 SKU | 最优 SKU | 漏解/违规 |\n| --- | --- | --- | --- | --- | --- |\n")
	for _, s := range r.Selection {
		for _, c := range s.Cases {
			if len(c.Violations) == 0 && (!c.Oracle.Feasible || c.OracleMatch) {
				continue
			}
			fmt.Fprintf(&b, "| %s | %s | %t | %s | %s | %s %s |\n", c.ID, s.Strategy.ID, c.Oracle.Feasible,
				strings.Join(c.SelectedIDs, ", "), strings.Join(c.Oracle.BestIDs, ", "), c.MissReason, strings.Join(c.Violations, ", "))
		}
	}
	fmt.Fprint(&b, "\n### 默认 Beam 相对对照的任务成功变化\n\n| 对照 | 改善 | 退化 | 持平 |\n| --- | --- | --- | --- |\n")
	for _, c := range r.Comparisons {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", c.Baseline, strings.Join(c.Improved, ", "), strings.Join(c.Regressed, ", "), strings.Join(c.Tied, ", "))
	}
	fmt.Fprint(&b, "\n## 独立 Mall 执行回放\n\n运行生产 NewMall Executor、分类核验、两次选品及最终检查，外部接口由内存快照替身实现。没有真实 Mall 搜索、gRPC 传输、JWT 验签、数据库、索引或库存预占；此层不与裸选择器的延迟混算。\n\n")
	q := r.ExecutionSummary
	fmt.Fprintf(&b, "快照变化 %d 例，故障 %d 例；预期终态 %s；故障闭合 %s；非空硬约束违规 %s；最终检索快照可满足成功 %s。Provider 读取 %d，分类核验调用 %d，展开尝试 %d。快照用例本机 P50/P95=%.4f/%.4f ms。\n\n",
		q.SnapshotCases, q.FaultCases, formatRatio(q.OutcomeAccuracy), formatRatio(q.FaultClosed), formatRatio(q.HardViolationRate), formatRatio(q.SnapshotSuccess), q.ProviderCalls, q.CheckCalls, q.SearchExpansions, q.ReplayP50MS, q.ReplayP95MS)
	fmt.Fprint(&b, "| 用例 | 注入阶段/故障 | 实际/预期 | 最终检索快照有解 | 所选 SKU | 读取/核验 | 初选/重选展开 | 门禁问题 |\n| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, c := range r.Execution {
		fmt.Fprintf(&b, "| %s | %s %s | %s/%s | %t | %s | %d/%d | %d/%d | %s |\n", c.ID, c.FaultStage, c.Fault, c.Outcome, c.Expected,
			c.FinalOracle.Feasible, strings.Join(c.SelectedIDs, ", "), c.ProviderCalls, c.CheckCalls, c.InitialExpansions, c.FinalExpansions, strings.Join(c.GateProblems, ", "))
	}
	fmt.Fprintf(&b, "\n机制门禁：%t。不要求新策略必须获胜；旧贪心的分类不支持、主动收窄策略的漏解和复核窗口外降价导致的漏解均保留。门禁约束事实/数值安全、新路径需求安全、调用/资源边界和故障预期。实际触及时限属于本次测量不确定，不能静默通过。\n", r.GatePassed)
	fmt.Fprint(&b, "\n## 口径、反例与未运行项\n\n- 可满足成功的分母由独立穷举确定；所有失败留在分母。空结果不算成功。硬约束违规分母是非空结果，包括缺必需类别；空结果漏解另报，不能用零违规掩盖漏解。\n- 必需/可选覆盖是逐例类别命中数的 micro 比率，包含不可满足输入。事实、排除或数值违规的结果覆盖归零；事实合法的部分必需覆盖仍记录，但不是任务成功。无适用样本的比率为 null。\n- 穷举目标：满足全部硬约束后，优先可选类别覆盖，再按 value 偏好（若有）/预录 RRF 效用/价格/件数/SKU 字典序。预录排名不是实际向量输出；当前 Mall 执行仍仅关键词召回，quiet 等属性未评分。\n- 窄 Beam 可能在所有类别均在窗口时漏解；窗口截断/展开上限也可能漏解。no_feasible_bundle 不是全库无解。外部降价不能使初次窗口之外的 SKU 自动获得最终核验资格；outside-window-not-revived 明确保留这一损失。\n- 选择耗时仅包围 Select（包含该调用内部准备），不含 fixture/分类目录构造、穷举或报告评估。执行耗时包围 Executor.Run，包含内存适配器与核验；不含构造/穷举/评估。nearest-rank、小样本、未控制机器负载，不能外推生产 P95。\n- 展开数是 Beam 评估尝试（含不可行尝试），不是 CPU 指令。贪心未插桩，展开数为 null 而非零；内存分配用独立 Benchmark -benchmem 测量，不在逐例质量报告中用全进程内存差冒充。\n- 读取/核验是内存接口调用（失败也计数），不是网络 RPC。超时/取消故障为错误码注入，不是耗尽真实期限或证明取消传播；真实环境的鉴权、期限与故障联调仍待验收。\n- 所有数据均是开发期已知合成回归样例，无独立 holdout，不修改旧 products/cases/review/retrieval 标签或旧基线。真实 Mall、Embedding、模型均 not_run，模型调用 0，Token/费用 null。\n")
	return b.String()
}
