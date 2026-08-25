package memory

import "github.com/cloudwego/eino/schema"

// Snapshot 是 PostgreSQL 与 Redis 两级记忆之间传递的会话快照。
// Version 来自 PostgreSQL，会话发生持久化变更时递增；Redis 只有在版本一致时才可命中。
type Snapshot struct {
	Version          int64             `json:"version"`
	CachedLimit      int               `json:"cached_limit"`
	Title            string            `json:"title"`
	TitleInitialized bool              `json:"title_initialized"`
	Messages         []*schema.Message `json:"messages"`
}

// recentSnapshotMessages 从快照中返回最近 limit 条消息。
// 快照来自本次数据库查询或 Redis JSON 反序列化，返回切片不会共享持久存储对象。
func recentSnapshotMessages(snapshot Snapshot, limit int) []*schema.Message {
	messages := snapshot.Messages
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	return messages
}
