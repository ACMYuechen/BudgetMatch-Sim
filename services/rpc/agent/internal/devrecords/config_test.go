package devrecords

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const testDSN = "host=127.0.0.1 port=15432 user=synthetic password='synthetic-secret' dbname=dev_records sslmode=disable TimeZone=Asia/Shanghai"

func validOptions() Options {
	return Options{EnvFile: "unused", DSNKey: "DEMO_DATABASE", ExpectedDB: "dev_records",
		AllowLocal: true, WriteDemo: true, UserID: "demo-user", RunID: "desktop-001"}
}

func TestOptionsRequireExplicitTargetAndWriteSelection(t *testing.T) {
	for name, mutate := range map[string]func(*Options){
		"no grant":            func(o *Options) { o.AllowLocal = false },
		"no source":           func(o *Options) { o.EnvFile = "" },
		"two sources":         func(o *Options) { o.ConfigFile = "another" },
		"missing expected db": func(o *Options) { o.ExpectedDB = "" },
		"unsafe env key":      func(o *Options) { o.DSNKey = "DSN=value" },
		"missing user":        func(o *Options) { o.UserID = "" },
		"unsafe user":         func(o *Options) { o.UserID = "someone:else" },
		"missing run id":      func(o *Options) { o.RunID = "" },
		"unsafe run id":       func(o *Options) { o.RunID = "../existing" },
		"long run id":         func(o *Options) { o.RunID = strings.Repeat("a", 49) },
		"ambiguous read mode": func(o *Options) { o.WriteDemo = false },
	} {
		t.Run(name, func(t *testing.T) { o := validOptions(); mutate(&o); require.Error(t, o.Validate()) })
	}
	o := validOptions()
	require.NoError(t, o.Validate())
	o.WriteDemo, o.RunID, o.UserID = false, "", ""
	require.NoError(t, o.Validate())
}

func TestDSNRejectsImplicitRemoteAndOverrideTargetsWithoutLeakingSecrets(t *testing.T) {
	for _, dsn := range []string{
		"", "postgresql://synthetic:synthetic-secret@127.0.0.1:15432/dev_records",
		strings.Replace(testDSN, "127.0.0.1", "localhost", 1),
		strings.Replace(testDSN, "127.0.0.1", "192.0.2.1", 1),
		strings.Replace(testDSN, "127.0.0.1", "/tmp", 1),
		strings.Replace(testDSN, "15432", "015432", 1),
		strings.Replace(testDSN, "15432", "99999", 1),
		strings.Replace(testDSN, "host=127.0.0.1 ", "", 1),
		strings.Replace(testDSN, "password='synthetic-secret'", "password=''", 1),
		strings.Replace(testDSN, "sslmode=disable", "sslmode=prefer", 1),
		testDSN + " host=192.0.2.1", testDSN + " port=5432", testDSN + " service=external",
		testDSN + " passfile=/private", testDSN + " options='-c search_path=other'", testDSN + " hostaddr=192.0.2.1",
		testDSN + "\n", testDSN + "\x00", testDSN + " application_name=$(danger)",
		strings.Replace(testDSN, "synthetic-secret'", "synthetic-secret", 1),
		strings.Replace(testDSN, "synthetic-secret'", "synthetic-secret'x", 1),
	} {
		_, err := parseConnection(dsn, "dev_records")
		require.Error(t, err)
		require.NotContains(t, err.Error(), "synthetic-secret")
	}
	_, err := parseConnection(testDSN, "different_database")
	require.ErrorContains(t, err, "does not match")
}

func TestDSNQuotingAndFreshConnectionParameters(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "::1"} {
		dsn := strings.Replace(testDSN, "127.0.0.1", host, 1)
		dsn = strings.Replace(dsn, "synthetic-secret", `space \' quote \\ slash`, 1)
		c, err := parseConnection(dsn, "dev_records")
		require.NoError(t, err)
		for _, readOnly := range []bool{true, false} {
			u, err := url.Parse(c.dsn(readOnly))
			require.NoError(t, err)
			password, _ := u.User.Password()
			require.Equal(t, "space ' quote \\ slash", password)
			require.Equal(t, "public", u.Query().Get("search_path"))
			require.Equal(t, "UTC", u.Query().Get("TimeZone"))
			want := "off"
			if readOnly {
				want = "on"
			}
			require.Equal(t, want, u.Query().Get("default_transaction_read_only"))
			require.Equal(t, "3", u.Query().Get("connect_timeout"))
		}
		data, err := json.Marshal(c.target)
		require.NoError(t, err)
		require.NotContains(t, string(data), "synthetic")
	}
}

func TestSelectedDotenvKeyOnlyAndNoInterpolation(t *testing.T) {
	for _, data := range []string{
		"UNRELATED_SECRET=\"not even valid quoting\nDEMO_DATABASE=\"" + testDSN + "\"\n",
		"export DEMO_DATABASE=\"" + testDSN + "\"\n",
		"DEMO_DATABASE=" + testDSN + "\n",
	} {
		value, err := dotenvValue([]byte(data), "DEMO_DATABASE")
		require.NoError(t, err)
		require.Equal(t, testDSN, value)
	}
	for _, data := range []string{"UNRELATED=x", "DEMO_DATABASE=", "DEMO_DATABASE=x\nDEMO_DATABASE=y", "DEMO_DATABASE=\"bad quoting"} {
		_, err := dotenvValue([]byte(data), "DEMO_DATABASE")
		require.Error(t, err)
	}
	_, err := parseConnection("host=${DB_HOST} password='synthetic-secret'", "dev_records")
	require.Error(t, err)
}

func TestSourcesRejectPGOverridesSymlinksAndUnexpectedConfig(t *testing.T) {
	o := validOptions()
	o.EnvFile = filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(o.EnvFile, []byte("DEMO_DATABASE=\""+testDSN+"\"\n"), 0600))
	c, err := loadConnection(o, nil)
	require.NoError(t, err)
	require.Equal(t, "dotenv:DEMO_DATABASE", c.target.Source)
	for _, entry := range []string{"PGHOST=other", "PGPASSWORD=secret", "PGSERVICEFILE=/private"} {
		_, err = loadConnection(o, []string{entry})
		require.ErrorContains(t, err, "PG*")
	}
	link := filepath.Join(t.TempDir(), "linked")
	require.NoError(t, os.Symlink(o.EnvFile, link))
	o.EnvFile = link
	_, err = loadConnection(o, nil)
	require.Error(t, err)
	o.EnvFile, o.ConfigFile = "", filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(o.ConfigFile, []byte("Database:\n  DSN: \""+testDSN+"\"\nModel:\n  APIKey: unrelated\n"), 0600))
	c, err = loadConnection(o, nil)
	require.NoError(t, err)
	require.Equal(t, "config:Database.DSN", c.target.Source)
	require.NoError(t, os.WriteFile(o.ConfigFile, []byte("Database:\n  DSN: ${UNRESOLVED}\n"), 0600))
	_, err = loadConnection(o, nil)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "UNRESOLVED")
}

func TestRunDoesNotReadFilesOrConnectWithoutExplicitGrant(t *testing.T) {
	o := validOptions()
	o.AllowLocal = false
	report, err := Run(context.Background(), o, nil)
	require.ErrorContains(t, err, "allow-local")
	require.False(t, report.DatabaseConnected)
	require.Equal(t, "blocked", report.Status)
	require.Nil(t, report.Demo)
}
