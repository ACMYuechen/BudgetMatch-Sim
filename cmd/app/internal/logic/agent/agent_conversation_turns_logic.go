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

// AgentConversationTurnsLogic 负责读取指定会话的完整推荐轮次。
type AgentConversationTurnsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewAgentConversationTurnsLogic 创建 Agent 会话轮次查询逻辑。
func NewAgentConversationTurnsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AgentConversationTurnsLogic {
	return &AgentConversationTurnsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AgentConversationTurns 分页查询轮次，并保留会话当前结构化状态供前端展示。
func (l *AgentConversationTurnsLogic) AgentConversationTurns(req *types.AgentConversationTurnsReq) (resp *types.AgentConversationTurnsResp, err error) {
	if _, err = request.MustUserId(l.ctx); err != nil {
		l.Logger.Errorf("return error: %v", err)
		return nil, err
	}
	rpcResp, err := l.svcCtx.AgentClient.ListConversationTurns(l.ctx, &recommendservice.ListConversationTurnsReq{
		ConversationId: req.ConversationId, Page: int32(req.Page), PageSize: int32(req.PageSize),
	})
	if err != nil {
		l.Logger.Errorf("return error: %v", err)
		return nil, err
	}
	resp = &types.AgentConversationTurnsResp{Conversation: mapConversationSummary(rpcResp.Conversation),
		List: make([]types.AgentConversationTurn, 0, len(rpcResp.List)), Page: int(rpcResp.Page), PageSize: int(rpcResp.PageSize), Total: rpcResp.Total}
	for _, item := range rpcResp.List {
		resp.List = append(resp.List, mapConversationTurn(item))
	}
	return resp, nil
}
