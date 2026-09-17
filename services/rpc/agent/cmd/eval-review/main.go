// Command eval-review 生成或校验人工复核记录；不运行推荐器或连接任何外部服务。
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"budgetmatch-sim/services/rpc/agent/internal/eval"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	f := flag.NewFlagSet("agent-eval-review", flag.ContinueOnError)
	f.SetOutput(stderr)
	snapshot := f.String("snapshot", "services/rpc/agent/testdata/eval/products.v1.json", "fixed synthetic snapshot")
	cases := f.String("cases", "services/rpc/agent/testdata/eval/cases.v1.jsonl", "full fixed JSONL case set")
	revision := f.String("revision", "unknown", "caller-supplied revision for a new review template")
	out := f.String("out", "", "NEW directory for review.json and review.md; never overwrites")
	check := f.String("check", "", "validate this filled review.json instead of generating a template")
	format := f.String("format", "json", "stdout format: json or markdown")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	invalid := f.NArg() != 0 || (*format != "json" && *format != "markdown")
	if *check != "" {
		f.Visit(func(f *flag.Flag) {
			if f.Name == "out" || f.Name == "revision" {
				invalid = true
			}
		})
	}
	if invalid {
		fmt.Fprintln(stderr, "invalid flags; check mode does not accept out or revision")
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
	d, err := eval.Load(sf, cf)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if *check != "" {
		file, err := os.Open(*check)
		if err != nil {
			fmt.Fprintln(stderr, "cannot open review")
			return 2
		}
		defer file.Close()
		r, err := eval.LoadReview(file)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		summary, err := eval.CheckReview(d, r)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		if err := emit(stdout, *format, summary, eval.ReviewSummaryMarkdown(summary)); err != nil {
			return 2
		}
		if !summary.RecordsComplete {
			return 1
		}
		return 0
	}
	r, err := eval.NewReview(d, *revision)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return 2
	}
	data = append(data, '\n')
	md := eval.ReviewMarkdown(d, r)
	if *out != "" {
		if err := writePacket(*out, data, []byte(md)); err != nil {
			fmt.Fprintln(stderr, "cannot create review directory/files; existing paths are never overwritten")
			return 2
		}
	}
	if err := emit(stdout, *format, r, md); err != nil {
		return 2
	}
	return 0 // 成功生成 pending 模板，不代表复核完成。
}

func emit(w io.Writer, format string, v any, markdown string) error {
	if format == "markdown" {
		_, err := io.WriteString(w, markdown)
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writePacket(directory string, data, markdown []byte) error {
	if err := os.Mkdir(directory, 0o700); err != nil {
		return err
	}
	for _, entry := range []struct {
		name string
		data []byte
	}{{"review.json", data}, {"review.md", markdown}} {
		f, err := os.OpenFile(filepath.Join(directory, entry.name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := f.Write(entry.data)
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
