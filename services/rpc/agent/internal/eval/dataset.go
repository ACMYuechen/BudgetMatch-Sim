// Package eval 提供无网络依赖的固定快照评测，不改变线上推荐策略。
package eval

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
)

const maxDatasetBytes = 4 << 20

type Product struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Category   string   `json:"category"`
	PriceCents int64    `json:"price_cents"`
	Stock      int64    `json:"stock"`
	Sold       int64    `json:"sold"`
	Active     bool     `json:"active"`
	Tags       []string `json:"tags"`
}

type Snapshot struct {
	Version          string    `json:"version"`
	Provenance       string    `json:"provenance"`
	AnnotationStatus string    `json:"annotation_status"`
	Products         []Product `json:"products"`
}

type TurnInput struct {
	Query       string `json:"query"`
	BudgetCents int64  `json:"budget_cents"`
	MaxItems    int32  `json:"max_items"`
}

type Requirement struct {
	ID        string   `json:"id"`
	AnyOfSKUs []string `json:"any_of_skus"`
}

type Expected struct {
	Status         string        `json:"status"`      // completed / rejected / error
	Feasibility    string        `json:"feasibility"` // satisfiable / unsatisfiable / needs_clarification / not_applicable
	BudgetCents    int64         `json:"budget_cents"`
	MaxItems       int32         `json:"max_items"`
	AcceptableSKUs []string      `json:"acceptable_skus"`
	RelevantSKUs   []string      `json:"relevant_skus"`
	Requirements   []Requirement `json:"requirements"`
	ErrorCode      string        `json:"error_code,omitempty"`
	Fallback       bool          `json:"fallback"`
}

type Case struct {
	ID              string      `json:"id"`
	Family          string      `json:"family"` // 同族改写不得跨调参集/保留集
	Split           string      `json:"split"`  // dev / holdout
	Scenario        string      `json:"scenario"`
	SnapshotVersion string      `json:"snapshot_version"`
	Input           TurnInput   `json:"input"`
	History         []TurnInput `json:"history"`
	Fault           string      `json:"fault,omitempty"`
	Expected        Expected    `json:"expected"`
}

type Dataset struct {
	Snapshot       Snapshot
	Cases          []Case
	SnapshotSHA256 string
	CasesSHA256    string
}

