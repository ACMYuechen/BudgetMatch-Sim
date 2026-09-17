// Command eval 在固定合成快照上运行规则基线；不读取 .env 或服务配置，不构建外部客户端。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"budgetmatch-sim/services/rpc/agent/internal/eval"
	"github.com/zeromicro/go-zero/core/logx"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// 保证 stdout 是单份 JSON/Markdown，不混入业务日志；错误类别已在报告中。
	logx.SetWriter(logx.NewWriter(io.Discard))
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("agent-eval", flag.ContinueOnError)
	flags.SetOutput(stderr)
	snapshot := flags.String("snapshot", "services/rpc/agent/testdata/eval/products.v1.json", "fixed synthetic product snapshot")
	cases := flags.String("cases", "services/rpc/agent/testdata/eval/cases.v1.jsonl", "fixed JSONL case set")
	split := flags.String("split", "all", "all, dev or holdout")
	topK := flags.Int("top-k", 10, "filtered retrieval limit, 1..256")
	revision := flags.String("revision", "unknown", "caller-supplied code revision; mark uncommitted changes explicitly")
	format := flags.String("format", "json", "stdout report format: json or markdown")
	out := flags.String("out", "", "optional NEW directory for report.json and report.md; never overwrites")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || (*format != "json" && *format != "markdown") {
		fmt.Fprintln(stderr, "invalid arguments or output format")
		return 2
	}
	sf, err := os.Open(*snapshot)
	if err != nil {
		fmt.Fprintln(stderr, "cannot open snapshot")
		return 2
	}
	defer sf.Close()
	cf, err := os.Open(*cases)
	if err != nil {
		fmt.Fprintln(stderr, "cannot open cases")
		return 2
	}
	defer cf.Close()
	dataset, err := eval.Load(sf, cf)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	report, err := eval.Run(ctx, dataset, eval.Options{Split: *split, TopK: *topK, Revision: *revision})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, "cannot encode report")
		return 2
	}
	data = append(data, '\n')
	md := []byte(eval.Markdown(report))
	if *out != "" {
		if err := writeReports(*out, data, md); err != nil {
			fmt.Fprintln(stderr, "cannot create report directory/files (existing paths are never overwritten)")
			return 2
		}
	}
	content := data
	if *format == "markdown" {
		content = md
	}
	if _, err := stdout.Write(content); err != nil {
		return 2
	}
	if !report.Summary.GatePassed {
		return 1
	}
	return 0
}

func writeReports(directory string, data, markdown []byte) error {
	if err := os.Mkdir(directory, 0o700); err != nil {
		return err
	}
	for _, file := range []struct {
		name string
		data []byte
	}{{"report.json", data}, {"report.md", markdown}} {
		f, err := os.OpenFile(filepath.Join(directory, file.name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := f.Write(file.data)
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
