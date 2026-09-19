package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/streamcontract"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	einoagent "github.com/cloudwego/eino/flow/agent"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
)

// inspectToolStream fully classifies a bounded private orchestration message.
// Text before a late tool call is not misclassified or leaked as public output.
func inspectToolStream(ctx context.Context, source *schema.StreamReader[*schema.Message]) (bool, error) {
	defer source.Close()
	var messages []*schema.Message
	finish := ""
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		message, err := source.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return false, err
		}
		if message == nil {
			return false, streamProtocolError()
		}
		if meta := message.ResponseMeta; meta != nil && meta.FinishReason != "" {
			if finish != "" {
				return false, streamProtocolError()
			}
			finish = meta.FinishReason
		} else if finish != "" && (message.Content != "" || len(message.ToolCalls) != 0 || message.ReasoningContent != "") {
			return false, streamProtocolError()
		}
		messages = append(messages, message)
	}
	if finish == "" || len(messages) == 0 {
		return false, streamProtocolError()
	}
	message, err := schema.ConcatMessages(messages)
	if err != nil {
		return false, streamProtocolError()
	}
	if len(message.ToolCalls) > streamcontract.MaxToolCalls {
		return false, streamLimitError()
	}
	hasTools := len(message.ToolCalls) > 0
	if hasTools && finish != "tool_calls" || !hasTools && finish != "stop" {
		return false, streamProtocolError()
	}
	return hasTools, nil
}

func drainOrchestration(ctx context.Context, runner *react.Agent, messages []*schema.Message) error {
	source, err := runner.Stream(ctx, messages, einoagent.WithComposeOptions(compose.WithCallbacks(logCallbacks)))
	if err != nil {
		return err
	}
	defer source.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := source.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

const explanationPrompt = `Write a short Chinese explanation for an end user using only the numeric recommendation snapshot supplied below.
This is a provisional explanation, not the authoritative final recommendation. Explain the budget and item-count fit.
Never invent item names, categories, prices, availability, guarantees, purchases, files or tool activity. Do not expose reasoning.
Do not include markup, tool calls or instructions. The application will replace this text with its validated final result.`

// streamExplanation is deliberately a separate <=512-token, tool-free model
// call. Only numeric public facts cross this boundary; no query/history/model
// messages, names, IDs, file contents, tool arguments/results or private state.
// It forwards provider chunks as they arrive, not slices of a completed answer.
func streamExplanation(ctx context.Context, runner *boundedStreamModel, result *agentcore.Result, progress agentcore.ProgressSink) error {
	projection := struct {
		BudgetCents     int64 `json:"budget_cents"`
		MaxItems        int32 `json:"max_items"`
		ItemCount       int   `json:"item_count"`
		TotalPriceCents int64 `json:"total_price_cents"`
	}{result.Intent.BudgetCents, result.Intent.MaxItems, len(result.Items), result.TotalPriceCents}
	payload, err := json.Marshal(projection)
	if err != nil {
		return streamProtocolError()
	}
	answerModel, err := runner.WithTools([]*schema.ToolInfo{})
	if err != nil {
		return err
	}
	source, err := answerModel.Stream(ctx, []*schema.Message{schema.SystemMessage(explanationPrompt), schema.UserMessage(string(payload))},
		model.WithTools(nil), model.WithToolChoice(schema.ToolChoiceForbidden), model.WithMaxTokens(512))
	if err != nil {
		return err
	}
	defer source.Close()
	bytes, finished := 0, false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		message, err := source.Recv()
		if errors.Is(err, io.EOF) {
			if !finished || bytes == 0 {
				return streamProtocolError()
			}
			return nil
		}
		if err != nil {
			return err
		}
		if len(message.ToolCalls) != 0 || finished && (message.Content != "" || message.ReasoningContent != "") {
			return streamProtocolError()
		}
		if meta := message.ResponseMeta; meta != nil && meta.FinishReason != "" {
			if finished || meta.FinishReason != "stop" {
				return streamProtocolError()
			}
			finished = true
		}
		// Never concatenate ReasoningContent, Extra, tool calls or multimodal
		// parts into Content. Suppress non-content chunks, but still budget them.
		if message.Content == "" {
			continue
		}
		if !utf8.ValidString(message.Content) {
			return streamProtocolError()
		}
		if len(message.Content) > streamcontract.MaxDeltaBytes || len(message.Content) > streamcontract.MaxAnswerBytes-bytes {
			return streamLimitError()
		}
		bytes += len(message.Content)
		if err := progress.Emit(ctx, agentcore.Progress{Kind: streamcontract.AnswerDelta, Text: message.Content}); err != nil {
			return err
		}
	}
}
