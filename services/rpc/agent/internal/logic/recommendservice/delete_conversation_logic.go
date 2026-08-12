package recommendservicelogic

import (
	"context"

	"budgetmatch-sim/services/rpc/agent/internal/svc"
	"budgetmatch-sim/services/rpc/agent/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

// DeleteConversationLogic 处理当前认证用户的会话删除 RPC。
type DeleteConversationLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

// NewDeleteConversationLogic 创建会话删除 RPC 逻辑。
func NewDeleteConversationLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteConversationLogic {
	return &DeleteConversationLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// DeleteConversation 删除认证用户拥有的会话；用户身份不会从请求体读取。
func (l *DeleteConversationLogic) DeleteConversation(in *pb.DeleteConversationReq) (*pb.DeleteConversationResp, error) {
	userId, err := authenticatedUserId(l.ctx)
	if err != nil {
		l.Logger.Errorf("return error: %v", err)
		return nil, err
	}
	deleted, err := l.svcCtx.RecommendService.DeleteConversation(l.ctx, userId, in.ConversationId)
	if err != nil {
		l.Logger.Errorf("return error: %v", err)
		return nil, err
	}
	return &pb.DeleteConversationResp{Deleted: deleted}, nil
}
