package llm

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
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
	contextTokens int
}

func newBoundedStreamModel(base model.ToolCallingChatModel, contextTokens int) *boundedStreamModel {
	return &boundedStreamModel{base: base, calls: &atomic.Int32{}, contextTokens: contextTokens}
}

func (m *boundedStreamModel) WithTools(infos []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	base, err := m.base.WithTools(infos)
	if err != nil {
		return nil, err
	}
	return &boundedStreamModel{base: base, tools: infos, calls: m.calls, contextTokens: m.contextTokens}, nil
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
	if m.contextTokens > 0 && estimateMessagesTokens(input)+estimateTextTokens(string(toolJSON)) > m.contextTokens {
		return nil, agentcore.ErrContextTooLarge
	}
	if common.MaxTokens == nil || *common.MaxTokens > 1024 {
		opts = append(opts, model.WithMaxTokens(1024))
	}
	source, err := m.base.Stream(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, streamProtocolError()
	}
	chunks, bytes := 0, 0
	return schema.StreamReaderWithConvert(source, func(message *schema.Message) (*schema.Message, error) {
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
		return message, nil
	}), nil
}

func streamLimitError() error {
	return errors.Join(agentcore.ErrStreamLimit, status.Error(codes.ResourceExhausted, "agent stream resource limit"))
}

func streamProtocolError() error {
	return status.Error(codes.FailedPrecondition, "agent model stream protocol mismatch")
}
