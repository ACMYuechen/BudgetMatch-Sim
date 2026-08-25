package memory

import "time"

// 默认配置值。
const (
	defaultMaxHistory       = 20             // 默认读取窗口 20 条消息（10 轮问答）
	defaultMaxContextTokens = 8000           // 默认发送给模型的完整消息上下文严格上限（近似 token）
	defaultTTL              = 24 * time.Hour // 默认会话空闲 24 小时后过期
)

// Conf 是会话记忆的行为配置。
type Conf struct {
	MaxHistory       int           `json:"maxHistory,optional"`       // 单次读取的最大历史消息数，按问答对对齐，默认 20；PostgreSQL 仍保留完整历史
	MaxContextTokens int           `json:"maxContextTokens,optional"` // LLM 完整消息上下文的严格近似 token 上限，默认 8000
	TTL              time.Duration `json:"ttl,optional"`              // Redis 快照/独立记忆与 InMemory 的过期时间，默认 24h；PostgreSQL 不使用
}

// Window 返回归一化后的记忆窗口大小，供读取方决定拉取的历史条数。
func (c Conf) Window() int {
	return c.normalize().MaxHistory
}

// ContextTokens 返回归一化后的 LLM 消息上下文 token 预算。
func (c Conf) ContextTokens() int {
	return c.normalize().MaxContextTokens
}

// normalize 返回归一化后的配置：
// 窗口最小为 2 且取偶数，保证截断永远切在 user/assistant 问答对边界上；TTL 非正时回落默认值。
func (c Conf) normalize() Conf {
	if c.MaxHistory <= 0 {
		c.MaxHistory = defaultMaxHistory
	}
	if c.MaxHistory < 2 {
		c.MaxHistory = 2
	}
	c.MaxHistory -= c.MaxHistory % 2
	if c.MaxContextTokens <= 0 {
		c.MaxContextTokens = defaultMaxContextTokens
	}
	if c.TTL <= 0 {
		c.TTL = defaultTTL
	}
	return c
}
