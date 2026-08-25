// Package memory 提供推荐 Agent 的跨请求会话记忆管理。
// Manager 保留模型上下文所需的消息窗口能力；ConversationStore 在其上增加
// 可恢复会话、完整轮次、结构化状态、幂等与会话级串行控制。
// Redis/InMemory 可按 TTL 回收短期数据，PostgreSQL 实现长期保留直到显式删除。
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudwego/eino/schema"
)

// Manager 定义注入模型上下文所需的消息记忆兼容接口。
//
// 实现约定：
//   - Append 追加消息（推荐场景按 user+assistant 成对写入），会话不存在时自动创建；
//   - History 返回最近 limit 条消息（时间正序），会话不存在或已过期时返回空切片而非错误；
//   - History 返回的消息必须是深拷贝，调用方修改返回值不得影响存储内容。
//   - GetOrCreateTitle 原子地保存首个候选标题，后续调用始终返回首次保存的标题；
//   - Clear 同时清除消息与标题。
type Manager interface {
	Append(ctx context.Context, userId, conversationId string, msgs ...*schema.Message) error
	History(ctx context.Context, userId, conversationId string, limit int) ([]*schema.Message, error)
	GetOrCreateTitle(ctx context.Context, userId, conversationId, candidate string) (string, error)
	Clear(ctx context.Context, userId, conversationId string) error
}

// conversationKey 同时使用认证用户和会话标识定位存储。
// 客户端仅提供会话标识时，无法读取其他用户的历史记录。
func conversationKey(userId, conversationId string) string {
	return userId + ":" + conversationId
}

// storedMessage 是消息的持久化载体，内嵌 eino 消息并附加写入时间便于排查。
//
// 注意：Extra 等 map[string]any 字段经 JSON round-trip 会丢失具体类型；
// 当前记忆只存最终问答文本（Extra 恒空），不受影响。
type storedMessage struct {
	schema.Message
	CreatedAt time.Time `json:"created_at"`
}

// encodeMessage 把消息序列化为存储字节。所有实现统一走 JSON 编码，
// 读取时解码出全新对象，从构造上保证深拷贝语义。
func encodeMessage(msg *schema.Message, now time.Time) ([]byte, error) {
	if msg == nil {
		return nil, fmt.Errorf("memory: message is nil")
	}
	data, err := json.Marshal(storedMessage{Message: *msg, CreatedAt: now})
	if err != nil {
		return nil, fmt.Errorf("memory: encode message: %w", err)
	}
	return data, nil
}

// decodeMessage 从存储字节还原消息，返回的对象与存储内容无共享。
func decodeMessage(data []byte) (*schema.Message, error) {
	var stored storedMessage
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("memory: decode message: %w", err)
	}
	msg := stored.Message
	return &msg, nil
}
