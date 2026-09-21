package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/redis/go-redis/v9"
)

// Redis 是 ConversationStore 的 Redis 实现，多实例部署时共享短期会话记忆。
//
// 独立 Redis 模式下，每个会话用 LIST 保存 JSON 编码的消息，另用 STRING 保存稳定标题。
// 写入用 pipeline 执行 RPUSH + LTRIM + EXPIRE：写时截断保证窗口上限，滑动 TTL 让活跃会话不过期。
// 与 PostgreSQL 组成两级记忆时，则使用单独的 STRING 原子保存带数据库版本的完整窗口快照。
// 只依赖 redis.UniversalClient 接口，便于用 miniredis 做单元测试。
type Redis struct {
	conf   Conf
	client redis.UniversalClient
}

// 确保 Redis 实现包含完整会话能力的 ConversationStore。
var _ ConversationStore = (*Redis)(nil)

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

// conversationMetaKey 保存可列出、可恢复的会话元数据和结构化状态。
func conversationMetaKey(userId, conversationId string) string {
	return "agent:user:" + userId + ":conv:" + conversationId + ":meta"
}

// conversationTurnsKey 保存按 sequence 排列的 turn_id 列表，用于历史分页。
func conversationTurnsKey(userId, conversationId string) string {
	return "agent:user:" + userId + ":conv:" + conversationId + ":turns"
}

// conversationTurnDataKey 保存 turn_id 到完整轮次 JSON 的映射。
func conversationTurnDataKey(userId, conversationId string) string {
	return "agent:user:" + userId + ":conv:" + conversationId + ":turn-data"
}

// conversationIndexKey 保存用户会话的最近更新时间有序索引。
func conversationIndexKey(userId string) string {
	return "agent:user:" + userId + ":conversations"
}

// conversationLockRedisKey 是跨实例串行化同一用户会话的锁 key。
func conversationLockRedisKey(userId, conversationId string) string {
	return "agent:user:" + userId + ":conv:" + conversationId + ":lock"
}

// GetConversation 从独立元数据 key 读取会话，不依赖可能被裁剪的消息窗口。
func (m *Redis) GetConversation(ctx context.Context, userId, conversationId string) (Conversation, bool, error) {
	return readRedisConversation(ctx, m.client, userId, conversationId)
}

func readRedisConversation(ctx context.Context, reader redis.Cmdable, userId, conversationId string) (Conversation, bool, error) {
	data, err := reader.Get(ctx, conversationMetaKey(userId, conversationId)).Bytes()
	if err == redis.Nil {
		return Conversation{}, false, nil
	}
	if err != nil {
		return Conversation{}, false, fmt.Errorf("memory: get redis conversation: %w", err)
	}
	var conversation Conversation
	if err := json.Unmarshal(data, &conversation); err != nil {
		return Conversation{}, false, fmt.Errorf("memory: decode redis conversation: %w", err)
	}
	return conversation, true, nil
}

// FindTurn 通过 Hash 字段 O(1) 定位轮次，支持 turn_id 幂等重放。
func (m *Redis) FindTurn(ctx context.Context, userId, conversationId, turnId string) (Turn, bool, error) {
	return readRedisTurn(ctx, m.client, userId, conversationId, turnId)
}

func readRedisTurn(ctx context.Context, reader redis.Cmdable, userId, conversationId, turnId string) (Turn, bool, error) {
	item, err := reader.HGet(ctx, conversationTurnDataKey(userId, conversationId), turnId).Bytes()
	if err == redis.Nil {
		return Turn{}, false, nil
	}
	if err != nil {
		return Turn{}, false, fmt.Errorf("memory: read redis turn: %w", err)
	}
	var turn Turn
	if err := json.Unmarshal(item, &turn); err != nil {
		return Turn{}, false, fmt.Errorf("memory: decode redis turn: %w", err)
	}
	return turn, true, nil
}

// SaveTurn 同步刷新元数据、完整轮次、会话索引和滚动消息窗口的滑动 TTL。
// 服务调用复用会话锁，直接调用自动获取锁；提交同时校验租约，过期持有者不得写入。
func (m *Redis) SaveTurn(ctx context.Context, req SaveTurnReq) (Conversation, Turn, error) {
	if req.UserId == "" || req.ConversationId == "" || req.TurnId == "" {
		return Conversation{}, Turn{}, fmt.Errorf("memory: user id, conversation id or turn id is empty")
	}
	if !json.Valid(req.ResultJSON) {
		return Conversation{}, Turn{}, fmt.Errorf("memory: result is not valid JSON")
	}
	var conversation Conversation
	var turn Turn
	err := m.writeWithLease(ctx, req.UserId, req.ConversationId, func(locked context.Context, tx *redis.Tx) error {
		var err error
		conversation, turn, err = m.saveTurnLocked(locked, tx, req)
		return err
	})
	if err != nil {
		return Conversation{}, Turn{}, err
	}
	return conversation, turn, nil
}

