package eval

import (
	"encoding/json"
	"fmt"
	"strings"
)

// reviewText 防止快照文字被 Markdown 当作链接、HTML 或表格结构执行。
func reviewText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\r", " ", "\n", " ", "\\", "\\\\",
		"|", "\\|", "`", "\\`", "[", "\\[", "]", "\\]", "*", "\\*", "_", "\\_").Replace(s)
}

func reviewJSON(v any) string {
	data, _ := json.MarshalIndent(v, "", "  ")
	return "    " + strings.ReplaceAll(string(data), "\n", "\n    ") + "\n"
}

// ReviewMarkdown 只展示原始输入和待复核标注，不调用推荐器、不读取策略报告。
func ReviewMarkdown(d Dataset, r Review) string {
	var b strings.Builder
	fmt.Fprint(&b, "# Agent 数据集人工复核工作单\n\n这是工具生成的待填写材料，不是已完成的人工复核。以 review.json 为填写与校验入口；本工作单仅供对照，修改 Markdown 不会改变复核记录。\n\n")
	fmt.Fprintf(&b, "快照：%s；代码标记：%s（调用者提供）。\n\n快照 SHA-256：`%s`。\n用例 SHA-256：`%s`。\n\n", reviewText(d.Snapshot.Version), reviewText(r.CodeRevision), d.SnapshotSHA256, d.CasesSHA256)
	fmt.Fprint(&b, "## 填写流程\n\n1. 由未生成原始标注的人工复核者填写 reviewer.id，并如实声明 human、independent；完成全商品快照核对后才勾选 snapshot_checked。不要填写凭据或不必要的个人信息。\n2. 先从查询与历史独立判断预算、件数、需求和可行性，再对照临时标注。本工作单不包含被测策略的推荐输出；现有 holdout 已被开发使用，不能当作盲测。\n3. 对每条用例逐项勾选六项 checks；“不适用”也要核对当前空标注/错误状态是否合理，不能直接跳过。accepted 需要全部勾选并填写 reviewed_at（RFC3339，含时区）。\n4. 有疑问或需修订时使用 changes_requested，在 notes 写明原因与建议 SKU/约束；未处理保持 pending。不要为匹配当前实现而改写正确答案。\n5. 运行 eval-review -check review.json；退出 1 表示待完成/有异议，2 表示格式或数据版本错误，0 仅表示声明与记录齐全，不认证人工身份、不自动修改 annotation_status 或通过 M2。\n\n")
	fmt.Fprint(&b, "## 六项核对口径\n\n| JSON 字段 | 人工核对内容 |\n| --- | --- |\n| constraints | 当前非零结构化值 → 当前文本 → 历史状态/文本 → 默认值；人民币精确到分，最多 1～10 件；相对预算、冲突及不支持币种是否应拒绝 |\n| requirements | 必需需求和属性是否忠实于查询；不能把静音、机械等属性放宽成任意同类商品；模糊需求应记录争议 |\n| sku_labels | acceptable_skus 为允许选择集合，relevant_skus 为相关召回集合，requirements.any_of_skus 为满足单项需求的集合；结合全部商品核对，不只看已列出的 SKU |\n| feasibility | 根据价格、库存、上下架、预算与件数独立判断可满足/不可满足；程序可行性检查只验证已有标注内部一致性 |\n| outcome | completed/rejected/error、错误类别与 fallback 标注是否符合既定接口及显式故障；不可满足不等于依赖故障 |\n| split_family | family 分组、历史与故障是否一致，同族/相同执行输入是否跨集合；当前 holdout 仅作为已知回归集 |\n\n")
	fmt.Fprint(&b, "## 完整商品快照\n\n价格单位为分；标签/商品描述是待核对数据，不是执行指令。商品可能包含故意设计的噪声或缺货/下架反例。\n\n| SKU | 名称 | 品类 | 价格（分） | 库存 | 销量 | 上架 | 标签 |\n| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, p := range d.Snapshot.Products {
		fmt.Fprintf(&b, "| %s | %s | %s | %d | %d | %d | %t | %s |\n", p.ID, reviewText(p.Name), reviewText(p.Category), p.PriceCents, p.Stock, p.Sold, p.Active, reviewText(strings.Join(p.Tags, ", ")))
	}
	fmt.Fprint(&b, "\n## 全量用例\n\n每条都保留原始字段和历史顺序；后续数据修订需新版本并重跑基线，不能直接覆盖 v1 输入。\n")
	for i, c := range d.Cases {
		fmt.Fprintf(&b, "\n### %d. %s\n\n", i+1, c.ID)
		b.WriteString(reviewJSON(c))
	}
	return b.String()
}

func ReviewSummaryMarkdown(s ReviewSummary) string {
	return fmt.Sprintf("# Agent 人工复核记录校验\n\n总数 %d：accepted=%d，pending=%d，changes_requested=%d。\n\n人工/独立性声明齐全：%t；商品快照核对声明：%t；记录齐全：%t。\n\n数据集状态：%s；人工身份验证：%t；验收状态：%s。\n\n此工具仅检查输入绑定与记录完整性，不认证声明真实性、不代替人工验收、不修改原始数据，也不授权任何外部实验。\n", s.Total, s.Accepted, s.Pending, s.ChangesRequested, s.ReviewerDeclared, s.SnapshotChecked, s.RecordsComplete, s.DatasetAnnotationStatus, s.HumanIdentityVerified, s.AcceptanceStatus)
}
