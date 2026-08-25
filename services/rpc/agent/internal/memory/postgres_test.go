package memory

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"budgetmatch-sim/services/rpc/agent/model/conversation_memory"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newPostgresTestStore(t *testing.T, conf Conf) (*Postgres, *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("AGENT_MEMORY_TEST_PG_DSN")
	if dsn == "" {
		dsn = os.Getenv("RAG_TEST_PG_DSN")
	}
	if dsn == "" {
		t.Skip("AGENT_MEMORY_TEST_PG_DSN or RAG_TEST_PG_DSN not set, skipping PostgreSQL integration test")
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
	if err := store.CheckSchema(); err != nil {
		t.Fatalf("CheckSchema() error = %v", err)
	}
	return store, tx
}

func TestPostgresConversationTurnPersistence(t *testing.T) {
	ctx := context.Background()
	store, tx := newPostgresTestStore(t, Conf{MaxHistory: 2})
	userId := "test-user-" + uuid.NewString()
	conversationId := "test-conversation-" + uuid.NewString()
	title, err := store.GetOrCreateTitle(ctx, userId, conversationId, "学习桌面配置")
	if err != nil || title != "学习桌面配置" {
		t.Fatalf("GetOrCreateTitle() = %q, %v", title, err)
	}

	var lastTurnId string
	for index, query := range []string{"第一轮", "第二轮", "第三轮"} {
		lastTurnId = uuid.NewString()
		resultJSON, _ := json.Marshal(map[string]any{"summary": "回答" + query, "round": index + 1})
		_, turn, err := store.SaveTurn(ctx, SaveTurnReq{
			UserId: userId, ConversationId: conversationId, TurnId: lastTurnId,
			Title: "不应覆盖", Query: query, Intent: IntentState{BudgetCents: int64(index+1) * 100000, MaxItems: 3},
			ResultJSON: resultJSON, Summary: "回答" + query,
		})
		if err != nil || turn.Sequence != int64(index+1) {
			t.Fatalf("SaveTurn(%s) = %+v, %v", query, turn, err)
		}
	}

	reopened := NewPostgres(tx, Conf{MaxHistory: 2})
	conversation, exists, err := reopened.GetConversation(ctx, userId, conversationId)
	if err != nil || !exists || conversation.Title != "学习桌面配置" || conversation.Version != 4 || conversation.TurnCount != 3 {
		t.Fatalf("reopened conversation = %+v, exists=%v, err=%v", conversation, exists, err)
	}
	if conversation.State.BudgetCents != 300000 {
		t.Fatalf("latest structured state = %+v", conversation.State)
	}

	history, err := reopened.History(ctx, userId, conversationId, 2)
	if err != nil || len(history) != 2 || history[0].Content != "第三轮" || history[1].Content != "回答第三轮" {
		t.Fatalf("recent history = %+v, err=%v", history, err)
	}
	_, turns, total, exists, err := reopened.ListTurns(ctx, userId, conversationId, 1, 20)
	if err != nil || !exists || total != 3 || len(turns) != 3 {
		t.Fatalf("ListTurns() total=%d exists=%v len=%d err=%v", total, exists, len(turns), err)
	}

	// 相同 turn_id 的网络重试不新增轮次，也不覆盖原结果。
	resultJSON, _ := json.Marshal(map[string]any{"summary": "错误覆盖"})
	_, retried, err := reopened.SaveTurn(ctx, SaveTurnReq{
		UserId: userId, ConversationId: conversationId, TurnId: lastTurnId,
		Title: "覆盖", Query: "重复请求", ResultJSON: resultJSON, Summary: "错误覆盖",
	})
	if err != nil || retried.Query != "第三轮" {
		t.Fatalf("idempotent SaveTurn() = %+v, %v", retried, err)
	}

	var storedCount int64
	if err := tx.Model(&conversation_memory.AgentConversationTurn{}).
		Where("user_id = ? AND conversation_id = ?", userId, conversationId).
		Count(&storedCount).Error; err != nil || storedCount != 3 {
		t.Fatalf("stored turn count = %d, err=%v", storedCount, err)
	}
	if _, exists, err := reopened.GetConversation(ctx, "other-user", conversationId); err != nil || exists {
		t.Fatalf("conversation leaked across users: exists=%v err=%v", exists, err)
	}
	deleted, err := reopened.DeleteConversation(ctx, userId, conversationId)
	if err != nil || !deleted {
		t.Fatalf("DeleteConversation() = %v, %v", deleted, err)
	}
	if err := tx.Model(&conversation_memory.AgentConversationTurn{}).
		Where("user_id = ? AND conversation_id = ?", userId, conversationId).
		Count(&storedCount).Error; err != nil || storedCount != 0 {
		t.Fatalf("cascading turn cleanup count = %d, err=%v", storedCount, err)
	}
}