func (m *Redis) saveTurnLocked(ctx context.Context, tx *redis.Tx, req SaveTurnReq) (Conversation, Turn, error) {
	if existing, found, err := readRedisTurn(ctx, tx, req.UserId, req.ConversationId, req.TurnId); err != nil {
		return Conversation{}, Turn{}, err
	} else if found {
		conversation, _, err := readRedisConversation(ctx, tx, req.UserId, req.ConversationId)
		return conversation, existing, err
	}
	if req.Now.IsZero() {
		req.Now = time.Now()
	}
	conversation, exists, err := readRedisConversation(ctx, tx, req.UserId, req.ConversationId)
	if err != nil {
		return Conversation{}, Turn{}, err
	}
	if !exists {
		conversation = Conversation{UserId: req.UserId, ConversationId: req.ConversationId,
			Title: req.Title, CreatedAt: req.Now}
	}
	if conversation.Title == "" {
		conversation.Title = req.Title
	}
	conversation.State = req.Intent
	conversation.Version++
	conversation.TurnCount++
	conversation.UpdatedAt = req.Now
	turn := Turn{UserId: req.UserId, ConversationId: req.ConversationId, TurnId: req.TurnId,
		Sequence: conversation.TurnCount, Status: TurnStatusCompleted, Query: req.Query,
		BudgetCents: req.BudgetCents, MaxItems: req.MaxItems, Intent: req.Intent,
		ResultJSON: append(json.RawMessage(nil), req.ResultJSON...), Summary: req.Summary,
		CreatedAt: req.Now, CompletedAt: req.Now}
	conversationJSON, err := json.Marshal(conversation)
	if err != nil {
		return Conversation{}, Turn{}, fmt.Errorf("memory: encode redis conversation: %w", err)
	}
	turnJSON, err := json.Marshal(turn)
	if err != nil {
		return Conversation{}, Turn{}, fmt.Errorf("memory: encode redis turn: %w", err)
	}
	userMessage, err := encodeMessage(schema.UserMessage(req.Query), req.Now)
	if err != nil {
		return Conversation{}, Turn{}, err
	}
	assistantMessage, err := encodeMessage(schema.AssistantMessage(req.Summary, nil), req.Now)
	if err != nil {
		return Conversation{}, Turn{}, err
	}
	_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, conversationMetaKey(req.UserId, req.ConversationId), conversationJSON, m.conf.TTL)
		pipe.RPush(ctx, conversationTurnsKey(req.UserId, req.ConversationId), req.TurnId)
		pipe.Expire(ctx, conversationTurnsKey(req.UserId, req.ConversationId), m.conf.TTL)
		pipe.HSet(ctx, conversationTurnDataKey(req.UserId, req.ConversationId), req.TurnId, turnJSON)
		pipe.Expire(ctx, conversationTurnDataKey(req.UserId, req.ConversationId), m.conf.TTL)
		pipe.RPush(ctx, convKey(req.UserId, req.ConversationId), userMessage, assistantMessage)
		pipe.LTrim(ctx, convKey(req.UserId, req.ConversationId), int64(-m.conf.MaxHistory), -1)
		pipe.Expire(ctx, convKey(req.UserId, req.ConversationId), m.conf.TTL)
		pipe.Set(ctx, titleKey(req.UserId, req.ConversationId), conversation.Title, m.conf.TTL)
		pipe.ZAdd(ctx, conversationIndexKey(req.UserId), redis.Z{Score: float64(req.Now.UnixMilli()), Member: req.ConversationId})
		pipe.Expire(ctx, conversationIndexKey(req.UserId), m.conf.TTL)
		return nil
	})
	if err != nil {
		return Conversation{}, Turn{}, fmt.Errorf("memory: save redis turn: %w", err)
	}
	return conversation, turn, nil
}

