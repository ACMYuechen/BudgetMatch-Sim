package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync/atomic"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/runtrace"
	"budgetmatch-sim/services/rpc/agent/streamcontract"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// boundedStreamModel is request-scoped, including copies returned by WithTools.
// The bounds apply before Eino's branching copies can retain an entire call.
// They bound decoded payloads, not SDK allocations before a chunk is delivered.
type boundedStreamModel struct {
	base          model.ToolCallingChatModel
	tools         []*schema.ToolInfo
	calls         *atomic.Int32
	estimated     *atomic.Int64
	contextTokens int
}

func newBoundedStreamModel(base model.ToolCallingChatModel, contextTokens int) *boundedStreamModel {
	return &boundedStreamModel{base: base, calls: &atomic.Int32{}, estimated: &atomic.Int64{}, contextTokens: contextTokens}
}

func (m *boundedStreamModel) WithTools(infos []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	base, err := m.base.WithTools(infos)
	if err != nil {
		return nil, err
	}
	return &boundedStreamModel{base: base, tools: infos, calls: m.calls, estimated: m.estimated, contextTokens: m.contextTokens}, nil
}

func (*boundedStreamModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return nil, streamProtocolError() // never simulate streaming by splitting Generate
}

func (m *boundedStreamModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.calls.Add(1) > streamcontract.MaxModelCalls {
		return nil, streamLimitError()
	}
	common := model.GetCommonOptions(&model.Options{Tools: m.tools}, opts...)
	toolJSON, err := json.Marshal(common.Tools)
	if err != nil {
		return nil, streamProtocolError()
	}
	inputTokens := estimateMessagesTokens(input) + estimateTextTokens(string(toolJSON))
	if m.contextTokens > 0 && inputTokens > m.contextTokens {
		return nil, agentcore.ErrContextTooLarge
	}
	outputTokens := 1024
	if common.MaxTokens == nil || *common.MaxTokens > outputTokens {
		opts = append(opts, model.WithMaxTokens(1024))
	} else {
		outputTokens = *common.MaxTokens
	}
	if outputTokens <= 0 {
		return nil, streamLimitError()
	}
	reservation := int64(inputTokens) + int64(outputTokens)
	if m.estimated.Add(reservation) > streamcontract.MaxEstimatedTokens {
		return nil, streamLimitError()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	call := runtrace.From(ctx).BeginModel(reservation)
	source, err := m.base.Stream(ctx, input, opts...)
	if err != nil {
		call.End(false)
		return nil, err
	}
	if source == nil {
		call.End(false)
		return nil, streamProtocolError()
	}
	chunks, bytes := 0, 0
	return schema.StreamReaderWithConvert(source, func(message *schema.Message) (out *schema.Message, err error) {
		defer func() {
			if err != nil {
				call.End(false)
			}
		}()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		chunks++
		if chunks > streamcontract.MaxModelChunks {
			return nil, streamLimitError()
		}
		if message == nil || message.Role != "" && message.Role != schema.Assistant ||
			message.ToolCallID != "" || message.ToolName != "" || len(message.MultiContent) != 0 ||
			len(message.UserInputMultiContent) != 0 || len(message.AssistantGenMultiContent) != 0 {
			return nil, streamProtocolError()
		}
		encoded, err := json.Marshal(message) // includes reasoning/metadata in the private byte budget
		if err != nil {
			return nil, streamProtocolError()
		}
		if len(encoded) > streamcontract.MaxModelBytes-bytes {
			return nil, streamLimitError()
		}
		bytes += len(encoded)
		if meta := message.ResponseMeta; meta != nil && meta.FinishReason != "" && meta.FinishReason != "stop" && meta.FinishReason != "tool_calls" {
			return nil, streamProtocolError() // length/filter/unknown completion is not a complete answer
		}
		if meta := message.ResponseMeta; meta != nil && meta.Usage != nil {
			usage := meta.Usage
			call.Observe(runtrace.Usage{Prompt: int64(usage.PromptTokens), Completion: int64(usage.CompletionTokens), Total: int64(usage.TotalTokens)})
		}
		return message, nil
	}, schema.WithErrWrapper(func(err error) error { call.End(false); return err }),
		schema.WithOnEOF(func() (any, error) {
			if err := ctx.Err(); err != nil {
				call.End(false)
				return nil, err
			}
			call.End(true)
			return nil, io.EOF
		})), nil
}

func streamLimitError() error {
	return errors.Join(agentcore.ErrStreamLimit, status.Error(codes.ResourceExhausted, "agent stream resource limit"))
}

func streamProtocolError() error {
	return status.Error(codes.FailedPrecondition, "agent model stream protocol mismatch")
}