var safeID = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,79}$`)

// Load 严格校验格式、唯一标识、划分、引用和基本标注一致性；不声称替代人工语义复核。
func Load(snapshot, cases io.Reader) (Dataset, error) {
	var d Dataset
	data, err := readBounded(snapshot)
	if err != nil {
		return d, err
	}
	d.SnapshotSHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
	if err = decodeStrict(data, &d.Snapshot); err != nil {
		return d, errors.New("invalid snapshot JSON")
	}
	data, err = readBounded(cases)
	if err != nil {
		return d, err
	}
	d.CasesSHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 64<<10)
	line := 0
	for scanner.Scan() {
		line++
		if strings.TrimSpace(scanner.Text()) == "" {
			return d, fmt.Errorf("blank case at line %d", line)
		}
		var c Case
		if err := decodeStrict(scanner.Bytes(), &c); err != nil {
			return d, fmt.Errorf("invalid case JSON at line %d", line)
		}
		d.Cases = append(d.Cases, c)
		if len(d.Cases) > 1000 {
			return d, errors.New("too many evaluation cases")
		}
	}
	if scanner.Err() != nil {
		return d, errors.New("case line exceeds size limit")
	}
	if err := d.Validate(); err != nil {
		return Dataset{}, err
	}
	return d, nil
}

func readBounded(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxDatasetBytes+1))
	if err != nil {
		return nil, errors.New("cannot read evaluation data")
	}
	if len(b) > maxDatasetBytes {
		return nil, errors.New("evaluation data exceeds size limit")
	}
	return b, nil
}

func decodeStrict(data []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}

func (d Dataset) Validate() error {
	s := d.Snapshot
	if !safeID.MatchString(s.Version) || s.Provenance != "synthetic" || s.AnnotationStatus != "pending_human_review" {
		return errors.New("snapshot requires version and explicit synthetic/unreviewed provenance")
	}
	if len(s.Products) == 0 || len(s.Products) > 256 || len(d.Cases) == 0 || len(d.Cases) > 1000 {
		return errors.New("invalid dataset size")
	}
	products := map[string]Product{}
	for _, p := range s.Products {
		if !safeID.MatchString(p.ID) || p.Name == "" || p.Category == "" || p.PriceCents <= 0 || p.PriceCents > agent.MaxBudgetCents || p.Stock < 0 || p.Sold < 0 {
			return errors.New("invalid snapshot product")
		}
		if _, exists := products[p.ID]; exists {
			return errors.New("duplicate snapshot product")
		}
		products[p.ID] = p
	}
	seen, families := map[string]bool{}, map[string]string{}
	inputSplits := map[string]string{}
	for i, c := range d.Cases {
		bad := func(reason string) error { return fmt.Errorf("case %d: %s", i+1, reason) }
		if !safeID.MatchString(c.ID) || seen[c.ID] || !safeID.MatchString(c.Family) || !safeID.MatchString(c.Scenario) {
			return bad("invalid or duplicate identifier")
		}
		seen[c.ID] = true
		if c.Split != "dev" && c.Split != "holdout" {
			return bad("split must be dev or holdout")
		}
		if previous := families[c.Family]; previous != "" && previous != c.Split {
			return bad("family leaks across splits")
		}
		families[c.Family] = c.Split
		inputKey, _ := json.Marshal(struct {
			Input   TurnInput
			History []TurnInput
			Fault   string
		}{c.Input, c.History, c.Fault})
		if previous := inputSplits[string(inputKey)]; previous != "" && previous != c.Split {
			return bad("identical execution input leaks across splits")
		}
		inputSplits[string(inputKey)] = c.Split
		if c.SnapshotVersion != s.Version || len(c.Input.Query) > 16000 || len(c.History) > 10 {
			return bad("version or input size invalid")
		}
		for _, h := range c.History {
			if len(h.Query) > 16000 {
				return bad("history query too large")
			}
		}
		switch c.Fault {
		case "", "provider_unavailable", "provider_permission", "primary_unavailable":
		default:
			return bad("unsupported fault")
		}
		e := c.Expected
		switch e.Status {
		case "completed":
			if _, err := agent.NewConstraints(agent.Intent{BudgetCents: e.BudgetCents, MaxItems: e.MaxItems}); err != nil || e.ErrorCode != "" {
				return bad("completed case requires valid oracle limits")
			}
			if e.Feasibility != "satisfiable" && e.Feasibility != "unsatisfiable" {
				return bad("completed case requires feasibility")
			}
		case "rejected":
			if e.Feasibility != "needs_clarification" {
				return bad("rejected case requires clarification label")
			}
		case "error":
			if e.Feasibility != "not_applicable" {
				return bad("error case requires not_applicable label")
			}
		default:
			return bad("invalid expected status")
		}
		if e.Status != "completed" {
			if !validErrorCode(e.ErrorCode) || len(e.Requirements) > 0 || len(e.AcceptableSKUs) > 0 || len(e.RelevantSKUs) > 0 {
				return bad("invalid error oracle")
			}
		}
		for _, ids := range [][]string{e.AcceptableSKUs, e.RelevantSKUs} {
			refs := map[string]bool{}
			for _, id := range ids {
				p, ok := products[id]
				if !ok || refs[id] || !p.Active || p.Stock <= 0 || p.PriceCents > e.BudgetCents {
					return bad("invalid available SKU annotation")
				}
				refs[id] = true
			}
		}
		if len(e.Requirements) > 8 {
			return bad("too many requirements")
		}
		reqs := map[string]bool{}
		for _, r := range e.Requirements {
			if !safeID.MatchString(r.ID) || reqs[r.ID] || len(r.AnyOfSKUs) == 0 {
				return bad("invalid requirement")
			}
			reqs[r.ID] = true
			ids := map[string]bool{}
			for _, id := range r.AnyOfSKUs {
				if _, ok := products[id]; !ok || ids[id] {
					return bad("invalid requirement SKU")
				}
				ids[id] = true
			}
		}
		if e.Status == "completed" {
			if len(e.Requirements) == 0 {
				return bad("completed case requires demand annotations")
			}
			if feasible(e, products) != (e.Feasibility == "satisfiable") {
				return bad("feasibility contradicts acceptable SKU budget/coverage")
			}
		}
		if e.Fallback != (c.Fault == "primary_unavailable") {
			return bad("fallback label contradicts configured fault")
		}
	}
	return nil
}

// feasible 仅验证人工候选标注是否有满足预算/件数/覆盖的组合，不参与实际推荐。
func feasible(e Expected, products map[string]Product) bool {
	masks := 1 << len(e.Requirements)
	const unreachable = agent.MaxBudgetCents + 1
	dp := make([][]int64, int(e.MaxItems)+1)
	for n := range dp {
		dp[n] = make([]int64, masks)
		for m := range dp[n] {
			dp[n][m] = unreachable
		}
	}
	dp[0][0] = 0
	for _, id := range e.AcceptableSKUs {
		mask := 0
		for i, r := range e.Requirements {
			if contains(r.AnyOfSKUs, id) {
				mask |= 1 << i
			}
		}
		for n := int(e.MaxItems); n > 0; n-- {
			for m, cost := range dp[n-1] {
				if cost <= e.BudgetCents-products[id].PriceCents {
					next := m | mask
					dp[n][next] = min(dp[n][next], cost+products[id].PriceCents)
				}
			}
		}
	}
	for n := 1; n < len(dp); n++ {
		if dp[n][masks-1] <= e.BudgetCents {
			return true
		}
	}
	return false
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func validErrorCode(code string) bool {
	switch code {
	case "invalid_argument", "budget_currency", "budget_text", "item_limit_text", "context_limit", "execution_failed", "permission_denied", "canceled", "deadline_exceeded":
		return true
	}
	return false
}
