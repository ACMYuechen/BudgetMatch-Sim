package recommendservicelogic

import (
	"context"

	"budgetmatch-sim/services/rpc/agent/internal/svc"
	"budgetmatch-sim/services/rpc/agent/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

// ListConversationsLogic 处理当前认证用户的会话列表 RPC。
type ListConversationsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

// NewListConversationsLogic 创建会话列表 RPC 逻辑。
func NewListConversationsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListConversationsLogic {
	return &ListConversationsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// ListConversations 分页返回认证用户的会话摘要，不接受客户端指定 user_id。
func (l *ListConversationsLogic) ListConversations(in *pb.ListConversationsReq) (*pb.ListConversationsResp, error) {
	userId, err := authenticatedUserId(l.ctx)
	if err != nil {
		l.Logger.Errorf("return error: %v", err)
		return nil, err
	}
	items, total, err := l.svcCtx.RecommendService.ListConversations(l.ctx, userId, int(in.Page), int(in.PageSize))
	if err != nil {
		l.Logger.Errorf("return error: %v", err)
		return nil, err
	}
	page, pageSize := normalizePBPage(in.Page, in.PageSize, 20)
	resp := &pb.ListConversationsResp{List: make([]*pb.ConversationSummary, 0, len(items)), Page: page, PageSize: pageSize, Total: total}
	for _, item := range items {
		resp.List = append(resp.List, toPBConversation(item))
	}
	return resp, nil
}
