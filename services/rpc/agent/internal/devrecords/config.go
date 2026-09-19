// Package devrecords provides an explicitly selected, non-destructive local
// development database workflow. It is never loaded by production services.
package devrecords

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/zeromicro/go-zero/core/conf"
)

// Options selects ONE source. No shell environment DSN or default database is
// used; ExpectedDB is an independent confirmation, not a database override.
type Options struct {
	EnvFile, DSNKey, ConfigFile, ExpectedDB string
	AllowLocal, WriteDemo                   bool
	UserID, RunID                           string
}

var (
	identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	dbName     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,63}$`)
	envKey     = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)
	runID      = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,47}$`)
)

func (o Options) Validate() error {
	if !o.AllowLocal {
		return errors.New("explicit -allow-local-dev-db is required")
	}
	if (o.EnvFile == "") == (o.ConfigFile == "") {
		return errors.New("select exactly one of -env and -config; there is no implicit source")
	}
	if !dbName.MatchString(o.ExpectedDB) {
		return errors.New("-expect-db must name the independently confirmed development database")
	}
	if o.EnvFile != "" && !envKey.MatchString(o.DSNKey) {
		return errors.New("-dsn-key must select one explicit dotenv key")
	}
	if o.UserID != "" && !identifier.MatchString(o.UserID) {
		return errors.New("-user-id must contain 1..128 letters, digits, underscores or hyphens")
	}
	if o.WriteDemo && (o.UserID == "" || !runID.MatchString(o.RunID)) {
		return errors.New("-write-demo requires an existing -user-id and a 1..48 character lowercase -run-id")
	}
	if !o.WriteDemo && o.RunID != "" {
		return errors.New("-run-id is only used with -write-demo")
	}
	return nil
}

// Target contains only safe metadata; credentials never enter a report.
type Target struct {
	Source   string `json:"source"`
	Address  string `json:"address"`
	Database string `json:"database"`
}

type connection struct {
	target Target
	user   string
	secret string
}

func loadConnection(o Options, environ []string) (connection, error) {
	if err := o.Validate(); err != nil {
		return connection{}, err
	}
	for _, entry := range environ {
		if strings.HasPrefix(entry, "PG") {
			return connection{}, errors.New("remove PG* environment overrides before running this command")
		}
	}
	path := o.EnvFile
	if path == "" {
		path = o.ConfigFile
	}
	data, err := readSource(path)
	if err != nil {
		return connection{}, err
	}
	var dsn, source string
	if o.EnvFile != "" {
		dsn, err = dotenvValue(data, o.DSNKey)
		source = "dotenv:" + o.DSNKey
	} else {
		var selected struct {
			Database struct {
				DSN string
			}
		}
		if conf.LoadFromYamlBytes(data, &selected) != nil || selected.Database.DSN == "" {
			return connection{}, errors.New("config must contain a literal Database.DSN; details suppressed")
		}
		dsn, source = selected.Database.DSN, "config:Database.DSN"
	}
	if err != nil {
		return connection{}, err
	}
	c, err := parseConnection(dsn, o.ExpectedDB)
	c.target.Source = source
	return c, err
}

func readSource(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 256*1024 {
		return nil, errors.New("source must be a regular non-symlink file no larger than 256 KiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot read selected database source")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("database source changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, 256*1024+1))
	if err != nil || len(data) > 256*1024 {
		return nil, errors.New("cannot read bounded database source")
	}
	return data, nil
}

// Read just the selected key. Do not source .env, interpolate variables, or
// validate/expose unrelated credentials. Duplicate selected keys fail closed.
func dotenvValue(data []byte, key string) (string, error) {
	scan := bufio.NewScanner(strings.NewReader(string(data)))
	scan.Buffer(make([]byte, 4096), 256*1024)
	var value string
	count := 0
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		line = strings.TrimPrefix(line, "export ")
		name, raw, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(name) != key {
			continue
		}
		count++
		value = strings.TrimSpace(raw)
		if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
			value = value[1 : len(value)-1]
		} else if strings.HasPrefix(value, `"`) {
			var err error
			value, err = strconv.Unquote(value)
			if err != nil {
				return "", errors.New("invalid quoting in selected dotenv value; details suppressed")
			}
		}
	}
	if scan.Err() != nil || count != 1 || value == "" {
		return "", errors.New("selected dotenv key must occur exactly once with a nonempty value")
	}
	return value, nil
}

