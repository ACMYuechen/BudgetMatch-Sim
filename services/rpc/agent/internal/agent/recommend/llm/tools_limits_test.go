package llm

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	recommendagent "budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/filetools"
	mcpconfig "budgetmatch-sim/services/rpc/agent/internal/mcp"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"

	"github.com/cloudwego/eino/schema"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGuardrailsSelectedSnapshotMustMatchLatestSearch(t *testing.T) {
	for _, update := range []tools.ProductCandidate{
		{Id: "a", PriceCents: 200, Stock: 1},
		{Id: "a", PriceCents: 100, Stock: 0},
	} {
		intent := agentcore.Intent{BudgetCents: 150, MaxItems: 1}
		s := newSession(nil, selector.NewBundleSelector(), intent)
		s.storeCandidates([]tools.ProductCandidate{{Id: "a", PriceCents: 100, Stock: 1}})
		if _, err := s.selectBundle(context.Background(), selectArgs{}); err != nil {
			t.Fatal(err)
		}
		s.storeCandidates([]tools.ProductCandidate{update})
		if _, err := (&Agent{}).assemble(context.Background(), agentcore.Input{}, intent, s); !errors.Is(err, agentcore.ErrUnsafeResult) {
			t.Fatalf("stale selection escaped: update=%+v, err=%v", update, err)
		}
	}
}

