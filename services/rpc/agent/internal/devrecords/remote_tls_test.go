package devrecords

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// All keys are ephemeral synthetic test keys. Only the public CA is written to
// the test's private temporary directory; no real host or credential is used.
func syntheticTLS(t *testing.T, ip string, expired bool) (tls.Certificate, string) {
	t.Helper()
	caPublic, caPrivate, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic-test-ca"},
		NotBefore: now.Add(-24 * time.Hour), NotAfter: now.Add(24 * time.Hour), IsCA: true,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, caPublic, caPrivate)
	require.NoError(t, err)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "synthetic-test-server"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IPAddresses: []net.IP{net.ParseIP(ip)},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if expired {
		leaf.NotBefore, leaf.NotAfter = now.Add(-2*time.Hour), now.Add(-time.Hour)
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, public, caPrivate)
	require.NoError(t, err)
	caPath := filepath.Join(t.TempDir(), "synthetic-ca.pem")
	require.NoError(t, os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0600))
	return tls.Certificate{Certificate: [][]byte{leafDER, caDER}, PrivateKey: private}, caPath
}

// Exercises the locked GORM/pgx driver over real loopback TCP/TLS, not just a
// URL assertion. The peer is a bounded protocol double, NOT a PostgreSQL server.
// It stops at authentication and never accepts a SQL query or performs a write.
func TestRemoteTLSDriverRejectsDowngradeAndUnverifiedPeers(t *testing.T) {
	for _, scenario := range []string{"verified read", "verified write connection", "TLS refused", "wrong IP", "unknown CA", "expired"} {
		t.Run(scenario, func(t *testing.T) {
			ip := "127.0.0.1"
			if scenario == "wrong IP" {
				ip = "192.0.2.11"
			}
			certificate, root := syntheticTLS(t, ip, scenario == "expired")
			if scenario == "unknown CA" {
				_, root = syntheticTLS(t, ip, false)
			}
			listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
			require.NoError(t, err)
			t.Cleanup(func() { _ = listener.Close() })
			readOnly := scenario != "verified write connection"
			wantStartup := scenario == "verified read" || scenario == "verified write connection"
			finished := make(chan error, 1)
			go func() {
				err := serveTLSAuthenticationDouble(listener, certificate, scenario == "TLS refused", wantStartup, readOnly)
				if err == nil {
					// The same peer remains available after refusal: any plaintext
					// or alternate-connection fallback would be observable here.
					_ = listener.SetDeadline(time.Now().Add(50 * time.Millisecond))
					conn, acceptErr := listener.Accept()
					if acceptErr == nil {
						_ = conn.Close()
						err = errors.New("unexpected reconnect after authentication or TLS failure")
					} else if netErr, ok := acceptErr.(net.Error); !ok || !netErr.Timeout() {
						err = acceptErr
					}
				}
				finished <- err
			}()
			c, err := parseSelectedConnection(remoteTestDSN, remoteOptions())
			require.NoError(t, err)
			// Route only this internal transport test to its owned listener.
			// Public option/source validation still refuses remote loopback.
			c.target.Address, c.rootCA = listener.Addr().String(), root
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			db, pool, err := openDatabase(ctx, c, readOnly)
			require.Error(t, err)
			require.Nil(t, db)
			require.Nil(t, pool)
			code := "tls_verification_failed"
			if wantStartup {
				code = "authentication_failed"
			} else if scenario == "TLS refused" {
				code = "database_error" // driver exposes no typed TLS-refusal cause
			}
			require.Equal(t, &Diagnostic{Stage: "connect", Code: code}, diagnosticOf(err))
			require.NotContains(t, err.Error(), "synthetic-secret")
			require.NotContains(t, err.Error(), privateErrorMarker)
			require.NotContains(t, err.Error(), root)
			select {
			case peerErr := <-finished:
				require.NoError(t, peerErr)
			case <-ctx.Done():
				t.Fatal("TLS double did not stop within deadline")
			}
		})
	}
}

