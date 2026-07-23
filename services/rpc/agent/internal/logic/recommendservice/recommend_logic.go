package recommendservicelogic

import (
	"context"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/svc"
	"budgetmatch-sim/services/rpc/agent/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

// RecommendLogic 推荐服务逻辑层，处理 Recommend RPC 请求。
// 该层只负责协议入参/出参转换，推荐业务编排由 svcCtx.RecommendService 承接。
type RecommendLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

// NewRecommendLogic 创建 RecommendLogic 实例，绑定请求上下文与服务上下文。
func NewRecommendLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RecommendLogic {
	return &RecommendLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// Recommend 处理推荐 RPC 请求。
// 流程：将 protobuf 请求转换为业务输入，调用推荐业务服务，再将业务结果转换为 protobuf 响应。
func (l *RecommendLogic) Recommend(in *pb.RecommendReq) (*pb.RecommendResp, error) {
	result, err := l.svcCtx.RecommendService.Recommend(l.ctx, agentcore.Input{
		Query:          in.Query,
		BudgetCents:    in.BudgetCents,
		MaxItems:       in.MaxItems,
		UserID:         in.UserId,
		ConversationID: in.ConversationId,
	})
	if err != nil {
		l.Logger.Errorf("return error: %v", err)
		return nil, err
	}

	return toPB(result), nil
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
		ConversationId:  result.ConversationID,
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