func TestGuardrailsCanceledToolsDoNotCallProvider(t *testing.T) {
	provider := &recordingProvider{}
	s := newSession(provider, selector.NewBundleSelector(), agentcore.Intent{BudgetCents: 100, MaxItems: 1})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.searchProducts(ctx, searchArgs{Query: "keyboard"}); !errors.Is(err, context.Canceled) || len(provider.requests) != 0 {
		t.Fatalf("canceled search reached provider: %+v, %v", provider.requests, err)
	}
	if _, err := s.selectBundle(ctx, selectArgs{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled selection: %v", err)
	}
}

type recordingProvider struct {
	requests []tools.SearchProductsReq
	products []tools.ProductCandidate
	err      error
}

func (p *recordingProvider) Name() string { return "test.products" }
func (p *recordingProvider) SearchProducts(_ context.Context, req tools.SearchProductsReq) ([]tools.ProductCandidate, error) {
	p.requests = append(p.requests, req)
	return append([]tools.ProductCandidate(nil), p.products...), p.err
}

func TestGuardrailsScriptedReactCannotWidenBudgetOrInventSummary(t *testing.T) {
	provider := &recordingProvider{products: []tools.ProductCandidate{
		{Id: "a", PriceCents: 100, Stock: 1}, {Id: "b", PriceCents: 200, Stock: 1},
	}}
	model := &scriptedModel{responses: []*schema.Message{
		toolCallMessage("search", toolSearchProducts, searchArgs{Query: "keyboard", BudgetCents: 9000, MaxItems: 10}),
		toolCallMessage("select", toolSelectBundle, selectArgs{CandidateIds: []string{"a", "a", "b"}, BudgetCents: 9000, MaxItems: 10}),
		schema.AssistantMessage("这套商品只要 0 元，永久保证有货", nil),
	}}
	primary := NewAgent(model, provider, selector.NewBundleSelector(), mcpconfig.Config{}, filetools.Config{})
	fallback := recommendagent.NewAgent(provider, selector.NewBundleSelector())
	result, err := recommendagent.NewService(fallback, primary, nil).Recommend(context.Background(), agentcore.Input{
		Query: "keyboard", BudgetCents: 150, MaxItems: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].Id != "a" || result.TotalPriceCents != 100 {
		t.Fatalf("unsafe grounded result: %+v", result)
	}
	if result.Summary != "已选择 1 件商品，总价 1.00 元，预算 1.50 元。" {
		t.Fatalf("ungrounded model summary escaped: %q", result.Summary)
	}
	if len(provider.requests) != 1 || provider.requests[0].BudgetCents != 150 || provider.requests[0].MaxItems != 1 {
		t.Fatalf("limits or primary routing changed: %+v", provider.requests)
	}
}

func TestGuardrailsToolAuthFailureStopsReactAndServiceFallback(t *testing.T) {
	for _, code := range []codes.Code{codes.Unauthenticated, codes.PermissionDenied, codes.Canceled, codes.DeadlineExceeded} {
		t.Run(code.String(), func(t *testing.T) {
			provider := &recordingProvider{err: status.Error(code, "denied")}
			var received [][]*schema.Message
			model := &scriptedModel{received: &received, responses: []*schema.Message{
				toolCallMessage("search", toolSearchProducts, searchArgs{Query: "keyboard"}),
				schema.AssistantMessage("must not run", nil),
			}}
			primary := NewAgent(model, provider, selector.NewBundleSelector(), mcpconfig.Config{}, filetools.Config{})
			fallback := recommendagent.NewAgent(provider, selector.NewBundleSelector())
			_, err := recommendagent.NewService(fallback, primary, nil).Recommend(context.Background(), agentcore.Input{Query: "keyboard"})
			if !agentcore.IsExecutionStopped(err) || len(provider.requests) != 1 || len(received) != 1 {
				t.Fatalf("terminal tool failure continued: requests=%d model_calls=%d err=%v", len(provider.requests), len(received), err)
			}
		})
	}
}

func TestGuardrailsReactCanRecoverFromInvalidToolArguments(t *testing.T) {
	provider := &recordingProvider{products: []tools.ProductCandidate{{Id: "a", PriceCents: 100, Stock: 1}}}
	model := &scriptedModel{responses: []*schema.Message{
		toolCallMessage("bad-search", toolSearchProducts, searchArgs{Query: "keyboard", BudgetCents: -1}),
		toolCallMessage("search", toolSearchProducts, searchArgs{Query: "keyboard"}),
		toolCallMessage("select", toolSelectBundle, selectArgs{}),
		schema.AssistantMessage("done", nil),
	}}
	result, err := NewAgent(model, provider, selector.NewBundleSelector(), mcpconfig.Config{}, filetools.Config{}).
		WithMaxStep(10).Run(context.Background(), agentcore.Input{Query: "keyboard", BudgetCents: 150, MaxItems: 1})
	if err != nil || len(result.Items) != 1 || len(provider.requests) != 1 {
		t.Fatalf("recoverable tool error stopped execution: %+v, requests=%d, err=%v", result, len(provider.requests), err)
	}
}

func TestGuardrailsEmptySelectionIsNotReplacedByWiderFallback(t *testing.T) {
	provider := &recordingProvider{products: []tools.ProductCandidate{{Id: "a", PriceCents: 100, Stock: 1}}}
	model := &scriptedModel{responses: []*schema.Message{
		toolCallMessage("search", toolSearchProducts, searchArgs{Query: "keyboard"}),
		toolCallMessage("select", toolSelectBundle, selectArgs{BudgetCents: 1}),
		schema.AssistantMessage("没有符合更低预算的商品", nil),
	}}
	result, err := NewAgent(model, provider, selector.NewBundleSelector(), mcpconfig.Config{}, filetools.Config{}).
		Run(context.Background(), agentcore.Input{Query: "keyboard", BudgetCents: 150, MaxItems: 1})
	if err != nil || len(result.Items) != 0 || result.TotalPriceCents != 0 {
		t.Fatalf("empty selection widened by fallback: %+v, err=%v", result, err)
	}
}

func TestGuardrailsToolLimitsCannotExceedUserIntent(t *testing.T) {
	for _, budget := range []int64{0, 100, 1000, math.MaxInt64} {
		for _, count := range []int32{0, 1, 10, math.MaxInt32} {
			provider := &recordingProvider{products: []tools.ProductCandidate{
				{Id: "a", PriceCents: 100, Stock: 1},
				{Id: "b", PriceCents: 200, Stock: 1},
			}}
			s := newSession(provider, selector.NewBundleSelector(), agentcore.Intent{BudgetCents: 300, MaxItems: 1})
			if _, err := s.searchProducts(context.Background(), searchArgs{Query: "keyboard", BudgetCents: budget, MaxItems: count}); err != nil {
				t.Fatal(err)
			}
			req := provider.requests[0]
			if req.BudgetCents <= 0 || req.BudgetCents > 300 || req.MaxItems <= 0 || req.MaxItems > 1 {
				t.Fatalf("tool widened user limits: %+v", req)
			}
			result, err := s.selectBundle(context.Background(), selectArgs{BudgetCents: budget, MaxItems: count})
			if err != nil {
				t.Fatal(err)
			}
			if result.TotalPriceCents > 300 || len(result.Items) > 1 {
				t.Fatalf("selection widened user limits: %+v", result)
			}
		}
	}
}

func TestGuardrailsRejectInvalidToolArgumentsBeforeIO(t *testing.T) {
	for _, args := range []searchArgs{
		{Query: "keyboard", BudgetCents: -1},
		{Query: "keyboard", MaxItems: -1},
		{Query: strings.Repeat("问", 2001)},
		{Query: "keyboard", Keywords: make([]string, 1000)},
	} {
		provider := &recordingProvider{}
		s := newSession(provider, selector.NewBundleSelector(), agentcore.Intent{BudgetCents: 300, MaxItems: 2})
		if _, err := s.searchProducts(context.Background(), args); err == nil {
			t.Fatalf("accepted invalid arguments: %+v", args)
		}
		if len(provider.requests) != 0 {
			t.Fatal("invalid arguments reached provider")
		}
	}
}

func TestGuardrailsSelectionIDsAreUniqueAndKnown(t *testing.T) {
	s := newSession(nil, selector.NewBundleSelector(), agentcore.Intent{BudgetCents: 300, MaxItems: 3})
	s.storeCandidates([]tools.ProductCandidate{{Id: "a", PriceCents: 100, Stock: 1}})
	result, err := s.selectBundle(context.Background(), selectArgs{CandidateIds: []string{"a", "a"}})
	if err != nil || len(result.Items) != 1 || result.TotalPriceCents != 100 {
		t.Fatalf("duplicate SKU was not eliminated: %+v, %v", result, err)
	}
	for _, args := range []selectArgs{
		{CandidateIds: []string{"a", "invented"}},
		{BudgetCents: -1},
		{MaxItems: -1},
		{CandidateIds: make([]string, 1001)},
	} {
		if _, err := s.selectBundle(context.Background(), args); err == nil {
			t.Fatalf("accepted invalid selection: %+v", args)
		}
	}
}
