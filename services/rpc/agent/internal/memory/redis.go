package memory

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/redis/go-redis/v9"
)

// Redis 是 Manager 的 Redis 实现，多实例部署时共享会话记忆。
//
// 存储结构：每个会话一个 LIST，key 为 agent:conv:{id}:msgs，元素为 JSON 编码的消息。
// 写入用 pipeline 执行 RPUSH + LTRIM + EXPIRE：写时截断保证窗口上限，滑动 TTL 让活跃会话不过期。
// 只依赖 redis.UniversalClient 接口，便于用 miniredis 做单元测试。
type Redis struct {
	conf   Conf
	client redis.UniversalClient
}

// 确保 Redis 实现 Manager。
var _ Manager = (*Redis)(nil)

// NewRedis 基于已建立的 Redis 客户端创建会话记忆。
func NewRedis(client redis.UniversalClient, c Conf) *Redis {
	return &Redis{
		conf:   c.normalize(),
		client: client,
	}
}

// convKey 返回会话消息列表的 Redis key。
// convKey 必须包含用户标识，避免不同用户使用相同会话标识时发生冲突。
func convKey(userID, conversationID string) string {
	return "agent:user:" + userID + ":conv:" + conversationID + ":msgs"
}

// Append 追加消息、按窗口截断并刷新 TTL，会话不存在时自动创建。
func (m *Redis) Append(ctx context.Context, userID, conversationID string, msgs ...*schema.Message) error {
	if userID == "" || conversationID == "" {
		return fmt.Errorf("memory: user id or conversation id is empty")
	}
	if len(msgs) == 0 {
		return nil
	}

	now := time.Now()
	values := make([]any, 0, len(msgs))
	for _, msg := range msgs {
		data, err := encodeMessage(msg, now)
		if err != nil {
			return err
		}
		values = append(values, data)
	}

	key := convKey(userID, conversationID)
	pipe := m.client.Pipeline()
	pipe.RPush(ctx, key, values...)
	pipe.LTrim(ctx, key, int64(-m.conf.MaxHistory), -1)
	pipe.Expire(ctx, key, m.conf.TTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("memory: append to redis: %w", err)
	}
	return nil
}

// History 返回最近 limit 条消息（时间正序）；limit 非正时使用窗口大小。
// 会话不存在或已过期时返回空切片。
func (m *Redis) History(ctx context.Context, userID, conversationID string, limit int) ([]*schema.Message, error) {
	if limit <= 0 {
		limit = m.conf.MaxHistory
	}

	items, err := m.client.LRange(ctx, convKey(userID, conversationID), int64(-limit), -1).Result()
	if err != nil {
		return nil, fmt.Errorf("memory: read history from redis: %w", err)
	}

	out := make([]*schema.Message, 0, len(items))
	for _, item := range items {
		msg, err := decodeMessage([]byte(item))
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, nil
}

// Clear 删除会话的全部历史。
func (m *Redis) Clear(ctx context.Context, userID, conversationID string) error {
	if err := m.client.Del(ctx, convKey(userID, conversationID)).Err(); err != nil {
		return fmt.Errorf("memory: clear conversation: %w", err)
	}
	return nil
}
