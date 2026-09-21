package testdb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenIsolatesSchemasAndCleansUp(t *testing.T) {
	outer := Open(t)
	var outerSchema, innerSchema string
	require.NoError(t, outer.Raw("SELECT current_schema()").Scan(&outerSchema).Error)
	require.NoError(t, outer.Exec("CREATE TABLE isolation_probe (value text)").Error)
	require.NoError(t, outer.Exec("INSERT INTO isolation_probe VALUES ('outer')").Error)
	t.Run("independent schema", func(t *testing.T) {
		inner := Open(t)
		require.NoError(t, inner.Raw("SELECT current_schema()").Scan(&innerSchema).Error)
		require.NotEqual(t, outerSchema, innerSchema)
		require.NoError(t, inner.Exec("CREATE TABLE isolation_probe (value text)").Error)
		require.NoError(t, inner.Exec("INSERT INTO isolation_probe VALUES ('inner')").Error)
		var value string
		require.NoError(t, inner.Raw("SELECT value FROM isolation_probe").Scan(&value).Error)
		require.Equal(t, "inner", value)
	})
	var value string
	require.NoError(t, outer.Raw("SELECT value FROM isolation_probe").Scan(&value).Error)
	require.Equal(t, "outer", value)
	var remaining int64
	require.NoError(t, outer.Raw("SELECT count(*) FROM pg_namespace WHERE nspname = ?", innerSchema).Scan(&remaining).Error)
	require.Zero(t, remaining)
}
