package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/redis/go-redis/v9"
)

// Redis 是 Manager 的 Redis 实现，多实例部署时共享会话记忆。
//
// 独立 Redis 模式下，每个会话用 LIST 保存 JSON 编码的消息，另用 STRING 保存稳定标题。
// 写入用 pipeline 执行 RPUSH + LTRIM + EXPIRE：写时截断保证窗口上限，滑动 TTL 让活跃会话不过期。
// 与 PostgreSQL 组成两级记忆时，则使用单独的 STRING 原子保存带数据库版本的完整窗口快照。
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
func convKey(userId, conversationId string) string {
	return "agent:user:" + userId + ":conv:" + conversationId + ":msgs"
}

// titleKey 返回会话稳定标题的 Redis key。
func titleKey(userId, conversationId string) string {
	return "agent:user:" + userId + ":conv:" + conversationId + ":title"
}

// snapshotKey 返回 PostgreSQL + Redis 两级记忆使用的原子快照 key。
func snapshotKey(userId, conversationId string) string {
	return "agent:user:" + userId + ":conv:" + conversationId + ":snapshot"
}

// Append 追加消息、按窗口截断并刷新 TTL，会话不存在时自动创建。
func (m *Redis) Append(ctx context.Context, userId, conversationId string, msgs ...*schema.Message) error {
	if userId == "" || conversationId == "" {
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

	key := convKey(userId, conversationId)
	pipe := m.client.Pipeline()
	pipe.RPush(ctx, key, values...)
	pipe.LTrim(ctx, key, int64(-m.conf.MaxHistory), -1)
	pipe.Expire(ctx, key, m.conf.TTL)
	pipe.Expire(ctx, titleKey(userId, conversationId), m.conf.TTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("memory: append to redis: %w", err)
	}
	return nil
}

// History 返回最近 limit 条消息（时间正序）；limit 非正时使用窗口大小。
// 会话不存在或已过期时返回空切片。
func (m *Redis) History(ctx context.Context, userId, conversationId string, limit int) ([]*schema.Message, error) {
	if limit <= 0 {
		limit = m.conf.MaxHistory
	}

	items, err := m.client.LRange(ctx, convKey(userId, conversationId), int64(-limit), -1).Result()
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

// GetOrCreateTitle 原子保存首个候选标题；已存在时返回原始标题。
func (m *Redis) GetOrCreateTitle(ctx context.Context, userId, conversationId, candidate string) (string, error) {
	if userId == "" || conversationId == "" {
		return "", fmt.Errorf("memory: user id or conversation id is empty")
	}

	key := titleKey(userId, conversationId)
	created, err := m.client.SetNX(ctx, key, candidate, m.conf.TTL).Result()
	if err != nil {
		return "", fmt.Errorf("memory: create conversation title: %w", err)
	}
	if created {
		return candidate, nil
	}

	title, err := m.client.Get(ctx, key).Result()
	if err != nil {
		return "", fmt.Errorf("memory: read conversation title: %w", err)
	}
	return title, nil
}

// LoadSnapshot 读取版本匹配且覆盖所需窗口的 Redis 快照。
// 整份快照存于一个 STRING 中，因此版本、标题和消息不会出现跨 key 的混合状态。
func (m *Redis) LoadSnapshot(ctx context.Context, userId, conversationId string, expectedVersion int64, limit int) (Snapshot, bool, error) {
	data, err := m.client.Get(ctx, snapshotKey(userId, conversationId)).Bytes()
	if err == redis.Nil {
		return Snapshot{}, false, nil
	}
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("memory: read redis conversation snapshot: %w", err)
	}

	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return Snapshot{}, false, fmt.Errorf("memory: decode redis conversation snapshot: %w", err)
	}
	if snapshot.Version != expectedVersion || snapshot.CachedLimit < limit {
		return Snapshot{}, false, nil
	}
	return snapshot, true, nil
}

// StoreSnapshot 原子写入会话窗口快照并设置短期 TTL。
func (m *Redis) StoreSnapshot(ctx context.Context, userId, conversationId string, snapshot Snapshot) error {
	if userId == "" || conversationId == "" {
		return fmt.Errorf("memory: user id or conversation id is empty")
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("memory: encode redis conversation snapshot: %w", err)
	}
	if err := m.client.Set(ctx, snapshotKey(userId, conversationId), data, m.conf.TTL).Err(); err != nil {
		return fmt.Errorf("memory: store redis conversation snapshot: %w", err)
	}
	return nil
}

// DeleteSnapshot 删除两级记忆的 Redis 快照；PostgreSQL 写入提交后调用。
func (m *Redis) DeleteSnapshot(ctx context.Context, userId, conversationId string) error {
	if err := m.client.Del(ctx, snapshotKey(userId, conversationId)).Err(); err != nil {
		return fmt.Errorf("memory: delete redis conversation snapshot: %w", err)
	}
	return nil
}

// Clear 删除独立 Redis 模式的消息/标题以及两级记忆快照。
func (m *Redis) Clear(ctx context.Context, userId, conversationId string) error {
	if err := m.client.Del(ctx, convKey(userId, conversationId), titleKey(userId, conversationId), snapshotKey(userId, conversationId)).Err(); err != nil {
		return fmt.Errorf("memory: clear conversation: %w", err)
	}
	return nil
}
