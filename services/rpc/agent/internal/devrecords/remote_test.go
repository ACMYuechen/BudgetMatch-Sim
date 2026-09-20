package devrecords

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const remoteTestDSN = "host=192.0.2.10 port=5432 user=synthetic password='synthetic-secret' dbname=dev_records sslmode=verify-full sslrootcert=system"

func remoteOptions() Options {
	return Options{ConfigFile: "unused", AllowRemote: true, ExpectedAddress: "192.0.2.10:5432", ExpectedDB: "dev_records"}
}

func TestRemoteOptionsRequireSeparateSourceAndPinnedAddress(t *testing.T) {
	for name, change := range map[string]func(*Options){
		"no grant":    func(o *Options) { o.AllowRemote = false },
		"both grants": func(o *Options) { o.AllowLocal = true },
		"dotenv only": func(o *Options) {
			o.ConfigFile, o.EnvFile, o.DSNKey = "", "private.env", "BUDGETMATCH_TEST_POSTGRES_DSN"
		},
		"both sources":   func(o *Options) { o.EnvFile = "private.env" },
		"missing source": func(o *Options) { o.ConfigFile = "" },
		"missing target": func(o *Options) { o.ExpectedAddress = "" },
		"local grant":    func(o *Options) { o.AllowRemote, o.AllowLocal = false, true },
		"missing db":     func(o *Options) { o.ExpectedDB = "" },
		"write identity": func(o *Options) { o.WriteDemo = true },
		"read identity":  func(o *Options) { o.VerifyDemo = true },
	} {
		t.Run(name, func(t *testing.T) {
			o := remoteOptions()
			change(&o)
			require.Error(t, o.Validate())
		})
	}
	for _, mode := range []string{"preflight", "write", "verify"} {
		o := remoteOptions()
		if mode != "preflight" {
			o.UserID, o.RunID = "existing-user", "remote-001"
			o.WriteDemo, o.VerifyDemo = mode == "write", mode == "verify"
		}
		require.NoError(t, o.Validate())
	}
}

func TestRemoteAddressRejectsAmbiguousLocalAndNonUnicastTargets(t *testing.T) {
	for _, address := range []string{
		"", "db.example:5432", "192.0.2.10", "192.0.2.10:05432", "192.0.2.10:0", "192.0.2.10:1023",
		"192.0.2.10:65536", "192.0.2.10:5432,192.0.2.11:5432", "127.0.0.1:5432", "[::1]:5432",
		"0.0.0.0:5432", "[::]:5432", "224.0.0.1:5432", "169.254.1.1:5432", "255.255.255.255:5432",
		"[::ffff:192.0.2.10]:5432", "[fe80::1%eth0]:5432", "[2001:db8:0:0::1]:5432",
	} {
		_, err := remoteAddress(address)
		require.Error(t, err, address)
	}
	for _, address := range []string{"192.0.2.10:5432", "10.10.10.10:15432", "[2001:db8::1]:5432"} {
		_, err := remoteAddress(address)
		require.NoError(t, err)
	}
}

func TestRemoteDSNRequiresExactTargetAndVerifiedTLSWithoutOverrides(t *testing.T) {
	for _, dsn := range []string{
		strings.Replace(remoteTestDSN, "192.0.2.10", "192.0.2.11", 1),
		strings.Replace(remoteTestDSN, "192.0.2.10", "db.example", 1),
		strings.Replace(remoteTestDSN, "5432", "15432", 1),
		strings.Replace(remoteTestDSN, "dev_records", "another_database", 1),
		strings.Replace(remoteTestDSN, "verify-full", "disable", 1),
		strings.Replace(remoteTestDSN, "verify-full", "prefer", 1),
		strings.Replace(remoteTestDSN, "verify-full", "require", 1),
		strings.Replace(remoteTestDSN, "verify-full", "verify-ca", 1),
		strings.Replace(remoteTestDSN, " sslrootcert=system", "", 1),
		strings.Replace(remoteTestDSN, "sslrootcert=system", "sslrootcert=./ca.pem", 1),
		strings.Replace(remoteTestDSN, "password='synthetic-secret'", "password=''", 1),
		remoteTestDSN + " host=192.0.2.11", remoteTestDSN + " sslrootcert=/another.pem",
		remoteTestDSN + " hostaddr=192.0.2.11", remoteTestDSN + " service=other", remoteTestDSN + " options='-c search_path=other'",
		remoteTestDSN + " sslcert=/client.pem", remoteTestDSN + " passfile=/secret", remoteTestDSN + " sslnegotiation=direct",
	} {
		_, err := parseSelectedConnection(dsn, remoteOptions())
		require.Error(t, err)
		require.NotContains(t, err.Error(), "synthetic-secret")
	}
	_, err := parseConnection(remoteTestDSN, "dev_records")
	require.Error(t, err, "local parser must never implicitly accept remote access")
}

