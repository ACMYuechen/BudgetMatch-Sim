package recommend

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
)

func TestSavePathDoesNotBecomeShoppingConstraint(t *testing.T) {
	p := NewPlanner()
	want, err := p.Resolve(agent.Input{Query: "预算500元最多两件键盘"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	query := "/save 耳机预算3000元最多八件.md\n预算500元最多两件键盘"
	got, err := p.Resolve(agent.Input{Query: query}, nil)
	if err != nil || !reflect.DeepEqual(want, got) {
		t.Fatalf("path influenced intent: %+v %v", got, err)
	}
	got, err = p.Resolve(agent.Input{Query: "继续"}, []string{query})
	if err != nil || !reflect.DeepEqual(want, got) {
		t.Fatalf("historical path influenced intent: %+v %v", got, err)
	}
}

func TestServiceRedactsBeforePersistenceAndReplay(t *testing.T) {
	mem := memory.NewInMemory(memory.Conf{})
	runner := agentFunc(func(context.Context, agent.Input) (*agent.Result, error) {
		return &agent.Result{ToolsUsed: []agent.ToolCall{{Name: "tool.read_file", Success: true, Detail: "PRIVATE"}}}, nil
	})
	s := NewService(runner, nil, mem)
	in := agent.Input{UserId: "u", ConversationId: "c", TurnId: "t", Query: "keyboard"}
	r, err := s.Recommend(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(r.ToolsUsed), "PRIVATE") {
		t.Fatal(r.ToolsUsed)
	}
	_, turns, _, _, err := s.ListTurns(context.Background(), "u", "c", 1, 10)
	if err != nil || len(turns) != 1 {
		t.Fatalf("turns: %v %v", turns, err)
	}
	if strings.Contains(string(turns[0].ResultJSON), "PRIVATE") {
		t.Fatal("persisted raw details")
	}
	legacy, _ := json.Marshal(agent.Result{ToolsUsed: []agent.ToolCall{{Name: "tool.PRIVATE", Detail: "PRIVATE"}}})
	replayed, err := decodeSavedResult(memory.Turn{ResultJSON: legacy})
	if err != nil || strings.Contains(fmt.Sprint(replayed.ToolsUsed), "PRIVATE") {
		t.Fatalf("replay: %v %v", replayed, err)
	}
}
