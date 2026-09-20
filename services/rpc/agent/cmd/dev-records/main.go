// dev-records inspects an explicitly selected local database and optionally
// retains two marked demo turns for an existing user. No external model calls.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/devrecords"

	"github.com/zeromicro/go-zero/core/logx"
)

type executor func(context.Context, devrecords.Options, []string) (devrecords.Report, error)

func run(args []string, out, stderr io.Writer, execute executor) int {
	fs := flag.NewFlagSet("dev-records", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o devrecords.Options
	var reportPath string
	fs.StringVar(&o.EnvFile, "env", "", "explicit dotenv file (no shell expansion)")
	fs.StringVar(&o.DSNKey, "dsn-key", "BUDGETMATCH_TEST_POSTGRES_DSN", "only selected key is read; never used as an implicit default connection")
	fs.StringVar(&o.ConfigFile, "config", "", "alternative YAML source containing literal Database.DSN")
	fs.StringVar(&o.ExpectedDB, "expect-db", "", "independently confirmed database name; must match source")
	fs.BoolVar(&o.AllowLocal, "allow-local-dev-db", false, "confirm access to selected local development database")
	fs.BoolVar(&o.WriteDemo, "write-demo", false, "retain two marked local-rule/mock-product turns (default: read-only preflight)")
	fs.BoolVar(&o.VerifyDemo, "verify-demo", false, "read-only check of an existing demo; never creates, repairs or replays writes")
	fs.StringVar(&o.UserID, "user-id", "", "existing enabled account that will own demo history; not a username")
	fs.StringVar(&o.RunID, "run-id", "", "stable demo namespace; same ID replays, different IDs create new histories")
	fs.StringVar(&reportPath, "report", "", "optional new private JSON report; refuses to overwrite existing files")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected positional arguments")
		return 2
	}
	if err := o.Validate(); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	var reportFile *os.File
	if reportPath != "" {
		var err error
		reportFile, err = os.OpenFile(reportPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			fmt.Fprintln(stderr, "report must be a new writable file; no database access attempted")
			return 2
		}
		defer reportFile.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	report, executeErr := execute(ctx, o, os.Environ())
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, "cannot encode execution report")
		return 1
	}
	data = append(data, '\n')
	if reportFile != nil {
		if _, err = reportFile.Write(data); err == nil {
			err = reportFile.Sync()
		}
		if err != nil {
			fmt.Fprintln(stderr, "cannot persist report; check database outcome before retrying")
			return 1
		}
	}
	if _, err = out.Write(data); err != nil || executeErr != nil {
		return 1
	}
	return 0
}

func main() {
	logx.SetLevel(logx.ErrorLevel)
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, devrecords.Run))
}
