package recommendservicelogic

import (
	"context"
	"encoding/json"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	recommendagent "budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	recommendeino "budgetmatch-sim/services/rpc/agent/internal/agent/recommend/eino"
	recommendtoolkit "budgetmatch-sim/services/rpc/agent/internal/agent/recommend/toolkit"
	"budgetmatch-sim/services/rpc/agent/internal/svc"
	"budgetmatch-sim/services/rpc/agent/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

// RecommendLogic 推荐服务逻辑层，处理 Recommend RPC 请求。
// 先调用确定性推荐 Agent 生成草稿结果，再根据运行时开关决定是否启用 Eino 运行时进行二次优化。
type RecommendLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

// NewRecommendLogic 创建 RecommendLogic 实例，绑定上下文与服务上下文。
func NewRecommendLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RecommendLogic {
	return &RecommendLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// Recommend 处理推荐 RPC 请求。
// 流程：1) 从服务上下文中获取推荐 Agent；2) 运行 Agent 生成草稿结果；
// 3) 若运行时开关开启，则调用 Eino 运行时进行二次优化；4) 将结果转换为 protobuf 响应返回。
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

	// 若推荐运行时开关开启，则对草稿结果进行二次优化
	if l.svcCtx.RecommendRuntimeEnabled() {
		result = l.runRecommendRuntime(in, result)
	}

	return toPB(result), nil
}

// runRecommendRuntime 调用 Eino 运行时推荐流程，对确定性 Agent 生成的草稿结果进行优化。
// 若运行时调用失败，则回退到草稿结果，并记录错误日志。
func (l *RecommendLogic) runRecommendRuntime(in *pb.RecommendReq, draft *agentcore.Result) *agentcore.Result {
	flowResult, err := l.svcCtx.RecommendRunner.Run(l.ctx, recommendRuntimeInput(in, draft))
	if err != nil {
		l.Errorf("recommend eino runtime failed, fallback to deterministic result: %v", err)
		draft.ToolsUsed = append(draft.ToolsUsed, agentcore.ToolCall{
			Name:    "llm." + l.svcCtx.RecommendRunner.Name(),
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
		Name:    "llm." + l.svcCtx.RecommendRunner.Name(),
		Success: true,
		Detail:  "completed eino function calling flow",
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

// recommendRuntimeInput 将 protobuf 请求与草稿结果组装为 Eino 运行时的输入参数。
func recommendRuntimeInput(in *pb.RecommendReq, draft *agentcore.Result) recommendeino.RunInput {
	return recommendeino.RunInput{
		Query:           in.Query,
		Intent:          draft.Intent,
		SelectedItems:   draft.Items,
		TotalPriceCents: draft.TotalPriceCents,
		ToolsUsed:       draft.ToolsUsed,
	}
}

// cloneAgentResult 深拷贝 agentcore.Result，避免运行时修改影响原始草稿结果。
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

// applySelectedBundle 从工具调用结果中解析选中的 bundle，并更新到结果中。
func applySelectedBundle(result *agentcore.Result, raw json.RawMessage) {
	var selected recommendtoolkit.SelectBundleResult
	if err := json.Unmarshal(raw, &selected); err != nil || len(selected.Items) == 0 {
		return
	}
	result.Items = selected.Items
	result.TotalPriceCents = selected.TotalPriceCents
}

// toolDetail 提取工具调用的详细结果信息，优先返回错误信息，其次返回 JSON 内容。
func toolDetail(tool recommendeino.ToolResult) string {
	if tool.Error != "" {
		return tool.Error
	}
	if len(tool.JSON) == 0 {
		return "completed"
	}
	return string(tool.JSON)
}

// toPB 将 agentcore.Result 转换为 protobuf 的 RecommendResp 响应。
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
