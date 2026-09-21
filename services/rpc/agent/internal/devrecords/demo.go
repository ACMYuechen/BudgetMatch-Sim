package devrecords

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

type DemoReport struct {
	UserID         string `json:"user_id"`
	ConversationID string `json:"conversation_id"`
	Created        bool   `json:"created"`
	Turns          int    `json:"turns"`
	ProviderCalls  int    `json:"provider_calls"`
	ReplayVerified bool   `json:"replay_verified"`
	Source         string `json:"source"`
}

type demoSnapshot struct {
	Conversation memory.Conversation
	Turns        []memory.Turn
	Messages     []string
}

func demoInputs(o Options) []agentcore.Input {
	conversation := "dev-demo-v1-" + o.RunID
	return []agentcore.Input{
		{UserId: o.UserID, ConversationId: conversation, TurnId: "demo-v1-first",
			Query: "【开发演示】办公桌面推荐", BudgetCents: 60000, MaxItems: 3},
		{UserId: o.UserID, ConversationId: conversation, TurnId: "demo-v1-budget",
			Query: "预算改为 400 元，最多 2 件", BudgetCents: 40000, MaxItems: 2},
	}
}

type countedProvider struct {
	tools.ProductProvider
	calls int
}

func (p *countedProvider) SearchProducts(ctx context.Context, in tools.SearchProductsReq) ([]tools.ProductCandidate, error) {
	p.calls++
	return p.ProductProvider.SearchProducts(ctx, in)
}

func demoService(store memory.ConversationStore) (*recommend.Service, *countedProvider) {
	p := &countedProvider{ProductProvider: tools.NewMockProductProvider()}
	a := recommend.NewAgent(p, selector.NewBundleSelector()).WithMemory(store, 20)
	return recommend.NewService(a, nil, store).WithFinalizer(recommend.DemoFinalizer{}), p
}

func readSnapshot(ctx context.Context, store memory.ConversationStore, o Options) (demoSnapshot, bool, error) {
	in := demoInputs(o)[0]
	c, turns, total, exists, err := store.ListTurns(ctx, o.UserID, in.ConversationId, 1, 10)
	if err != nil || !exists {
		return demoSnapshot{}, exists, err
	}
	if total != int64(len(turns)) {
		return demoSnapshot{}, true, errors.New("demo namespace contains unexpected additional turns")
	}
	history, err := store.History(ctx, o.UserID, in.ConversationId, 20)
	s := demoSnapshot{Conversation: c, Turns: turns}
	for _, msg := range history {
		s.Messages = append(s.Messages, string(msg.Role)+":"+msg.Content)
	}
	return s, true, err
}

// Fixture comparison ignores only timestamps (created by a different run).
// Replay comparison keeps timestamps too. JSONB key order is not data loss;
// UseNumber avoids hiding changes above float64 integer precision.
func snapshotJSON(s demoSnapshot, fixture bool) ([]byte, error) {
	s.Conversation.CreatedAt = s.Conversation.CreatedAt.UTC()
	s.Conversation.UpdatedAt = s.Conversation.UpdatedAt.UTC()
	s.Turns = append([]memory.Turn{}, s.Turns...)
	if fixture {
		s.Conversation.CreatedAt, s.Conversation.UpdatedAt = time.Time{}, time.Time{}
	}
	for i := range s.Turns {
		s.Turns[i].CreatedAt = s.Turns[i].CreatedAt.UTC()
		s.Turns[i].CompletedAt = s.Turns[i].CompletedAt.UTC()
		if fixture {
			s.Turns[i].CreatedAt, s.Turns[i].CompletedAt = time.Time{}, time.Time{}
		}
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func sameSnapshot(a, b demoSnapshot, fixture bool) bool {
	aa, errA := snapshotJSON(a, fixture)
	bb, errB := snapshotJSON(b, fixture)
	return errA == nil && errB == nil && bytes.Equal(aa, bb)
}

func expectedSnapshot(ctx context.Context, o Options) (demoSnapshot, error) {
	store := memory.NewInMemory(memory.Conf{})
	svc, _ := demoService(store)
	for _, in := range demoInputs(o) {
		if _, err := svc.Recommend(ctx, in); err != nil {
			return demoSnapshot{}, err
		}
	}
	s, _, err := readSnapshot(ctx, store, o)
	return s, err
}

// exercise is invoked under ONE database transaction and the production lock
// key. Existing data must match the entire versioned fixture before replay; a
// partial, edited or foreign conversation is refused, never repaired/overwritten.
func exercise(ctx context.Context, store memory.ConversationStore, o Options) (*DemoReport, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if !o.WriteDemo {
		return nil, errors.New("demo writes are not selected")
	}
	expected, err := expectedSnapshot(ctx, o)
	if err != nil {
		return nil, errors.New("cannot prepare deterministic demo fixture")
	}
	before, exists, err := readSnapshot(ctx, store, o)
	if err != nil {
		return nil, errors.New("cannot inspect demo namespace")
	}
	if exists && !sameSnapshot(before, expected, true) {
		return nil, errors.New("demo namespace already contains different or partial data; choose a new run-id")
	}
	svc, provider := demoService(store)
	for _, in := range demoInputs(o) {
		if _, err := svc.Recommend(ctx, in); err != nil {
			return nil, errors.New("demo recommendation could not be persisted")
		}
	}
	after, found, err := readSnapshot(ctx, store, o)
	if err != nil || !found || !sameSnapshot(after, expected, true) {
		return nil, errors.New("saved demo differs from the expected fixture")
	}
	wantCalls := 2
	if exists {
		wantCalls = 0
		if !sameSnapshot(before, after, false) {
			return nil, errors.New("replay changed existing demo data")
		}
	}
	if provider.calls != wantCalls {
		return nil, errors.New("unexpected demo provider calls")
	}
	// Check another replay with a new Service; it must not generate or save again.
	replay, replayProvider := demoService(store)
	for _, in := range demoInputs(o) {
		if _, err := replay.Recommend(ctx, in); err != nil {
			return nil, errors.New("demo replay failed")
		}
	}
	verified, _, err := readSnapshot(ctx, store, o)
	if err != nil || replayProvider.calls != 0 || !sameSnapshot(after, verified, false) {
		return nil, errors.New("demo replay changed committed values or generated again")
	}
	return &DemoReport{UserID: o.UserID, ConversationID: demoInputs(o)[0].ConversationId,
		Created: !exists, Turns: len(after.Turns), ProviderCalls: provider.calls,
		ReplayVerified: true, Source: "local_rule_with_mock_products_not_live_mall"}, nil
}
