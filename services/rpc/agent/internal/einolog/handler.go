// Package einolog 提供 Eino 组件的统一日志回调。
//
// 通过 eino 的 utils/callbacks.NewHandlerHelper 构建类型化 handler，
// 覆盖模型调用、工具执行、向量检索、嵌入与离线索引/加载链路，
// 全部输出到 go-zero logx（与请求 trace 上下文自动关联）。
//
// handler 本身无状态（耗时通过 ctx 传递），构建一次全局复用：
// 在线链路挂在 ReAct Generate 的 compose 选项上，
// 离线链路（商品向量同步）通过 callbacks.InitCallbacks 注入。
package einolog

import (
	"context"
	"math"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/safety"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/components/tool"
	utilcallbacks "github.com/cloudwego/eino/utils/callbacks"
	"github.com/zeromicro/go-zero/core/logx"
)

// NewHandler 构建覆盖全组件的日志回调 handler。
func NewHandler() callbacks.Handler {
	return utilcallbacks.NewHandlerHelper().
		ChatModel(&utilcallbacks.ModelCallbackHandler{
			OnStart: func(ctx context.Context, info *callbacks.RunInfo, input *model.CallbackInput) context.Context {
				n := 0
				if input != nil {
					n = len(input.Messages)
				}
				logx.WithContext(ctx).Infow("eino chat_model start", fields(info, logx.Field("messages", n))...)
				return markStart(ctx)
			},
			OnEnd: func(ctx context.Context, info *callbacks.RunInfo, output *model.CallbackOutput) context.Context {
				extra := []logx.LogField{logx.Field("duration_ms", durationMS(ctx))}
				if output != nil && output.TokenUsage != nil {
					extra = append(extra,
						logx.Field("prompt_tokens", output.TokenUsage.PromptTokens),
						logx.Field("completion_tokens", output.TokenUsage.CompletionTokens),
					)
				}
				logx.WithContext(ctx).Infow("eino chat_model end", fields(info, extra...)...)
				return ctx
			},
			OnError: onError("eino chat_model error"),
		}).
		Tool(&utilcallbacks.ToolCallbackHandler{
			OnStart: func(ctx context.Context, info *callbacks.RunInfo, input *tool.CallbackInput) context.Context {
				argsBytes := 0
				if input != nil {
					argsBytes = len(input.ArgumentsInJSON)
				}
				logx.WithContext(ctx).Infow("eino tool start", fields(info, logx.Field("argument_bytes", argsBytes))...)
				return markStart(ctx)
			},
			OnEnd: func(ctx context.Context, info *callbacks.RunInfo, output *tool.CallbackOutput) context.Context {
				responseBytes := 0
				if output != nil {
					responseBytes = len(output.Response)
				}
				logx.WithContext(ctx).Infow("eino tool end", fields(info,
					logx.Field("duration_ms", durationMS(ctx)),
					logx.Field("response_bytes", responseBytes),
				)...)
				return ctx
			},
			OnError: onError("eino tool error"),
		}).
		Retriever(&utilcallbacks.RetrieverCallbackHandler{
			OnStart: func(ctx context.Context, info *callbacks.RunInfo, input *retriever.CallbackInput) context.Context {
				extra := []logx.LogField{}
				if input != nil {
					extra = append(extra, logx.Field("query_bytes", len(input.Query)), logx.Field("top_k", input.TopK))
				}
				logx.WithContext(ctx).Infow("eino retriever start", fields(info, extra...)...)
				return markStart(ctx)
			},
			OnEnd: func(ctx context.Context, info *callbacks.RunInfo, output *retriever.CallbackOutput) context.Context {
				extra := []logx.LogField{logx.Field("duration_ms", durationMS(ctx))}
				if output != nil {
					extra = append(extra, logx.Field("docs", len(output.Docs)))
					if lo, hi, ok := scoreRange(output); ok {
						extra = append(extra, logx.Field("score_min", lo), logx.Field("score_max", hi))
					}
				}
				logx.WithContext(ctx).Infow("eino retriever end", fields(info, extra...)...)
				return ctx
			},
			OnError: onError("eino retriever error"),
		}).
		Embedding(&utilcallbacks.EmbeddingCallbackHandler{
			OnStart: func(ctx context.Context, info *callbacks.RunInfo, input *embedding.CallbackInput) context.Context {
				n := 0
				if input != nil {
					n = len(input.Texts)
				}
				logx.WithContext(ctx).Infow("eino embedding start", fields(info, logx.Field("texts", n))...)
				return markStart(ctx)
			},
			OnEnd: func(ctx context.Context, info *callbacks.RunInfo, output *embedding.CallbackOutput) context.Context {
				logx.WithContext(ctx).Infow("eino embedding end", fields(info, logx.Field("duration_ms", durationMS(ctx)))...)
				return ctx
			},
			OnError: onError("eino embedding error"),
		}).
		Indexer(&utilcallbacks.IndexerCallbackHandler{
			OnStart: func(ctx context.Context, info *callbacks.RunInfo, input *indexer.CallbackInput) context.Context {
				n := 0
				if input != nil {
					n = len(input.Docs)
				}
				logx.WithContext(ctx).Infow("eino indexer start", fields(info, logx.Field("docs", n))...)
				return markStart(ctx)
			},
			OnEnd: func(ctx context.Context, info *callbacks.RunInfo, output *indexer.CallbackOutput) context.Context {
				extra := []logx.LogField{logx.Field("duration_ms", durationMS(ctx))}
				if output != nil {
					extra = append(extra, logx.Field("stored", len(output.IDs)))
				}
				logx.WithContext(ctx).Infow("eino indexer end", fields(info, extra...)...)
				return ctx
			},
			OnError: onError("eino indexer error"),
		}).
		Loader(&utilcallbacks.LoaderCallbackHandler{
			OnStart: func(ctx context.Context, info *callbacks.RunInfo, input *document.LoaderCallbackInput) context.Context {
				sourceBytes := 0
				if input != nil {
					sourceBytes = len(input.Source.URI)
				}
				logx.WithContext(ctx).Infow("eino loader start", fields(info, logx.Field("source_bytes", sourceBytes))...)
				return markStart(ctx)
			},
			OnEnd: func(ctx context.Context, info *callbacks.RunInfo, output *document.LoaderCallbackOutput) context.Context {
				extra := []logx.LogField{logx.Field("duration_ms", durationMS(ctx))}
				if output != nil {
					extra = append(extra, logx.Field("docs", len(output.Docs)))
				}
				logx.WithContext(ctx).Infow("eino loader end", fields(info, extra...)...)
				return ctx
			},
			OnError: onError("eino loader error"),
		}).
		Handler()
}