func TestRemoteFreshParametersKeepTLSAndDoNotInheritCredentialFiles(t *testing.T) {
	for _, host := range []struct{ ip, address string }{{"192.0.2.10", "192.0.2.10:5432"}, {"2001:db8::1", "[2001:db8::1]:5432"}} {
		o := remoteOptions()
		o.ExpectedAddress = host.address
		c, err := parseSelectedConnection(strings.Replace(remoteTestDSN, "192.0.2.10", host.ip, 1), o)
		require.NoError(t, err)
		require.Equal(t, "verify-full", c.target.TLSMode)
		for _, readOnly := range []bool{true, false} {
			u, err := url.Parse(c.dsn(readOnly))
			require.NoError(t, err)
			require.Equal(t, host.address, u.Host)
			require.Equal(t, "verify-full", u.Query().Get("sslmode"))
			require.Equal(t, "system", u.Query().Get("sslrootcert"))
			for _, key := range []string{"sslcert", "sslkey", "passfile"} {
				require.True(t, u.Query().Has(key))
				require.Empty(t, u.Query().Get(key))
			}
			want := "off"
			if readOnly {
				want = "on"
			}
			require.Equal(t, want, u.Query().Get("default_transaction_read_only"))
			require.Equal(t, "public", u.Query().Get("search_path"))
			require.Equal(t, "5000", u.Query().Get("statement_timeout"))
		}
		data, err := json.Marshal(c.target)
		require.NoError(t, err)
		require.NotContains(t, string(data), "synthetic")
		require.NotContains(t, string(data), "rootcert")
	}
	c, err := parseConnection(testDSN, "dev_records")
	require.NoError(t, err)
	u, err := url.Parse(c.dsn(true))
	require.NoError(t, err)
	require.Equal(t, "disable", u.Query().Get("sslmode"))
	require.True(t, u.Query().Has("sslrootcert"))
	require.Empty(t, u.Query().Get("sslrootcert"))
}

func TestRemoteConfigMustBePrivateAndCAExplicit(t *testing.T) {
	o := remoteOptions()
	o.ConfigFile = filepath.Join(t.TempDir(), "database.yaml")
	write := func(dsn string) {
		t.Helper()
		require.NoError(t, os.WriteFile(o.ConfigFile, []byte("Database:\n  DSN: \""+dsn+"\"\n"), 0600))
	}
	write(remoteTestDSN)
	c, err := loadConnection(o, nil)
	require.NoError(t, err)
	require.Equal(t, "config:Database.DSN", c.target.Source)
	for _, mode := range []os.FileMode{0644, 0640, 0604} {
		require.NoError(t, os.Chmod(o.ConfigFile, mode))
		_, err = loadConnection(o, nil)
		require.ErrorContains(t, err, "permissions")
	}
	require.NoError(t, os.Chmod(o.ConfigFile, 0600))
	for _, entry := range []string{"PGHOST=another", "PGSSLMODE=disable", "PGSSLROOTCERT=/another", "PGSERVICEFILE=/private"} {
		_, err = loadConnection(o, []string{entry})
		require.ErrorContains(t, err, "PG*")
	}
	linked := o
	linked.ConfigFile = filepath.Join(t.TempDir(), "linked.yaml")
	require.NoError(t, os.Symlink(o.ConfigFile, linked.ConfigFile))
	_, err = loadConnection(linked, nil)
	require.Error(t, err)
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	write(strings.Replace(remoteTestDSN, "sslrootcert=system", "sslrootcert="+caFile, 1))
	_, err = loadConnection(o, nil)
	require.ErrorContains(t, err, "PEM CA")
	require.NoError(t, os.WriteFile(caFile, []byte("not a certificate"), 0600))
	_, err = loadConnection(o, nil)
	require.ErrorContains(t, err, "PEM CA")
	require.NotContains(t, err.Error(), caFile)
	require.NotContains(t, err.Error(), "synthetic-secret")
	write(remoteTestDSN)
	require.NoError(t, os.Chmod(o.ConfigFile, 0400))
	_, err = loadConnection(o, nil)
	require.NoError(t, err)
}

func TestCertificateDiagnosticNeverExposesCertificateOrPeerDetails(t *testing.T) {
	err := databaseFailure("connect", &tls.CertificateVerificationError{Err: errors.New(privateErrorMarker)})
	require.Equal(t, &Diagnostic{Stage: "connect", Code: "tls_verification_failed"}, diagnosticOf(err))
	require.NotContains(t, err.Error(), privateErrorMarker)
	require.Nil(t, errors.Unwrap(err))
}