// Deliberately support only the bounded libpq key=value format used by this
// repository. No service files, sockets, DNS, multiple hosts, URL overrides,
// runtime options, password files or variable/shell expansion are accepted.
func parseConnection(dsn, expected string) (connection, error) {
	bad := errors.New("invalid local DSN: require explicit loopback host/port/user/password/dbname and sslmode=disable; details suppressed")
	if len(dsn) > 16*1024 || strings.ContainsAny(dsn, "\r\n\x00") || strings.Contains(dsn, "${") || strings.Contains(dsn, "$(") || strings.Contains(dsn, "`") {
		return connection{}, bad
	}
	values := map[string]string{}
	for rest := strings.TrimSpace(dsn); rest != ""; rest = strings.TrimSpace(rest) {
		key, next, ok := strings.Cut(rest, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		if !ok || key == "" || strings.ContainsAny(key, " \t") {
			return connection{}, bad
		}
		switch key {
		case "host", "port", "user", "password", "dbname", "sslmode", "timezone":
		default:
			return connection{}, bad
		}
		if _, exists := values[key]; exists {
			return connection{}, bad
		}
		rest = strings.TrimLeft(next, " \t")
		quoted := strings.HasPrefix(rest, "'")
		if quoted {
			rest = rest[1:]
		}
		var value strings.Builder
		closed := !quoted
		for len(rest) > 0 {
			ch := rest[0]
			rest = rest[1:]
			if ch == '\\' {
				if len(rest) == 0 {
					return connection{}, bad
				}
				value.WriteByte(rest[0])
				rest = rest[1:]
			} else if quoted && ch == '\'' {
				closed = true
				if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
					return connection{}, bad
				}
				break
			} else if !quoted && (ch == ' ' || ch == '\t') {
				break
			} else {
				value.WriteByte(ch)
			}
		}
		if !closed {
			return connection{}, bad
		}
		values[key] = value.String()
	}
	ip, err := netip.ParseAddr(values["host"])
	port, portErr := strconv.Atoi(values["port"])
	if err != nil || !ip.IsLoopback() || ip.Zone() != "" || portErr != nil || port < 1024 || port > 65535 ||
		strconv.Itoa(port) != values["port"] || values["sslmode"] != "disable" ||
		values["user"] == "" || values["password"] == "" || !dbName.MatchString(values["dbname"]) {
		return connection{}, bad
	}
	if values["dbname"] != expected {
		return connection{}, errors.New("database source does not match -expect-db; no fallback or override allowed")
	}
	return connection{target: Target{Address: net.JoinHostPort(ip.String(), values["port"]), Database: expected},
		user: values["user"], secret: values["password"]}, nil
}

func (c connection) dsn(readOnly bool) string {
	u := url.URL{Scheme: "postgresql", Host: c.target.Address, Path: "/" + c.target.Database,
		User: url.UserPassword(c.user, c.secret)}
	readMode := "on"
	if !readOnly {
		readMode = "off"
	}
	// Construct fresh driver configuration, not the supplied DSN. Never inherit
	// arbitrary runtime parameters. Timezone is explicitly UTC for both modes.
	u.RawQuery = url.Values{"sslmode": {"disable"}, "connect_timeout": {"3"}, "search_path": {"public"},
		"TimeZone": {"UTC"}, "application_name": {"agent-dev-records"}, "statement_timeout": {"5000"},
		"lock_timeout": {"1000"}, "idle_in_transaction_session_timeout": {"10000"},
		"default_transaction_read_only": {readMode}}.Encode()
	return u.String()
}