func serveTLSAuthenticationDouble(listener *net.TCPListener, certificate tls.Certificate, refuse, wantStartup, readOnly bool) error {
	_ = listener.SetDeadline(time.Now().Add(3 * time.Second))
	conn, err := listener.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	var sslRequest [8]byte
	if _, err := io.ReadFull(conn, sslRequest[:]); err != nil {
		return err
	}
	if binary.BigEndian.Uint32(sslRequest[:4]) != 8 || binary.BigEndian.Uint32(sslRequest[4:]) != 80877103 {
		return errors.New("client sent plaintext startup instead of SSLRequest")
	}
	if refuse {
		if _, err := conn.Write([]byte{'N'}); err != nil {
			return err
		}
		var next [1]byte
		if n, err := conn.Read(next[:]); n != 0 || !errors.Is(err, io.EOF) {
			return errors.New("client failed to close after TLS refusal")
		}
		return nil
	}
	if _, err := conn.Write([]byte{'S'}); err != nil {
		return err
	}
	secure := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	if err := secure.Handshake(); err != nil {
		if !wantStartup {
			return nil
		}
		return err
	}
	if !wantStartup {
		return errors.New("client accepted an unverified certificate")
	}
	var header [4]byte
	if _, err := io.ReadFull(secure, header[:]); err != nil {
		return err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size < 8 || size > 16384 {
		return errors.New("unexpected startup length")
	}
	startup := make([]byte, size-4)
	if _, err := io.ReadFull(secure, startup); err != nil {
		return err
	}
	if binary.BigEndian.Uint32(startup[:4]) != 196608 {
		return errors.New("expected PostgreSQL v3 startup")
	}
	fields := strings.Split(string(startup[4:]), "\x00")
	params := map[string]string{}
	for i := 0; i+1 < len(fields); i += 2 {
		params[fields[i]] = fields[i+1]
	}
	wantMode := "on"
	if !readOnly {
		wantMode = "off"
	}
	if params["database"] != "dev_records" || params["user"] != "synthetic" ||
		params["default_transaction_read_only"] != wantMode || params["search_path"] != "public" ||
		params["statement_timeout"] != "5000" || params["lock_timeout"] != "1000" ||
		params["idle_in_transaction_session_timeout"] != "10000" {
		return errors.New("TLS startup did not enforce expected target, mode and query bounds")
	}
	body := []byte("SFATAL\x00C28P01\x00M" + privateErrorMarker + "\x00\x00")
	frame := make([]byte, 5+len(body))
	frame[0] = 'E'
	binary.BigEndian.PutUint32(frame[1:5], uint32(4+len(body)))
	copy(frame[5:], body)
	_, err = secure.Write(frame)
	return err
}

func TestRemoteConfigLoadsOnlyExplicitValidCAFile(t *testing.T) {
	_, root := syntheticTLS(t, "127.0.0.1", false)
	o := remoteOptions()
	o.ConfigFile = filepath.Join(t.TempDir(), "remote.yaml")
	dsn := strings.Replace(remoteTestDSN, "sslrootcert=system", "sslrootcert="+root, 1)
	require.NoError(t, os.WriteFile(o.ConfigFile, []byte("Database:\n  DSN: \""+dsn+"\"\n"), 0600))
	c, err := loadConnection(o, nil)
	require.NoError(t, err)
	require.Equal(t, root, c.rootCA)
	link := filepath.Join(t.TempDir(), "ca-link.pem")
	require.NoError(t, os.Symlink(root, link))
	dsn = strings.Replace(dsn, root, link, 1)
	require.NoError(t, os.WriteFile(o.ConfigFile, []byte("Database:\n  DSN: \""+dsn+"\"\n"), 0600))
	_, err = loadConnection(o, nil)
	require.ErrorContains(t, err, "PEM CA")
	require.NotContains(t, err.Error(), link)
}
