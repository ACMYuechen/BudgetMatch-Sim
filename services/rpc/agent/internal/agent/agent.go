// agent 定义了 Agent 核心领域模型，包括输入、意图、商品、工具调用和结果等数据结构，以及 Agent 接口。
package agent

import "context"

// Input 表示用户向 Agent 发起一次推荐请求时的输入参数。
type Input struct {
	Query       string // Query 用户原始查询文本
	BudgetCents int64  // BudgetCents 用户预算，单位为分
	MaxItems    int32  // MaxItems 期望返回的最大商品数量
	UserID      string // UserID 用户唯一标识
}

// Intent 表示从用户输入中解析出的意图，用于指导后续推荐逻辑。
type Intent struct {
	BudgetCents int64    // BudgetCents 解析后的预算，单位为分
	MaxItems    int32    // MaxItems 解析后的最大商品数量
	Keywords    []string // Keywords 提取出的关键词列表
	Preferences []string // Preferences 用户偏好标签列表
}

// BundleItem 表示推荐结果中的一个商品条目。
type BundleItem struct {
	ID         string  // ID 商品唯一标识
	Name       string  // Name 商品名称
	Category   string  // Category 商品分类
	Source     string  // Source 商品来源
	PriceCents int64   // PriceCents 商品价格，单位为分
	Stock      int64   // Stock 商品库存数量
	Score      float64 // Score 商品综合评分
	Reason     string  // Reason 推荐理由
}

// ToolCall 记录 Agent 执行过程中调用外部工具的信息。
type ToolCall struct {
	Name    string // Name 工具名称
	Success bool   // Success 工具调用是否成功
	Detail  string // Detail 工具调用详情
}

// Result 表示 Agent 执行一次推荐后的完整结果。
type Result struct {
	Intent          Intent       // Intent 解析出的用户意图
	Items           []BundleItem // Items 推荐的商品列表
	TotalPriceCents int64        // TotalPriceCents 推荐商品总价，单位为分
	Summary         string       // Summary 推荐结果摘要
	ToolsUsed       []ToolCall   // ToolsUsed 执行过程中使用的工具记录
}

// Agent 是推荐 Agent 的抽象接口，每个实现代表一种推荐策略。
type Agent interface {
	// Name 返回 Agent 的唯一名称标识。
	Name() string
	// Run 执行推荐逻辑，根据输入参数返回推荐结果或错误。
	Run(ctx context.Context, input Input) (*Result, error)
}
