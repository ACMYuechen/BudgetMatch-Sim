// Code scaffolded by goctl. No recover, Safe to edit.

package agent

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/services/rpc/agent/client/recommendservice"

	"github.com/zeromicro/go-zero/core/logx"
)

type AgentRecommendLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Agent 推荐
func NewAgentRecommendLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AgentRecommendLogic {
	return &AgentRecommendLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AgentRecommendLogic) AgentRecommend(req *types.AgentRecommendReq) (resp *types.AgentRecommendResp, err error) {
	rpcResp, err := l.svcCtx.AgentClient.Recommend(l.ctx, &recommendservice.RecommendReq{
		Query:       req.Query,
		BudgetCents: req.BudgetCents,
		MaxItems:    int32(req.MaxItems),
	})
	if err != nil {
		return nil, err
	}

	return mapRecommendResp(rpcResp), nil
}

func mapRecommendResp(resp *recommendservice.RecommendResp) *types.AgentRecommendResp {
	if resp == nil {
		return &types.AgentRecommendResp{}
	}

	items := make([]types.AgentBundleItem, 0, len(resp.Items))
	for _, item := range resp.Items {
		items = append(items, types.AgentBundleItem{
			Id:         item.GetId(),
			Name:       item.GetName(),
			Category:   item.GetCategory(),
			Source:     item.GetSource(),
			PriceCents: item.GetPriceCents(),
			Stock:      item.GetStock(),
			Score:      item.GetScore(),
			Reason:     item.GetReason(),
		})
	}

	toolsUsed := make([]types.AgentToolCall, 0, len(resp.ToolsUsed))
	for _, tool := range resp.ToolsUsed {
		toolsUsed = append(toolsUsed, types.AgentToolCall{
			Name:    tool.GetName(),
			Success: tool.GetSuccess(),
			Detail:  tool.GetDetail(),
		})
	}

	intent := resp.GetIntent()
	return &types.AgentRecommendResp{
		Intent: types.AgentIntent{
			BudgetCents: intent.GetBudgetCents(),
			MaxItems:    intent.GetMaxItems(),
			Keywords:    intent.GetKeywords(),
			Preferences: intent.GetPreferences(),
		},
		Items:           items,
		TotalPriceCents: resp.GetTotalPriceCents(),
		Summary:         resp.GetSummary(),
		ToolsUsed:       toolsUsed,
	}
}
