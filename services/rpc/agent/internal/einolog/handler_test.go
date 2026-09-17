package einolog

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/zeromicro/go-zero/core/logx"
)

func TestHandlerDoesNotLogToolPayloadOrRawError(t *testing.T) {
	var buf bytes.Buffer
	previous := logx.Reset()
	logx.SetWriter(logx.NewWriter(&buf))
	t.Cleanup(func() { logx.SetWriter(previous) })
	const secret = "M13_PRIVATE_MARKER"
	handler := NewHandler()
	info := &callbacks.RunInfo{Component: components.ComponentOfTool, Name: "read_file"}
	ctx := handler.OnStart(context.Background(), info, &tool.CallbackInput{ArgumentsInJSON: secret})
	handler.OnEnd(ctx, info, &tool.CallbackOutput{Response: secret})
	handler.OnError(ctx, info, errors.New(secret))
	if strings.Contains(buf.String(), secret) {
		t.Fatalf("secret in callback log: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "duration_ms") {
		t.Fatal("timing metadata missing")
	}
}

func TestHandlerDoesNotLogQueriesSourcesMessagesOrExternalNames(t *testing.T) {
	var buf bytes.Buffer
	previous := logx.Reset()
	logx.SetWriter(logx.NewWriter(&buf))
	t.Cleanup(func() { logx.SetWriter(previous) })
	const secret = "M13_PRIVATE_MARKER"
	h := NewHandler()
	for _, tc := range []struct {
		component components.Component
		input     callbacks.CallbackInput
	}{
		{components.ComponentOfChatModel, &model.CallbackInput{Messages: []*schema.Message{schema.UserMessage(secret)}}},
		{components.ComponentOfRetriever, &retriever.CallbackInput{Query: secret}},
		{components.ComponentOfEmbedding, &embedding.CallbackInput{Texts: []string{secret}}},
		{components.ComponentOfLoader, &document.LoaderCallbackInput{Source: document.Source{URI: secret}}},
	} {
		info := &callbacks.RunInfo{Component: tc.component, Name: secret, Type: secret}
		ctx := h.OnStart(context.Background(), info, tc.input)
		h.OnError(ctx, info, errors.New(secret))
	}
	if strings.Contains(buf.String(), secret) {
		t.Fatal(buf.String())
	}
}