// startKey 是耗时统计的 ctx key。每个组件的 OnStart 塞入自己的开始时间，
// 对应 OnEnd/OnError 从同一条 ctx 链取出；嵌套组件各自遮蔽，互不干扰。
type startKey struct{}

// markStart 在 ctx 中记录组件开始时间。
func markStart(ctx context.Context) context.Context {
	return context.WithValue(ctx, startKey{}, time.Now())
}

// durationMS 返回距 markStart 的毫秒数；ctx 中无标记时返回 -1。
func durationMS(ctx context.Context) int64 {
	if t, ok := ctx.Value(startKey{}).(time.Time); ok {
		return time.Since(t).Milliseconds()
	}
	return -1
}

// fields 组装组件元信息与附加字段。
func fields(info *callbacks.RunInfo, extra ...logx.LogField) []logx.LogField {
	out := make([]logx.LogField, 0, len(extra)+3)
	if info != nil {
		out = append(out,
			logx.Field("eino_component", safety.Label(string(info.Component))),
			logx.Field("eino_type", safety.Label(info.Type)),
			logx.Field("eino_name", safety.Label(info.Name)),
		)
	}
	return append(out, extra...)
}

// onError 构建统一的错误回调。
func onError(msg string) func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
	return func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
		logx.WithContext(ctx).Errorw(msg, fields(info,
			logx.Field("duration_ms", durationMS(ctx)),
			logx.Field("error_code", safety.ErrorCode(err)),
		)...)
		return ctx
	}
}

// scoreRange 返回检索结果的分数区间。
func scoreRange(output *retriever.CallbackOutput) (lo, hi float64, ok bool) {
	for _, doc := range output.Docs {
		if doc == nil {
			continue
		}
		score := doc.Score()
		if math.IsNaN(score) || math.IsInf(score, 0) {
			continue
		}
		if !ok {
			lo, hi = score, score
			ok = true
			continue
		}
		if score < lo {
			lo = score
		}
		if score > hi {
			hi = score
		}
	}
	return lo, hi, ok
}
