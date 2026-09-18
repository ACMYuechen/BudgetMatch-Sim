// Command eval 运行规则基线、脚本模型容错、检索或需求快照回放；不读取 .env 或服务配置，不构建外部客户端。
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
	suite := flags.String("suite", "rule", "rule, scripted (Fake Model + real ReAct), retrieval (offline ranks), or demand (offline selection + check replay)")
	retrievalFixture := flags.String("retrieval-fixture", "services/rpc/agent/testdata/eval/retrieval.v1.json", "retrieval suite only: versioned synthetic rankings and protocol bounds")
	demandFixture := flags.String("demand-fixture", "services/rpc/agent/testdata/eval/demand.v1.json", "demand suite only: versioned synthetic facts, demands and check faults")
	compare := flags.String("compare", "", "optional prior rule report.json; requires identical inputs and protocol")
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
	if flags.NArg() != 0 || (*format != "json" && *format != "markdown") || (*suite != "rule" && *suite != "scripted" && *suite != "retrieval" && *suite != "demand") {
		fmt.Fprintln(stderr, "invalid arguments or output format")
		return 2
	}
	if *suite == "demand" {
		invalid := false
		flags.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "suite", "demand-fixture", "revision", "format", "out":
			default:
				invalid = true
			}
		})
		if invalid {
			fmt.Fprintln(stderr, "demand suite uses fixed protocol bounds; other suite flags are not accepted")
			return 2
		}
		f, err := os.Open(*demandFixture)
		if err != nil {
			fmt.Fprintln(stderr, "cannot open demand fixture")
			return 2
		}
		defer f.Close()
		dataset, err := eval.LoadDemand(f)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		report, err := eval.RunDemand(ctx, dataset, *revision)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		return emit(report, eval.DemandMarkdown(report), report.GatePassed, *format, *out, stdout, stderr)
	}
	if *suite == "retrieval" {
		invalid := false
		flags.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "suite", "retrieval-fixture", "revision", "format", "out":
			default:
				invalid = true
			}
		})
		if invalid {
			fmt.Fprintln(stderr, "retrieval suite uses fixture-bound parameters; rule flags are not accepted")
			return 2
		}
		f, err := os.Open(*retrievalFixture)
		if err != nil {
			fmt.Fprintln(stderr, "cannot open retrieval fixture")
			return 2
		}
		defer f.Close()
		dataset, err := eval.LoadRetrieval(f)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		report, err := eval.RunRetrieval(ctx, dataset, *revision)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		return emit(report, eval.RetrievalMarkdown(report), report.GatePassed, *format, *out, stdout, stderr)
	}
	invalidFixtureFlag := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "retrieval-fixture" || f.Name == "demand-fixture" {
			invalidFixtureFlag = true
		}
	})
	if invalidFixtureFlag {
		fmt.Fprintln(stderr, "fixture flag requires its matching suite")
		return 2
	}
	if *suite == "scripted" {
		invalid := false
		flags.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "snapshot", "cases", "split", "top-k", "compare":
				invalid = true
			}
		})
		if invalid {
			fmt.Fprintln(stderr, "scripted suite has fixed fixtures; rule dataset/comparison flags are not accepted")
			return 2
		}
		report, err := eval.RunScripted(ctx, *revision)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		return emit(report, eval.ScriptedMarkdown(report), report.GatePassed, *format, *out, stdout, stderr)
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
	if *compare != "" {
		f, err := os.Open(*compare)
		if err != nil {
			fmt.Fprintln(stderr, "cannot open comparison report")
			return 2
		}
		before, loadErr := eval.LoadReport(f)
		closeErr := f.Close()
		if loadErr != nil || closeErr != nil {
			fmt.Fprintln(stderr, "invalid comparison report")
			return 2
		}
		report.Comparison, err = eval.Compare(before, report)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	}
	return emit(report, eval.Markdown(report), report.Summary.GatePassed, *format, *out, stdout, stderr)
}

func emit(report any, markdown string, gatePassed bool, format, out string, stdout, stderr io.Writer) int {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, "cannot encode report")
		return 2
	}
	data = append(data, '\n')
	md := []byte(markdown)
	if out != "" {
		if err := writeReports(out, data, md); err != nil {
			fmt.Fprintln(stderr, "cannot create report directory/files (existing paths are never overwritten)")
			return 2
		}
	}
	content := data
	if format == "markdown" {
		content = md
	}
	if _, err := stdout.Write(content); err != nil {
		return 2
	}
	if !gatePassed {
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
