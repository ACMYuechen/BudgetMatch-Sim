// Code scaffolded by goctl. No recover, Safe to edit.

package agent

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/infra/request"
	"budgetmatch-sim/services/rpc/agent/client/recommendservice"

	"github.com/zeromicro/go-zero/core/logx"
)

// AgentRecommendLogic 负责普通 HTTP 推荐请求的鉴权和 RPC 转发。
type AgentRecommendLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewAgentRecommendLogic 创建非流式 Agent 推荐逻辑。
func NewAgentRecommendLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AgentRecommendLogic {
	return &AgentRecommendLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AgentRecommend 将 HTTP 请求转换为 RPC 请求，并返回完整推荐结果。
func (l *AgentRecommendLogic) AgentRecommend(req *types.AgentRecommendReq) (resp *types.AgentRecommendResp, err error) {
	// 在网关先确认登录状态；RPC 服务会再次从认证拦截器上下文读取用户身份。
	_, err = request.MustUserId(l.ctx)
	if err != nil {
		l.Logger.Errorf("return error: %v", err)
		return nil, err
	}
	rpcResp, err := l.svcCtx.AgentClient.Recommend(l.ctx, &recommendservice.RecommendReq{
		Query:          req.Query,
		BudgetCents:    req.BudgetCents,
		MaxItems:       int32(req.MaxItems),
		ConversationId: req.ConversationId,
		TurnId:         req.TurnId,
	})
	if err != nil {
		l.Logger.Errorf("return error: %v", err)
		return nil, err
	}

	return mapRecommendResp(rpcResp), nil
}

// mapRecommendResp 将 protobuf 推荐结果转换为 HTTP API 响应类型。
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
		Items:             items,
		TotalPriceCents:   resp.GetTotalPriceCents(),
		Summary:           resp.GetSummary(),
		ToolsUsed:         toolsUsed,
		ConversationId:    resp.GetConversationId(),
		ConversationTitle: resp.GetConversationTitle(),
		TurnId:            resp.GetTurnId(),
	}
}
