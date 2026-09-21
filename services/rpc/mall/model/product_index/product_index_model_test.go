package product_index

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPageQueryIsBoundedReadOnlyProjection(t *testing.T) {
	// Dry-run SQL generation only: no connection, migrations, or production data.
	db, err := gorm.Open(postgres.Open("host=unused.invalid user=test dbname=test sslmode=disable"),
		&gorm.Config{DisableAutomaticPing: true, DryRun: true})
	require.NoError(t, err)
	var rows []Entry
	query := pageQuery(db, "sku-001", 101).Find(&rows)
	require.NoError(t, query.Error)
	sql := query.Statement.SQL.String()
	for _, expected := range []string{"SELECT ", "JOIN products AS p ON p.id = s.product_id", "p.status = $1 AND s.status = $2",
		"p.deleted_at IS NULL AND s.deleted_at IS NULL", `s.id COLLATE "C" > $3`, `ORDER BY s.id COLLATE "C" ASC LIMIT $4`} {
		require.Contains(t, sql, expected)
	}
	for _, forbidden := range []string{"user_id", "agent_comment", "orders", "SELECT *", "OFFSET", "UPDATE", "DELETE"} {
		require.NotContains(t, sql, forbidden)
	}
	require.Equal(t, []interface{}{1, 1, "sku-001", 101}, query.Statement.Vars)
	first := pageQuery(db, "", 201).Find(&rows)
	require.NotContains(t, first.Statement.SQL.String(), `COLLATE "C" >`)
	// User-controlled cursor values remain bound parameters.
	attack := "x' OR 1=1 --"
	param := pageQuery(db, attack, 2).Find(&rows)
	require.NotContains(t, param.Statement.SQL.String(), attack)
	require.Contains(t, param.Statement.Vars, attack)
}

func TestPageBoundsRejectBeforeDatabaseAccess(t *testing.T) {
	model := NewModel(nil)
	for _, cursor := range []string{strings.Repeat("a", MaxCursorBytes+1), " x", "x ", "x\x00", string([]byte{0xff})} {
		_, err := model.ListPage(context.Background(), cursor, 1)
		require.Error(t, err)
	}
	for _, limit := range []int{-1, 0, MaxPageSize + 2} {
		_, err := model.ListPage(context.Background(), "", limit)
		require.Error(t, err)
	}
	require.True(t, ValidCursor(""))
	require.True(t, ValidCursor("psku-example"))
}
