package conversation_memory

import (
	"database/sql/driver"
	"reflect"
	"strings"
	"sync"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestConversationMemorySchemaMetadata(t *testing.T) {
	cache := &sync.Map{}
	conversationSchema, err := schema.Parse(&AgentConversation{}, cache, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse conversation schema: %v", err)
	}
	primaryColumns := fieldNames(conversationSchema.PrimaryFields)
	if want := []string{"user_id", "conversation_id"}; !reflect.DeepEqual(primaryColumns, want) {
		t.Fatalf("conversation primary columns = %v, want %v", primaryColumns, want)
	}

	turnSchema, err := schema.Parse(&AgentConversationTurn{}, cache, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse turn schema: %v", err)
	}
	if want := []string{"user_id", "conversation_id", "turn_id"}; !reflect.DeepEqual(fieldNames(turnSchema.PrimaryFields), want) {
		t.Fatalf("turn primary columns = %v, want %v", fieldNames(turnSchema.PrimaryFields), want)
	}
	relation := conversationSchema.Relationships.Relations["Turns"]
	if relation == nil || relation.ParseConstraint() == nil {
		t.Fatal("conversation turns relationship is missing")
	}
	constraint := relation.ParseConstraint()
	if constraint.Name != turnConversationFK || constraint.OnDelete != "CASCADE" {
		t.Fatalf("constraint = %s on delete %s", constraint.Name, constraint.OnDelete)
	}
	if constraint.Schema.Table != turnTable {
		t.Fatalf("constraint table = %s, want %s", constraint.Schema.Table, turnTable)
	}
	if constraint.ReferenceSchema.Table != conversationTable {
		t.Fatalf("constraint reference table = %s, want %s", constraint.ReferenceSchema.Table, conversationTable)
	}
	if want := []string{"user_id", "conversation_id"}; !reflect.DeepEqual(fieldNames(constraint.ForeignKeys), want) {
		t.Fatalf("constraint foreign columns = %v, want %v", fieldNames(constraint.ForeignKeys), want)
	}

	var sequenceIndex *schema.Index
	for _, index := range turnSchema.ParseIndexes() {
		if index.Name == turnSequenceIdx {
			sequenceIndex = index
			break
		}
	}
	if sequenceIndex == nil || sequenceIndex.Class != "UNIQUE" {
		t.Fatalf("unique index %s is missing", turnSequenceIdx)
	}
	columns := make([]string, 0, len(sequenceIndex.Fields))
	for _, field := range sequenceIndex.Fields {
		columns = append(columns, field.Field.DBName)
	}
	if want := []string{"user_id", "conversation_id", "sequence"}; !reflect.DeepEqual(columns, want) {
		t.Fatalf("sequence index columns = %v, want %v", columns, want)
	}
}

func TestJSONDocumentScannerValuer(t *testing.T) {
	document := JSONDocument(`{"budget_cents":300000}`)
	value, err := document.Value()
	if err != nil || value != driver.Value(`{"budget_cents":300000}`) {
		t.Fatalf("Value() = %v, %v", value, err)
	}
	var scanned JSONDocument
	if err := scanned.Scan([]byte(`{"max_items":3}`)); err != nil || string(scanned) != `{"max_items":3}` {
		t.Fatalf("Scan() = %q, %v", scanned, err)
	}
	if _, err := (JSONDocument(`not-json`)).Value(); err == nil {
		t.Fatal("invalid JSON should fail Value()")
	}
}

func TestJSONDocumentUsesPostgresCast(t *testing.T) {
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 user=test dbname=test", PreferSimpleProtocol: true}), &gorm.Config{
		DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatalf("open dry-run postgres: %v", err)
	}
	statement := db.Create(&AgentConversation{UserId: "u1", ConversationId: "c1", Title: "title", State: JSONDocument(`{}`)})
	if statement.Error != nil {
		t.Fatalf("create dry-run statement: %v", statement.Error)
	}
	if !strings.Contains(statement.Statement.SQL.String(), "AS JSONB)") {
		t.Fatalf("JSONB cast missing from SQL: %s", statement.Statement.SQL.String())
	}
}

func fieldNames(fields []*schema.Field) []string {
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		names = append(names, field.DBName)
	}
	return names
}
