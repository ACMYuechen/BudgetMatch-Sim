package recommendservicelogic

import (
	"context"
	"encoding/json"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	recommendagent "budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	recommendflow "budgetmatch-sim/services/rpc/agent/internal/agent/recommend/flow"
	recommendprompt "budgetmatch-sim/services/rpc/agent/internal/agent/recommend/prompt"
	recommendtoolkit "budgetmatch-sim/services/rpc/agent/internal/agent/recommend/toolkit"
	"budgetmatch-sim/services/rpc/agent/internal/svc"
	"budgetmatch-sim/services/rpc/agent/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type RecommendLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRecommendLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RecommendLogic {
	return &RecommendLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *RecommendLogic) Recommend(in *pb.RecommendReq) (*pb.RecommendResp, error) {
	runner, ok := l.svcCtx.Agents.Get(recommendagent.AgentName)
	if !ok {
		return nil, agentcore.ErrAgentNotFound
	}

	result, err := runner.Run(l.ctx, agentcore.Input{
		Query:       in.Query,
		BudgetCents: in.BudgetCents,
		MaxItems:    in.MaxItems,
		UserID:      in.UserId,
	})
	if err != nil {
		return nil, err
	}

	if l.svcCtx.RecommendFlowEnabled() {
		result = l.runRecommendFlow(in, result)
	}

	return toPB(result), nil
}

func (l *RecommendLogic) runRecommendFlow(in *pb.RecommendReq, draft *agentcore.Result) *agentcore.Result {
	flowResult, err := l.svcCtx.RecommendFlow.Run(l.ctx, recommendflowInput(in, draft, l.svcCtx))
	if err != nil {
		l.Errorf("recommend flow failed, fallback to deterministic result: %v", err)
		draft.ToolsUsed = append(draft.ToolsUsed, agentcore.ToolCall{
			Name:    "llm." + l.svcCtx.ModelClient.Name(),
			Success: false,
			Detail:  err.Error(),
		})
		return draft
	}

	out := cloneAgentResult(draft)
	if flowResult.FinalText != "" {
		out.Summary = flowResult.FinalText
	}
	out.ToolsUsed = append(out.ToolsUsed, agentcore.ToolCall{
		Name:    "llm." + l.svcCtx.ModelClient.Name(),
		Success: true,
		Detail:  "completed function calling flow",
	})
	for _, tool := range flowResult.ToolResults {
		out.ToolsUsed = append(out.ToolsUsed, agentcore.ToolCall{
			Name:    "tool." + tool.Name,
			Success: tool.Error == "",
			Detail:  toolDetail(tool),
		})
		if tool.Name == recommendtoolkit.ToolSelectBundle && tool.Error == "" {
			applySelectedBundle(out, tool.JSON)
		}
	}
	return out
}

func recommendflowInput(in *pb.RecommendReq, draft *agentcore.Result, svcCtx *svc.ServiceContext) recommendflow.RunInput {
	return recommendflow.RunInput{
		Messages: svcCtx.PromptBuilder.Build(recommendprompt.BuildInput{
			Query:           in.Query,
			Intent:          draft.Intent,
			SelectedItems:   draft.Items,
			TotalPriceCents: draft.TotalPriceCents,
			ToolsUsed:       draft.ToolsUsed,
		}),
		Tools: svcCtx.PromptBuilder.FunctionTools(),
	}
}

func cloneAgentResult(result *agentcore.Result) *agentcore.Result {
	if result == nil {
		return &agentcore.Result{}
	}
	return &agentcore.Result{
		Intent: agentcore.Intent{
			BudgetCents: result.Intent.BudgetCents,
			MaxItems:    result.Intent.MaxItems,
			Keywords:    append([]string(nil), result.Intent.Keywords...),
			Preferences: append([]string(nil), result.Intent.Preferences...),
		},
		Items:           append([]agentcore.BundleItem(nil), result.Items...),
		TotalPriceCents: result.TotalPriceCents,
		Summary:         result.Summary,
		ToolsUsed:       append([]agentcore.ToolCall(nil), result.ToolsUsed...),
	}
}

func applySelectedBundle(result *agentcore.Result, raw json.RawMessage) {
	var selected recommendtoolkit.SelectBundleResult
	if err := json.Unmarshal(raw, &selected); err != nil || len(selected.Items) == 0 {
		return
	}
	result.Items = selected.Items
	result.TotalPriceCents = selected.TotalPriceCents
}

func toolDetail(tool recommendflow.ToolResult) string {
	if tool.Error != "" {
		return tool.Error
	}
	if len(tool.JSON) == 0 {
		return "completed"
	}
	return string(tool.JSON)
}

func toPB(result *agentcore.Result) *pb.RecommendResp {
	resp := &pb.RecommendResp{
		Intent: &pb.Intent{
			BudgetCents: result.Intent.BudgetCents,
			MaxItems:    result.Intent.MaxItems,
			Keywords:    result.Intent.Keywords,
			Preferences: result.Intent.Preferences,
		},
		Items:           make([]*pb.BundleItem, 0, len(result.Items)),
		TotalPriceCents: result.TotalPriceCents,
		Summary:         result.Summary,
		ToolsUsed:       make([]*pb.ToolCall, 0, len(result.ToolsUsed)),
	}

	for _, item := range result.Items {
		resp.Items = append(resp.Items, &pb.BundleItem{
			Id:         item.ID,
			Name:       item.Name,
			Category:   item.Category,
			Source:     item.Source,
			PriceCents: item.PriceCents,
			Stock:      item.Stock,
			Score:      item.Score,
			Reason:     item.Reason,
		})
	}

	for _, tool := range result.ToolsUsed {
		resp.ToolsUsed = append(resp.ToolsUsed, &pb.ToolCall{
			Name:    tool.Name,
			Success: tool.Success,
			Detail:  tool.Detail,
		})
	}

	return resp
}
