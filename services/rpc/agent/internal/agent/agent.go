// agent 定义了 Agent 核心领域模型，包括输入、意图、商品、工具调用和结果等数据结构，以及 Agent 接口。
package agent

import "context"

// Input 表示用户向 Agent 发起一次推荐请求时的输入参数。
type Input struct {
	Query          string  // Query 用户原始查询文本
	BudgetCents    int64   // BudgetCents 用户预算，单位为分
	MaxItems       int32   // MaxItems 期望返回的最大商品数量
	UserId         string  // UserId 用户唯一标识
	ConversationId string  // ConversationId 多轮对话会话标识，为空表示新会话
	TurnId         string  // TurnId 客户端生成的轮次幂等标识，为空时由服务端生成
	PriorIntent    *Intent // PriorIntent 持久化的上一轮结构化状态，当前轮显式约束优先
}

// Intent 表示从用户输入中解析出的意图，用于指导后续推荐逻辑。
type Intent struct {
	BudgetCents int64    `json:"budget_cents"` // BudgetCents 解析后的预算，单位为分
	MaxItems    int32    `json:"max_items"`    // MaxItems 解析后的最大商品数量
	Keywords    []string `json:"keywords"`     // Keywords 提取出的关键词列表
	Preferences []string `json:"preferences"`  // Preferences 用户偏好标签列表
}

// BundleItem 表示推荐结果中的一个商品条目。
type BundleItem struct {
	Id         string  `json:"id"`          // Id 商品唯一标识
	Name       string  `json:"name"`        // Name 商品名称
	Category   string  `json:"category"`    // Category 商品分类
	Source     string  `json:"source"`      // Source 商品来源
	PriceCents int64   `json:"price_cents"` // PriceCents 商品价格，单位为分
	Stock      int64   `json:"stock"`       // Stock 商品库存数量
	Score      float64 `json:"score"`       // Score 商品综合评分
	Reason     string  `json:"reason"`      // Reason 推荐理由
}

// ToolCall 记录 Agent 执行过程中调用外部工具的信息。
type ToolCall struct {
	Name    string `json:"name"`    // Name 工具名称
	Success bool   `json:"success"` // Success 工具调用是否成功
	Detail  string `json:"detail"`  // Detail 工具调用详情
}

// Result 表示 Agent 执行一次推荐后的完整结果。
type Result struct {
	Intent            Intent       `json:"intent"`             // Intent 解析出的用户意图
	Items             []BundleItem `json:"items"`              // Items 推荐的商品列表
	TotalPriceCents   int64        `json:"total_price_cents"`  // TotalPriceCents 推荐商品总价，单位为分
	Summary           string       `json:"summary"`            // Summary 推荐结果摘要
	ToolsUsed         []ToolCall   `json:"tools_used"`         // ToolsUsed 执行过程中使用的工具记录
	ConversationId    string       `json:"conversation_id"`    // ConversationID 本次对话的会话标识，客户端携带它发起下一轮
	ConversationTitle string       `json:"conversation_title"` // ConversationTitle 会话的稳定展示标题
	TurnId            string       `json:"turn_id"`            // TurnId 本轮幂等标识，重试相同标识返回同一结果
}

// Agent 是推荐 Agent 的抽象接口，每个实现代表一种推荐策略。
type Agent interface {
	// Name 返回 Agent 的唯一名称标识。
	Name() string
	// Run 执行推荐逻辑，根据输入参数返回推荐结果或错误。
	Run(ctx context.Context, input Input) (*Result, error)
}
