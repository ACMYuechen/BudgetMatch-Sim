// Package storageacceptance contains opt-in, destructive-on-test-data acceptance
// tests. It is not imported by a service and never loads deployment configuration.
package storageacceptance

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/memory"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	configEnv = "AGENT_STORAGE_ACCEPTANCE_CONFIG"
	grantEnv  = "AGENT_STORAGE_ACCEPTANCE_RUN_ID"
	ownerKey  = "budgetmatch:agent:m62:owner"
)

// Addresses are literal loopback IPs on non-default ports. Database and user
// names are derived/fixed, not arbitrary DSNs; passwords belong ONLY to newly
// provisioned disposable instances. No deployment or legacy test env is used.
type targetConfig struct {
	RunID            string `json:"run_id"`
	PostgresAddress  string `json:"postgres_address"`
	PostgresPassword string `json:"postgres_password"`
	RedisAddress     string `json:"redis_address"`
	RedisPassword    string `json:"redis_password"`
}

func decodeTarget(r io.Reader) (targetConfig, error) {
	var cfg targetConfig
	dec := json.NewDecoder(io.LimitReader(r, 16*1024+1))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, errors.New("invalid acceptance config JSON (details suppressed)")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return cfg, errors.New("acceptance config must contain exactly one JSON object")
	}
	return cfg, cfg.validate(true)
}

func (c targetConfig) validate(withPostgres bool) error {
	id, err := hex.DecodeString(c.RunID)
	if err != nil || len(id) != 16 || strings.ToLower(c.RunID) != c.RunID {
		return errors.New("run_id must be 32 lowercase hexadecimal characters")
	}
	if err := checkAddress(c.RedisAddress); err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	if len(c.RedisPassword) < 16 || len(c.RedisPassword) > 256 {
		return errors.New("redis requires a dedicated 16..256 byte test password")
	}
	if withPostgres {
		if err := checkAddress(c.PostgresAddress); err != nil {
			return fmt.Errorf("postgres: %w", err)
		}
		if c.PostgresAddress == c.RedisAddress {
			return errors.New("storage endpoints must differ")
		}
		if len(c.PostgresPassword) < 16 || len(c.PostgresPassword) > 256 {
			return errors.New("postgres requires a dedicated 16..256 byte test password")
		}
	}
	return nil
}

func checkAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return errors.New("expected literal loopback IP:port")
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.IsLoopback() || ip.Zone() != "" {
		return errors.New("only literal loopback IPs are allowed; no DNS or socket paths")
	}
	p, err := strconv.Atoi(port)
	if err != nil || strconv.Itoa(p) != port || p < 1024 || p > 65535 || p == 5432 || p == 6379 {
		return errors.New("use a non-default, unprivileged test port")
	}
	return nil
}

func loadGrantedTarget(path, grant string, environ []string) (targetConfig, error) {
	if path == "" || grant == "" {
		return targetConfig{}, errors.New("both acceptance config and explicit run_id grant are required")
	}
	// pgx accepts libpq environment defaults. Refuse them rather than silently
	// inheriting service, password-file, host or TLS overrides in the parent.
	for _, entry := range environ {
		if strings.HasPrefix(entry, "PG") {
			return targetConfig{}, errors.New("remove PG* environment overrides before storage acceptance")
		}
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16*1024 || info.Mode().Perm()&0077 != 0 {
		return targetConfig{}, errors.New("acceptance config must be a private regular file (0600, at most 16 KiB)")
	}
	f, err := os.Open(path)
	if err != nil {
		return targetConfig{}, errors.New("cannot open acceptance config")
	}
	defer f.Close()
	openedInfo, err := f.Stat()
	if err != nil || !os.SameFile(info, openedInfo) {
		return targetConfig{}, errors.New("acceptance config must be a private regular file (0600, at most 16 KiB)")
	}
	cfg, err := decodeTarget(f)
	if err != nil {
		return targetConfig{}, err
	}
	if grant != cfg.RunID {
		return targetConfig{}, errors.New("explicit run_id grant does not match config")
	}
	return cfg, nil
}

func (c targetConfig) database() string { return "agent_m62_" + c.RunID }

func (c targetConfig) postgresDSN() string {
	u := url.URL{Scheme: "postgresql", Host: c.PostgresAddress, Path: "/" + c.database(),
		User: url.UserPassword("agent_m62", c.PostgresPassword)}
	u.RawQuery = url.Values{"sslmode": {"disable"}, "connect_timeout": {"3"},
		"application_name": {"agent-m62-" + c.RunID}, "search_path": {"public"}}.Encode()
	return u.String()
}

// All opens are read-only until BOTH ownership markers have been verified by
// the parent. Workers verify again and never create schemas or markers.
func (c targetConfig) openPostgres(ctx context.Context) (*gorm.DB, *sql.DB, error) {
	if err := c.validate(true); err != nil {
		return nil, nil, err
	}
	db, err := gorm.Open(postgres.Open(c.postgresDSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true,
	})
	if err != nil {
		return nil, nil, errors.New("open acceptance postgres failed (details suppressed)")
	}
	pool, err := db.DB()
	if err != nil {
		return nil, nil, errors.New("acceptance postgres pool unavailable")
	}
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	var identity struct{ Database, Owner string }
	err = db.WithContext(ctx).Raw(`SELECT current_database() AS database,
		(SELECT run_id FROM public.agent_storage_acceptance_guard) AS owner`).Scan(&identity).Error
	if err != nil || identity.Database != c.database() || identity.Owner != c.RunID {
		_ = pool.Close()
		return nil, nil, errors.New("postgres ownership check failed; no schema changes made")
	}
	return db, pool, nil
}

func (c targetConfig) openRedis(ctx context.Context) (*redis.Client, error) {
	if err := c.validate(false); err != nil {
		return nil, err
	}
	client := redis.NewClient(&redis.Options{Addr: c.RedisAddress, Password: c.RedisPassword,
		DB: 0, PoolSize: 1, MaxRetries: -1, ContextTimeoutEnabled: true,
		DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second,
		PoolTimeout: time.Second})
	owner, err := client.Get(ctx, ownerKey).Result()
	if err != nil || owner != c.RunID {
		_ = client.Close()
		return nil, errors.New("redis ownership check failed; no keys changed")
	}
	return client, nil
}

type openedStore struct {
	store memory.ConversationStore
	pg    *gorm.DB
	pool  *sql.DB
	rdb   *redis.Client
}

func (s *openedStore) close() {
	if s.pool != nil {
		_ = s.pool.Close()
	}
	if s.rdb != nil {
		_ = s.rdb.Close()
	}
}

func openStore(ctx context.Context, cfg targetConfig, mode string) (*openedStore, error) {
	s := &openedStore{}
	if mode != "redis" && mode != "postgres" && mode != "tiered" {
		return nil, errors.New("unknown storage mode")
	}
	var durable *memory.Postgres
	if mode != "redis" {
		var err error
		s.pg, s.pool, err = cfg.openPostgres(ctx)
		if err != nil {
			return nil, err
		}
		durable = memory.NewPostgres(s.pg, memory.Conf{})
		s.store = durable
	}
	if mode != "postgres" {
		var err error
		s.rdb, err = cfg.openRedis(ctx)
		if err != nil {
			s.close()
			return nil, err
		}
		cache := memory.NewRedis(s.rdb, memory.Conf{TTL: time.Hour})
		s.store = cache
		if mode == "tiered" {
			s.store = memory.NewTiered(durable, cache, memory.Conf{})
		}
	}
	return s, nil
}
