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

// AgentConversationDeleteLogic 负责校验网关身份并转发会话删除请求。
type AgentConversationDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewAgentConversationDeleteLogic 创建 Agent 会话删除逻辑。
func NewAgentConversationDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AgentConversationDeleteLogic {
	return &AgentConversationDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AgentConversationDelete 删除当前登录用户拥有的指定会话及其全部轮次。
func (l *AgentConversationDeleteLogic) AgentConversationDelete(req *types.AgentConversationDeleteReq) (resp *types.AgentConversationDeleteResp, err error) {
	if _, err = request.MustUserId(l.ctx); err != nil {
		l.Logger.Errorf("return error: %v", err)
		return nil, err
	}
	rpcResp, err := l.svcCtx.AgentClient.DeleteConversation(l.ctx, &recommendservice.DeleteConversationReq{ConversationId: req.ConversationId})
	if err != nil {
		l.Logger.Errorf("return error: %v", err)
		return nil, err
	}
	return &types.AgentConversationDeleteResp{Deleted: rpcResp.Deleted}, nil
}
