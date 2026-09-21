package product_index

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestCandidateQueryChecksBothStatusesWithoutCacheOrExtraData(t *testing.T) {
	db, err := gorm.Open(postgres.Open("host=unused.invalid user=test dbname=test sslmode=disable"),
		&gorm.Config{DisableAutomaticPing: true, DryRun: true})
	require.NoError(t, err)
	var rows []CandidateFacts
	attack := "x' OR 1=1 --"
	query := candidateQuery(db, []string{"a", attack}).Find(&rows)
	require.NoError(t, query.Error)
	sql := query.Statement.SQL.String()
	for _, expected := range []string{"JOIN products AS p ON p.id = s.product_id", "p.status = $1 AND s.status = $2",
		"p.deleted_at IS NULL AND s.deleted_at IS NULL", "s.id IN ($3,$4)", "LIMIT $5"} {
		require.Contains(t, sql, expected)
	}
	for _, forbidden := range []string{attack, "user_id", "agent_comment", "content", "specs", "orders", "SELECT *", "OFFSET", "UPDATE", "DELETE"} {
		require.NotContains(t, sql, forbidden)
	}
	require.Equal(t, []interface{}{1, 1, "a", attack, 32}, query.Statement.Vars)
	_, err = NewModel(nil).FindActiveCandidates(context.Background(), []string{"a", "a"})
	require.Error(t, err, "bounds must reject before DB access")
}
