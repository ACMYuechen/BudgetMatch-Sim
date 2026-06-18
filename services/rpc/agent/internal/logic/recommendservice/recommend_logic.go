package recommendservicelogic

import (
	"context"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	recommendagent "budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
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

	return toPB(result), nil
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
