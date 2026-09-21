package product_index

import (
	"context"
	"testing"

	"budgetmatch-sim/services/rpc/mall/candidatecontract"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestDemandFactsUseOneJoinedSnapshotWithoutChangingLegacySQL(t *testing.T) {
	db, err := gorm.Open(postgres.Open("host=unused.invalid user=test dbname=test sslmode=disable"), &gorm.Config{DisableAutomaticPing: true, DryRun: true})
	require.NoError(t, err)
	var rows []CandidateFacts
	query := demandCandidateQuery(db, []string{"a", "b"}).Find(&rows)
	require.NoError(t, query.Error)
	sql := query.Statement.SQL.String()
	for _, fragment := range []string{"LEFT JOIN product_demand_categories AS dc ON dc.product_id = p.id", "s.price, s.stock, s.sold", "dc.category_code", "dc.taxonomy_version", "dc.revision", "p.deleted_at IS NULL AND s.deleted_at IS NULL", "LIMIT"} {
		require.Contains(t, sql, fragment)
	}
	for _, forbidden := range []string{"user_id", "agent_comment", "content", "specs", "orders", "SELECT *", "UPDATE", "DELETE"} {
		require.NotContains(t, sql, forbidden)
	}
	require.Equal(t, []interface{}{candidatecontract.DemandTaxonomyVersion, 1, 1, "a", "b", 32}, query.Statement.Vars)
	legacy := candidateQuery(db, []string{"a"}).Find(&rows)
	require.NotContains(t, legacy.Statement.SQL.String(), "product_demand_categories")
	_, err = NewModel(nil).FindActiveDemandCandidates(context.Background(), []string{"a", "a"})
	require.Error(t, err)
}
