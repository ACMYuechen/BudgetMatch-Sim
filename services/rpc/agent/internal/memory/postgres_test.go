package memory

import (
	"context"
	"os"
	"testing"

	"budgetmatch-sim/services/rpc/agent/model/conversation_memory"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// newPostgresTestStore 使用真实 PostgreSQL 事务隔离测试数据；未配置测试 DSN 时跳过。
// 本地运行示例：
//
//	AGENT_MEMORY_TEST_PG_DSN="host=127.0.0.1 user=root password=123456 dbname=budgetmatch-sim port=15432 sslmode=disable" \
//	  go test ./services/rpc/agent/internal/memory -run TestPostgresPersistence
func newPostgresTestStore(t *testing.T, conf Conf) (*Postgres, *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("AGENT_MEMORY_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("AGENT_MEMORY_TEST_PG_DSN not set, skipping PostgreSQL integration test")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })

	store := NewPostgres(tx, conf)
	if err := store.CreateTable(); err != nil {
		t.Fatalf("CreateTable() error = %v", err)
	}
	if err := store.CreateTable(); err != nil {
		t.Fatalf("CreateTable() second call error = %v", err)
	}
	if err := store.CheckSchema(); err != nil {
		t.Fatalf("CheckSchema() error = %v", err)
	}
	return store, tx
}

// TestPostgresPersistence 验证管理器重建后仍能恢复标题和历史，且窗口读取不会删除旧消息。
func TestPostgresPersistence(t *testing.T) {
	ctx := context.Background()
	store, tx := newPostgresTestStore(t, Conf{MaxHistory: 2})
	userId := "test-user-" + uuid.NewString()
	conversationId := "test-conversation-" + uuid.NewString()

	title, err := store.GetOrCreateTitle(ctx, userId, conversationId, "学习桌面配置")
	if err != nil || title != "学习桌面配置" {
		t.Fatalf("GetOrCreateTitle() = %q, %v", title, err)
	}

	for _, round := range []string{"第一轮", "第二轮", "第三轮"} {
		if err := store.Append(ctx, userId, conversationId,
			schema.UserMessage(round),
			schema.AssistantMessage("回答"+round, nil)); err != nil {
			t.Fatalf("Append(%s) error = %v", round, err)
		}
	}

	// 模拟服务重启后重新构造 Manager；数据仍由数据库恢复。
	reopened := NewPostgres(tx, Conf{MaxHistory: 2})
	stableTitle, err := reopened.GetOrCreateTitle(ctx, userId, conversationId, "不应覆盖")
	if err != nil || stableTitle != "学习桌面配置" {
		t.Fatalf("reopened title = %q, %v", stableTitle, err)
	}
	version, exists, err := reopened.Version(ctx, userId, conversationId)
	if err != nil || !exists || version != 4 {
		t.Fatalf("Version() = %d, %v, %v; want 4, true, nil", version, exists, err)
	}

	emptyTitleConversationId := "test-empty-title-" + uuid.NewString()
	if first, err := reopened.GetOrCreateTitle(ctx, userId, emptyTitleConversationId, ""); err != nil || first != "" {
		t.Fatalf("first empty title = %q, %v", first, err)
	}
	if again, err := reopened.GetOrCreateTitle(ctx, userId, emptyTitleConversationId, "不应覆盖空标题"); err != nil || again != "" {
		t.Fatalf("empty first title was overwritten: %q, %v", again, err)
	}

	history, err := reopened.History(ctx, userId, conversationId, 0)
	if err != nil {
		t.Fatalf("reopened History() error = %v", err)
	}
	if len(history) != 2 || history[0].Content != "第三轮" || history[1].Content != "回答第三轮" {
		t.Fatalf("unexpected recent history: %+v", history)
	}
	snapshot, exists, err := reopened.LoadSnapshot(ctx, userId, conversationId, 2)
	if err != nil || !exists {
		t.Fatalf("LoadSnapshot() exists=%v error=%v", exists, err)
	}
	if snapshot.Version != 4 || snapshot.CachedLimit != 2 || snapshot.Title != "学习桌面配置" || len(snapshot.Messages) != 2 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}

	var storedCount int64
	if err := tx.Model(&conversation_memory.AgentConversationMessage{}).
		Where("user_id = ? AND conversation_id = ?", userId, conversationId).
		Count(&storedCount).Error; err != nil {
		t.Fatalf("count stored messages: %v", err)
	}
	if storedCount != 6 {
		t.Fatalf("PostgreSQL should retain all messages, got %d", storedCount)
	}

	otherUserHistory, err := reopened.History(ctx, "other-user", conversationId, 0)
	if err != nil || len(otherUserHistory) != 0 {
		t.Fatalf("history leaked across users: %v, %v", otherUserHistory, err)
	}

	if err := reopened.Clear(ctx, userId, conversationId); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	if err := tx.Model(&conversation_memory.AgentConversationMessage{}).
		Where("user_id = ? AND conversation_id = ?", userId, conversationId).
		Count(&storedCount).Error; err != nil {
		t.Fatalf("count messages after clear: %v", err)
	}
	if storedCount != 0 {
		t.Fatalf("expected cascading message cleanup, got %d", storedCount)
	}
}