// ListConversations 从用户级有序索引读取会话，并顺带移除已过期的悬空成员。
func (m *Redis) ListConversations(ctx context.Context, userId string, page, pageSize int) ([]Conversation, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	ids, err := m.client.ZRevRange(ctx, conversationIndexKey(userId), 0, -1).Result()
	if err != nil {
		return nil, 0, fmt.Errorf("memory: list redis conversation ids: %w", err)
	}
	items := make([]Conversation, 0, len(ids))
	stale := make([]any, 0)
	for _, id := range ids {
		conversation, exists, err := m.GetConversation(ctx, userId, id)
		if err != nil {
			return nil, 0, err
		}
		if !exists {
			stale = append(stale, id)
			continue
		}
		items = append(items, conversation)
	}
	if len(stale) > 0 {
		_ = m.client.ZRem(ctx, conversationIndexKey(userId), stale...).Err()
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	total := int64(len(items))
	start := (page - 1) * pageSize
	if start >= len(items) {
		return []Conversation{}, total, nil
	}
	end := min(start+pageSize, len(items))
	return items[start:end], total, nil
}

// ListTurns 根据有序 turn_id 列表分页，再批量读取 Hash 中的完整轮次。
func (m *Redis) ListTurns(ctx context.Context, userId, conversationId string, page, pageSize int) (Conversation, []Turn, int64, bool, error) {
	page, pageSize = normalizePage(page, pageSize)
	conversation, exists, err := m.GetConversation(ctx, userId, conversationId)
	if err != nil || !exists {
		return Conversation{}, []Turn{}, 0, exists, err
	}
	total, err := m.client.LLen(ctx, conversationTurnsKey(userId, conversationId)).Result()
	if err != nil {
		return Conversation{}, nil, 0, true, fmt.Errorf("memory: count redis turns: %w", err)
	}
	start := int64((page - 1) * pageSize)
	if start >= total {
		return conversation, []Turn{}, total, true, nil
	}
	turnIds, err := m.client.LRange(ctx, conversationTurnsKey(userId, conversationId), start, start+int64(pageSize)-1).Result()
	if err != nil {
		return Conversation{}, nil, 0, true, fmt.Errorf("memory: list redis turn ids: %w", err)
	}
	values, err := m.client.HMGet(ctx, conversationTurnDataKey(userId, conversationId), turnIds...).Result()
	if err != nil {
		return Conversation{}, nil, 0, true, fmt.Errorf("memory: load redis turns: %w", err)
	}
	turns := make([]Turn, 0, len(values))
	for index, value := range values {
		item, ok := value.(string)
		if !ok {
			return Conversation{}, nil, 0, true, fmt.Errorf("memory: redis turn %s is missing", turnIds[index])
		}
		var turn Turn
		if err := json.Unmarshal([]byte(item), &turn); err != nil {
			return Conversation{}, nil, 0, true, fmt.Errorf("memory: decode redis turn: %w", err)
		}
		turns = append(turns, turn)
	}
	return conversation, turns, total, true, nil
}

// DeleteConversation 清理一个会话的所有 Redis key 及用户级索引成员。
func (m *Redis) DeleteConversation(ctx context.Context, userId, conversationId string) (bool, error) {
	var exists int64
	err := m.writeWithLease(ctx, userId, conversationId, func(locked context.Context, tx *redis.Tx) error {
		var err error
		exists, err = tx.Exists(locked, conversationMetaKey(userId, conversationId)).Result()
		if err != nil {
			return fmt.Errorf("memory: check redis conversation: %w", err)
		}
		_, err = tx.TxPipelined(locked, func(pipe redis.Pipeliner) error {
			pipe.Del(locked, convKey(userId, conversationId), titleKey(userId, conversationId), snapshotKey(userId, conversationId),
				conversationMetaKey(userId, conversationId), conversationTurnsKey(userId, conversationId),
				conversationTurnDataKey(userId, conversationId))
			pipe.ZRem(locked, conversationIndexKey(userId), conversationId)
			return nil
		})
		return err
	})
	return exists > 0 && err == nil, err
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

	return m.writeWithLease(ctx, userId, conversationId, func(locked context.Context, tx *redis.Tx) error {
		key := convKey(userId, conversationId)
		_, err := tx.TxPipelined(locked, func(pipe redis.Pipeliner) error {
			pipe.RPush(locked, key, values...)
			pipe.LTrim(locked, key, int64(-m.conf.MaxHistory), -1)
			pipe.Expire(locked, key, m.conf.TTL)
			pipe.Expire(locked, titleKey(userId, conversationId), m.conf.TTL)
			return nil
		})
		return err
	})
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

	var title string
	err := m.writeWithLease(ctx, userId, conversationId, func(locked context.Context, tx *redis.Tx) error {
		var err error
		title, err = tx.Get(locked, titleKey(userId, conversationId)).Result()
		createTitle := err == redis.Nil
		if err != nil && !createTitle {
			return fmt.Errorf("memory: read conversation title: %w", err)
		}
		if createTitle {
			title = candidate
		}
		_, exists, err := readRedisConversation(locked, tx, userId, conversationId)
		if err != nil || exists && !createTitle {
			return err
		}
		now := time.Now()
		conversation := Conversation{UserId: userId, ConversationId: conversationId, Title: title,
			CreatedAt: now, UpdatedAt: now}
		data, err := json.Marshal(conversation)
		if err != nil {
			return err
		}
		_, err = tx.TxPipelined(locked, func(pipe redis.Pipeliner) error {
			if createTitle {
				pipe.Set(locked, titleKey(userId, conversationId), title, m.conf.TTL)
			}
			if !exists {
				pipe.Set(locked, conversationMetaKey(userId, conversationId), data, m.conf.TTL)
				pipe.ZAdd(locked, conversationIndexKey(userId), redis.Z{Score: float64(now.UnixMilli()), Member: conversationId})
				pipe.Expire(locked, conversationIndexKey(userId), m.conf.TTL)
			}
			return nil
		})
		return err
	})
	if err != nil {
		return "", err
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
	_, err := m.DeleteConversation(ctx, userId, conversationId)
	return err
}
