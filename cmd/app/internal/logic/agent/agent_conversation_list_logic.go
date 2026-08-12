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

// AgentConversationListLogic 负责读取当前登录用户可见的 Agent 会话。
type AgentConversationListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewAgentConversationListLogic 创建 Agent 会话列表逻辑。
func NewAgentConversationListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AgentConversationListLogic {
	return &AgentConversationListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AgentConversationList 分页查询会话并把 RPC 类型转换为网关响应类型。
func (l *AgentConversationListLogic) AgentConversationList(req *types.AgentConversationListReq) (resp *types.AgentConversationListResp, err error) {
	if _, err = request.MustUserId(l.ctx); err != nil {
		l.Logger.Errorf("return error: %v", err)
		return nil, err
	}
	rpcResp, err := l.svcCtx.AgentClient.ListConversations(l.ctx, &recommendservice.ListConversationsReq{Page: int32(req.Page), PageSize: int32(req.PageSize)})
	if err != nil {
		l.Logger.Errorf("return error: %v", err)
		return nil, err
	}
	resp = &types.AgentConversationListResp{List: make([]types.AgentConversationSummary, 0, len(rpcResp.List)), Page: int(rpcResp.Page), PageSize: int(rpcResp.PageSize), Total: rpcResp.Total}
	for _, item := range rpcResp.List {
		resp.List = append(resp.List, mapConversationSummary(item))
	}
	return resp, nil
}
