package memory

import (
	"context"

	"github.com/cloudwego/eino/schema"
	"github.com/zeromicro/go-zero/core/logx"
)

// durableMemory 是两级记忆依赖的 PostgreSQL 能力。
// 独立接口让缓存编排可以在不连接真实数据库的情况下完整测试。
type durableMemory interface {
	Manager
	Version(ctx context.Context, userId, conversationId string) (version int64, exists bool, err error)
	LoadSnapshot(ctx context.Context, userId, conversationId string, limit int) (snapshot Snapshot, exists bool, err error)
}

// snapshotCache 是 Redis 会话快照所需的最小能力。
type snapshotCache interface {
	LoadSnapshot(ctx context.Context, userId, conversationId string, expectedVersion int64, limit int) (snapshot Snapshot, hit bool, err error)
	StoreSnapshot(ctx context.Context, userId, conversationId string, snapshot Snapshot) error
	DeleteSnapshot(ctx context.Context, userId, conversationId string) error
	Clear(ctx context.Context, userId, conversationId string) error
}

// Tiered 以 PostgreSQL 为事实源、Redis 为最近会话窗口缓存。
//
// 写路径先提交 PostgreSQL，再使 Redis 快照失效；读路径先读取 PostgreSQL 版本，
// 只有 Redis 快照版本一致时才命中，否则回源 PostgreSQL 并重新回填。
// Redis 的读写失败只会降低缓存命中率，不会影响持久化结果或阻断读取。
type Tiered struct {
	conf    Conf
	durable durableMemory
	cache   snapshotCache
}

var _ Manager = (*Tiered)(nil)

// NewTiered 创建 PostgreSQL + Redis 两级会话记忆。
func NewTiered(durable *Postgres, cache *Redis, c Conf) *Tiered {
	return newTiered(durable, cache, c)
}

func newTiered(durable durableMemory, cache snapshotCache, c Conf) *Tiered {
	return &Tiered{
		conf:    c.normalize(),
		durable: durable,
		cache:   cache,
	}
}

// Append 以 PostgreSQL 提交成功作为本轮记忆写入成功的唯一标准。
// 提交后删除 Redis 快照；即使删除失败，下一次读取也会因版本不一致而回源。
func (m *Tiered) Append(ctx context.Context, userId, conversationId string, msgs ...*schema.Message) error {
	if err := m.durable.Append(ctx, userId, conversationId, msgs...); err != nil {
		return err
	}
	m.invalidateSnapshot(ctx, userId, conversationId)
	return nil
}

// History 优先使用版本一致的 Redis 快照，未命中或缓存异常时回源 PostgreSQL。
func (m *Tiered) History(ctx context.Context, userId, conversationId string, limit int) ([]*schema.Message, error) {
	if limit <= 0 {
		limit = m.conf.MaxHistory
	}
	// 超出标准窗口的诊断/管理读取直接访问持久层，避免把异常大的快照写进 Redis。
	if limit > m.conf.MaxHistory {
		return m.durable.History(ctx, userId, conversationId, limit)
	}

	version, exists, err := m.durable.Version(ctx, userId, conversationId)
	if err != nil {
		return nil, err
	}
	if !exists {
		m.invalidateSnapshot(ctx, userId, conversationId)
		return []*schema.Message{}, nil
	}

	if snapshot, hit, cacheErr := m.cache.LoadSnapshot(ctx, userId, conversationId, version, limit); cacheErr != nil {
		m.logCacheError(ctx, "load conversation snapshot from redis failed", userId, conversationId, cacheErr)
	} else if hit {
		return recentSnapshotMessages(snapshot, limit), nil
	}

	snapshot, exists, err := m.loadAndCacheSnapshot(ctx, userId, conversationId)
	if err != nil {
		return nil, err
	}
	if !exists {
		return []*schema.Message{}, nil
	}
	return recentSnapshotMessages(snapshot, limit), nil
}

// GetOrCreateTitle 优先复用版本一致快照中的稳定标题；尚未初始化时仍由 PostgreSQL 原子创建。
func (m *Tiered) GetOrCreateTitle(ctx context.Context, userId, conversationId, candidate string) (string, error) {
	version, exists, err := m.durable.Version(ctx, userId, conversationId)
	if err != nil {
		return "", err
	}
	if exists {
		if snapshot, hit, cacheErr := m.cache.LoadSnapshot(ctx, userId, conversationId, version, 1); cacheErr != nil {
			m.logCacheError(ctx, "load conversation title from redis failed", userId, conversationId, cacheErr)
		} else if hit && snapshot.TitleInitialized {
			return snapshot.Title, nil
		}

		snapshot, stillExists, loadErr := m.loadAndCacheSnapshot(ctx, userId, conversationId)
		if loadErr != nil {
			return "", loadErr
		}
		if stillExists && snapshot.TitleInitialized {
			return snapshot.Title, nil
		}
	}

	title, err := m.durable.GetOrCreateTitle(ctx, userId, conversationId, candidate)
	if err != nil {
		return "", err
	}
	m.invalidateSnapshot(ctx, userId, conversationId)
	return title, nil
}

// Clear 先删除 PostgreSQL 会话，再尽力清理 Redis 的快照及兼容键。
func (m *Tiered) Clear(ctx context.Context, userId, conversationId string) error {
	if err := m.durable.Clear(ctx, userId, conversationId); err != nil {
		return err
	}
	if err := m.cache.Clear(ctx, userId, conversationId); err != nil {
		m.logCacheError(ctx, "clear conversation cache failed", userId, conversationId, err)
	}
	return nil
}

func (m *Tiered) loadAndCacheSnapshot(ctx context.Context, userId, conversationId string) (Snapshot, bool, error) {
	snapshot, exists, err := m.durable.LoadSnapshot(ctx, userId, conversationId, m.conf.MaxHistory)
	if err != nil {
		return Snapshot{}, false, err
	}
	if !exists {
		m.invalidateSnapshot(ctx, userId, conversationId)
		return Snapshot{}, false, nil
	}
	if err := m.cache.StoreSnapshot(ctx, userId, conversationId, snapshot); err != nil {
		m.logCacheError(ctx, "store conversation snapshot in redis failed", userId, conversationId, err)
	}
	return snapshot, true, nil
}

func (m *Tiered) invalidateSnapshot(ctx context.Context, userId, conversationId string) {
	if err := m.cache.DeleteSnapshot(ctx, userId, conversationId); err != nil {
		m.logCacheError(ctx, "invalidate conversation snapshot failed", userId, conversationId, err)
	}
}

func (m *Tiered) logCacheError(ctx context.Context, message, userId, conversationId string, err error) {
	logx.WithContext(ctx).Errorw(message,
		logx.Field("user_id", userId),
		logx.Field("conversation_id", conversationId),
		logx.Field("error", err.Error()),
	)
}
