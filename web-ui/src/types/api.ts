export interface LoginResp {
  token: string
  user_id: string
  role: number
}

export interface RegisterReq {
  username: string
  email: string
  password: string
  code: string
}

export interface RegisterResp {
  success: boolean
}

export interface UserInfo {
  user_id: string
  username: string
  email: string
  avatar: string
  phone: string
  role: number
}

export interface UserProfile {
  user_id: string
  real_name: string
  school: string
  major: string
  grade: string
  gender: number
  expected_city: string
  expected_position: string
  self_introduction: string
}

export interface UpdateUserProfileReq {
  real_name?: string
  school?: string
  major?: string
  grade?: string
  gender?: number
  expected_city?: string
  expected_position?: string
  self_introduction?: string
}

export interface Product {
  id: string
  spu_code: string
  name: string
  category_id: string
  brand: string
  status: number
  main_image: string
  detail: string
  created_at: string
  updated_at: string
}

export interface Sku {
  id: string
  product_id: string
  sku_code: string
  name: string
  price: number
  stock: number
  status: number
  attributes: Record<string, string>
  created_at: string
  updated_at: string
}

export interface OrderItem {
  product_id: string
  sku_id: string
  sku_name: string
  price: number
  quantity: number
  total_amount: number
  snapshot: string
}

export interface Order {
  id: string
  user_id: string
  original_amount: number
  discount_amount: number
  pay_amount: number
  status: number
  pay_type: string
  pay_time: string
  remark: string
  snapshot: string
  idempotency_key: string
  items: OrderItem[]
  created_at: string
  updated_at: string
}

export interface Activity {
  id: string
  name: string
  description: string
  start_time: string
  end_time: string
  status: number
  created_at: string
  updated_at: string
}

export interface SeckillSku {
  id: string
  activity_id: string
  sku_id: string
  sku_name: string
  seckill_price: number
  stock: number
  limit: number
  status: number
}

/** 可跨轮继承的结构化推荐约束。 */
export interface AgentIntent {
  budget_cents: number
  max_items: number
  keywords: string[]
  preferences: string[]
}

export interface AgentBundleItem {
  id: string
  name: string
  category: string
  source: string
  price_cents: number
  stock: number
  score: number
  reason: string
}

export interface AgentToolCall {
  name: string
  success: boolean
  detail: string
}

/** 一轮推荐的完整结果，同时携带会话和幂等轮次标识。 */
export interface AgentRecommendResp {
  intent: AgentIntent
  items: AgentBundleItem[]
  total_price_cents: number
  summary: string
  tools_used: AgentToolCall[]
	conversation_id: string
	conversation_title: string
	turn_id: string
}

/** 会话侧栏及历史页使用的轻量摘要。 */
export interface AgentConversationSummary {
	conversation_id: string
	conversation_title: string
	state: AgentIntent
	turn_count: number
	created_at_ms: number
	updated_at_ms: number
}

/** 一轮不可变历史，包含原始问题、当轮意图和保存的完整结果。 */
export interface AgentConversationTurn {
	turn_id: string
	sequence: number
	query: string
	budget_cents: number
	max_items: number
	intent: AgentIntent
	result: AgentRecommendResp
	created_at_ms: number
	completed_at_ms: number
}

/** 会话历史分页响应，并附带当前最新的结构化状态。 */
export interface AgentConversationTurnsResp {
	conversation: AgentConversationSummary
	list: AgentConversationTurn[]
	page: number
	page_size: number
	total: number
}

export interface ListResp<T> {
  list: T[]
  page: number
  page_size: number
  total: number
}
