package conversation_memory

import (
	"reflect"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestConversationMemorySchemaMetadata(t *testing.T) {
	cache := &sync.Map{}
	conversationSchema, err := schema.Parse(&AgentConversation{}, cache, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse conversation schema: %v", err)
	}
	primaryColumns := make([]string, 0, len(conversationSchema.PrimaryFields))
	for _, field := range conversationSchema.PrimaryFields {
		primaryColumns = append(primaryColumns, field.DBName)
	}
	if want := []string{"user_id", "conversation_id"}; !reflect.DeepEqual(primaryColumns, want) {
		t.Fatalf("conversation primary columns = %v, want %v", primaryColumns, want)
	}

	messageSchema, err := schema.Parse(&AgentConversationMessage{}, cache, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse message schema: %v", err)
	}
	relation := messageSchema.Relationships.Relations["Conversation"]
	if relation == nil {
		t.Fatal("message conversation relationship is missing")
	}
	constraint := relation.ParseConstraint()
	if constraint == nil {
		t.Fatal("message conversation constraint is missing")
	}
	if constraint.Name != messageConversationFK || constraint.OnDelete != "CASCADE" {
		t.Fatalf("constraint = %s on delete %s", constraint.Name, constraint.OnDelete)
	}
	foreignColumns := make([]string, 0, len(constraint.ForeignKeys))
	for _, field := range constraint.ForeignKeys {
		foreignColumns = append(foreignColumns, field.DBName)
	}
	referenceColumns := make([]string, 0, len(constraint.References))
	for _, field := range constraint.References {
		referenceColumns = append(referenceColumns, field.DBName)
	}
	if want := []string{"user_id", "conversation_id"}; !reflect.DeepEqual(foreignColumns, want) {
		t.Fatalf("constraint foreign columns = %v, want %v", foreignColumns, want)
	}
	if want := []string{"user_id", "conversation_id"}; !reflect.DeepEqual(referenceColumns, want) {
		t.Fatalf("constraint reference columns = %v, want %v", referenceColumns, want)
	}

	var recentIndex *schema.Index
	for _, index := range messageSchema.ParseIndexes() {
		if index.Name == recentMessageIndex {
			recentIndex = index
			break
		}
	}
	if recentIndex == nil {
		t.Fatalf("index %s is missing", recentMessageIndex)
	}
	indexColumns := make([]string, 0, len(recentIndex.Fields))
	for _, field := range recentIndex.Fields {
		indexColumns = append(indexColumns, field.Field.DBName)
	}
	if want := []string{"user_id", "conversation_id", "id"}; !reflect.DeepEqual(indexColumns, want) {
		t.Fatalf("recent index columns = %v, want %v", indexColumns, want)
	}
	if recentIndex.Fields[2].Sort != "desc" {
		t.Fatalf("recent index id sort = %q, want desc", recentIndex.Fields[2].Sort)
	}
}
