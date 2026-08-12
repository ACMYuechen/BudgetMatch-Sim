package recommendservicelogic

import (
	"context"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/agent/internal/svc"
	"budgetmatch-sim/services/rpc/agent/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

// ListConversationTurnsLogic 处理会话历史轮次查询 RPC。
type ListConversationTurnsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

// NewListConversationTurnsLogic 创建会话轮次查询 RPC 逻辑。
func NewListConversationTurnsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListConversationTurnsLogic {
	return &ListConversationTurnsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// ListConversationTurns 只查询认证用户范围内的会话，并将不存在统一映射为 NotFound。
func (l *ListConversationTurnsLogic) ListConversationTurns(in *pb.ListConversationTurnsReq) (*pb.ListConversationTurnsResp, error) {
	userId, err := authenticatedUserId(l.ctx)
	if err != nil {
		l.Logger.Errorf("return error: %v", err)
		return nil, err
	}
	conversation, turns, total, exists, err := l.svcCtx.RecommendService.ListTurns(l.ctx, userId, in.ConversationId, int(in.Page), int(in.PageSize))
	if err != nil {
		l.Logger.Errorf("return error: %v", err)
		return nil, err
	}
	if !exists {
		l.Logger.Errorf("return error: %v", errors.NotFound)
		return nil, errors.NotFound
	}
	page, pageSize := normalizePBPage(in.Page, in.PageSize, 50)
	resp := &pb.ListConversationTurnsResp{Conversation: toPBConversation(conversation), List: make([]*pb.ConversationTurn, 0, len(turns)), Page: page, PageSize: pageSize, Total: total}
	for _, turn := range turns {
		item, err := toPBTurn(turn)
		if err != nil {
			l.Logger.Errorf("return error: %v", err)
			return nil, err
		}
		resp.List = append(resp.List, item)
	}
	return resp, nil
}
