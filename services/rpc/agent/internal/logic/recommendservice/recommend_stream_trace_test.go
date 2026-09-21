package recommendservicelogic_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"budgetmatch-sim/infra/interceptor"
	logic "budgetmatch-sim/services/rpc/agent/internal/logic/recommendservice"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	"budgetmatch-sim/services/rpc/agent/internal/runtrace"
	"budgetmatch-sim/services/rpc/agent/internal/svc"
	"budgetmatch-sim/services/rpc/agent/pb"
	"budgetmatch-sim/services/rpc/agent/streamcontract"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type summaryLogBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *summaryLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}
func (b *summaryLogBuffer) text() string { b.mu.Lock(); defer b.mu.Unlock(); return b.Buffer.String() }

func TestRecommendStreamTraceSummarizesModelUsageFailureAndReplay(t *testing.T) {
	for _, mode := range []string{"success", "missing usage", "save failure", "final send failure"} {
		t.Run(mode, func(t *testing.T) {
			var logs summaryLogBuffer
			previous := logx.Reset()
			logx.SetWriter(logx.NewWriter(&logs))
			t.Cleanup(func() { logx.SetWriter(previous) })
			script := &modelStreamScript{answer: answerChunks(), usage: mode != "missing usage"}
			store := newStreamStore()
			if mode == "save failure" {
				store.save = func(context.Context, memory.SaveTurnReq) (memory.Conversation, memory.Turn, error) {
					return memory.Conversation{}, memory.Turn{}, errors.New("PRIVATE failed save")
				}
			}
			service := newModelStreamService(script, store, &streamAgent{}, &streamFinalizer{})
			ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), interceptor.ContextKeyUserId, "PRIVATE user"), time.Second)
			defer cancel()
			fake := &fakeRecommendStream{ctx: ctx}
			if mode == "final send failure" {
				fake.send = func(event *pb.RecommendStreamEvent) error {
					if event.Event == streamcontract.Final {
						return status.Error(codes.Unavailable, "PRIVATE send failure")
					}
					return nil
				}
			}
			req := streamRequest()
			req.Query = "PRIVATE request"
			req.BudgetCents = 300000
			err := logic.NewRecommendStreamLogic(ctx, &svc.ServiceContext{RecommendService: service}).RecommendStream(req, fake)
			if mode == "final send failure" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if mode == "success" || mode == "final send failure" {
				replay := &fakeRecommendStream{ctx: ctx}
				require.NoError(t, logic.NewRecommendStreamLogic(ctx, &svc.ServiceContext{RecommendService: service}).RecommendStream(req, replay))
			}
			require.NotContains(t, logs.text(), "PRIVATE")
			var summaries []runtrace.Summary
			for _, line := range strings.Split(logs.text(), "\n") {
				var entry struct {
					Content   string           `json:"content"`
					Execution runtrace.Summary `json:"execution"`
				}
				if json.Unmarshal([]byte(line), &entry) == nil && entry.Content == "recommendation stream summary" {
					summaries = append(summaries, entry.Execution)
				}
			}
			require.NotEmpty(t, summaries)
			first := summaries[0]
			require.Equal(t, fake.events[0].ExecutionId, first.ExecutionID)
			require.Equal(t, 4, first.ModelCalls)
			require.Equal(t, "primary", first.Path)
			require.Equal(t, "mock.product_provider", first.SelectedProvider)
			require.NotNil(t, first.FirstDeltaMS)
			require.Equal(t, len(fake.events), first.EventsSent)
			require.Equal(t, "unknown", first.CostStatus)
			require.Equal(t, mode != "save failure", first.Committed)
			if mode == "missing usage" {
				require.Equal(t, "unknown", first.UsageStatus)
				require.Nil(t, first.ReportedUsage)
			} else {
				require.Equal(t, "complete", first.UsageStatus)
				require.Equal(t, &runtrace.Usage{Prompt: 40, Completion: 8, Total: 48}, first.ReportedUsage)
			}
			switch mode {
			case "save failure":
				require.Equal(t, "failed", first.Outcome)
				require.False(t, fake.events[len(fake.events)-1].GetDone().Ok)
			case "final send failure":
				require.Equal(t, "transport_error", first.Outcome)
			default:
				require.Equal(t, "succeeded", first.Outcome)
			}
			if mode == "success" || mode == "final send failure" {
				require.Len(t, summaries, 2)
				require.NotEqual(t, first.ExecutionID, summaries[1].ExecutionID)
				require.Equal(t, "replayed", summaries[1].Outcome)
				require.Zero(t, summaries[1].ModelCalls)
				require.Nil(t, summaries[1].ReportedUsage)
				require.Nil(t, summaries[1].FirstDeltaMS)
			} else {
				require.Len(t, summaries, 1)
			}
		})
	}
}
