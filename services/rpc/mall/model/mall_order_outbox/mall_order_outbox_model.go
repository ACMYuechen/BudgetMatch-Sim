// Code scaffolded by goctl. Only init once, Safe to edit.
// gorm

package mall_order_outbox

import (
	"context"
	"database/sql"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"budgetmatch-sim/infra/uuid"
)

var _ MallOrderOutboxModel = (*customMallOrderOutboxModel)(nil)

type (
	// MallOrderOutboxModel 原生 DB 接口，不走缓存
	MallOrderOutboxModel interface {
		mallOrderOutboxModel
		// InsertTx 在业务事务中插入待发送事件。
		InsertTx(tx *gorm.DB, data *MallOrderOutbox) error
		// ClaimBatch 使用行锁批量领取可发送事件，并回收处理租约已过期的事件。
		ClaimBatch(ctx context.Context, limit int, now, lockedUntil time.Time) ([]MallOrderOutbox, int64, error)
		// MarkSent 将当前领取版本从发送中流转为已发送。
		MarkSent(ctx context.Context, id string, attempt int, publishedAt time.Time) (bool, error)
		// MarkRetry 将当前领取版本从发送中流转回待发送。
		MarkRetry(ctx context.Context, req *MarkRetryReq) (bool, error)
		// MarkDead 将当前领取版本从发送中流转为死信。
		MarkDead(ctx context.Context, req *MarkDeadReq) (bool, error)
		ListFiltered(ctx context.Context, req ListFilteredReq) ([]MallOrderOutbox, int64, error)
		GetStats(ctx context.Context) ([]StatusCount, time.Time, error)
		ReplayDead(ctx context.Context, id string, now time.Time) (bool, error)
	}

	customMallOrderOutboxModel struct {
		*defaultMallOrderOutboxModel
	}

	MarkRetryReq struct {
		Id          string
		Attempt     int
		NextRetryAt time.Time
		LastError   string
		Now         time.Time
	}

	MarkDeadReq struct {
		Id        string
		Attempt   int
		LastError string
		Now       time.Time
	}

	ListFilteredReq struct {
		Page        int
		Size        int
		Status      int
		EventType   string
		AggregateId string
		DedupKey    string
	}

	StatusCount struct {
		Status    int
		EventType string
		Count     int64
	}
)

// NewMallOrderOutboxModel 创建纯 DB model
func NewMallOrderOutboxModel(conn *gorm.DB) MallOrderOutboxModel {
	return &customMallOrderOutboxModel{
		defaultMallOrderOutboxModel: newMallOrderOutboxModel(conn),
	}
}

func (MallOrderOutbox) TableName() string {
	return "mall_order_outbox"
}

func NewMallOrderOutboxId() string {
	return uuid.NewPrefixedShortUUID("obx-")
}

func (m *customMallOrderOutboxModel) InsertTx(tx *gorm.DB, data *MallOrderOutbox) error {
	return tx.Create(data).Error
}

func (m *customMallOrderOutboxModel) ClaimBatch(ctx context.Context, limit int, now, lockedUntil time.Time) ([]MallOrderOutbox, int64, error) {
	list := make([]MallOrderOutbox, 0)
	var expiredDeadCount int64
	err := m.conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		deadResult := tx.Model(&MallOrderOutbox{}).Where("status = ? AND locked_until <= ? AND attempts >= max_attempts", StatusProcessing, now).Updates(map[string]any{"status": StatusDead, "last_error": "processing lease expired after maximum attempts", "updated_at": now})
		if deadResult.Error != nil {
			return deadResult.Error
		}
		expiredDeadCount = deadResult.RowsAffected

		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("(status = ? AND next_retry_at <= ?) OR (status = ? AND locked_until <= ? AND attempts < max_attempts)", StatusPending, now, StatusProcessing, now).Order("created_at ASC").Limit(limit).Find(&list).Error; err != nil {
			return err
		}
		if len(list) == 0 {
			return nil
		}

		ids := make([]string, 0, len(list))
		for i := range list {
			ids = append(ids, list[i].Id)
		}
		if err := tx.Model(&MallOrderOutbox{}).Where("id IN ?", ids).Updates(map[string]any{"status": StatusProcessing, "attempts": gorm.Expr("attempts + 1"), "locked_until": lockedUntil, "updated_at": now}).Error; err != nil {
			return err
		}
		for i := range list {
			list[i].Status = StatusProcessing
			list[i].Attempts++
			list[i].LockedUntil = lockedUntil
		}
		return nil
	})
	return list, expiredDeadCount, err
}

func (m *customMallOrderOutboxModel) MarkSent(ctx context.Context, id string, attempt int, publishedAt time.Time) (bool, error) {
	result := m.conn.WithContext(ctx).Model(&MallOrderOutbox{}).Where("id = ? AND status = ? AND attempts = ?", id, StatusProcessing, attempt).Updates(map[string]any{"status": StatusSent, "last_error": "", "locked_until": publishedAt, "published_at": publishedAt.Unix(), "updated_at": publishedAt})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (m *customMallOrderOutboxModel) MarkRetry(ctx context.Context, req *MarkRetryReq) (bool, error) {
	result := m.conn.WithContext(ctx).Model(&MallOrderOutbox{}).Where("id = ? AND status = ? AND attempts = ?", req.Id, StatusProcessing, req.Attempt).Updates(map[string]any{"status": StatusPending, "next_retry_at": req.NextRetryAt, "locked_until": req.Now, "last_error": req.LastError, "updated_at": req.Now})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (m *customMallOrderOutboxModel) MarkDead(ctx context.Context, req *MarkDeadReq) (bool, error) {
	result := m.conn.WithContext(ctx).Model(&MallOrderOutbox{}).Where("id = ? AND status = ? AND attempts = ?", req.Id, StatusProcessing, req.Attempt).Updates(map[string]any{"status": StatusDead, "locked_until": req.Now, "last_error": req.LastError, "updated_at": req.Now})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (m *customMallOrderOutboxModel) ListFiltered(ctx context.Context, req ListFilteredReq) ([]MallOrderOutbox, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.Size <= 0 {
		req.Size = 20
	}
	if req.Size > 100 {
		req.Size = 100
	}

	query := m.conn.WithContext(ctx).Model(&MallOrderOutbox{})
	if req.Status >= 0 {
		query = query.Where("status = ?", req.Status)
	}
	if req.EventType != "" {
		query = query.Where("event_type = ?", req.EventType)
	}
	if req.AggregateId != "" {
		query = query.Where("aggregate_id = ?", req.AggregateId)
	}
	if req.DedupKey != "" {
		query = query.Where("dedup_key = ?", req.DedupKey)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	list := make([]MallOrderOutbox, 0)
	if total == 0 {
		return list, 0, nil
	}
	err := query.Order("created_at DESC").Limit(req.Size).Offset((req.Page - 1) * req.Size).Find(&list).Error
	return list, total, err
}

func (m *customMallOrderOutboxModel) GetStats(ctx context.Context) ([]StatusCount, time.Time, error) {
	counts := make([]StatusCount, 0)
	if err := m.conn.WithContext(ctx).Model(&MallOrderOutbox{}).
		Select("status, event_type, COUNT(*) AS count").
		Group("status, event_type").
		Order("status, event_type").
		Scan(&counts).Error; err != nil {
		return nil, time.Time{}, err
	}

	var oldest sql.NullTime
	if err := m.conn.WithContext(ctx).Model(&MallOrderOutbox{}).
		Where("status = ?", StatusPending).
		Select("MIN(created_at)").
		Scan(&oldest).Error; err != nil {
		return nil, time.Time{}, err
	}
	if oldest.Valid {
		return counts, oldest.Time, nil
	}
	return counts, time.Time{}, nil
}

func (m *customMallOrderOutboxModel) ReplayDead(ctx context.Context, id string, now time.Time) (bool, error) {
	result := m.conn.WithContext(ctx).Model(&MallOrderOutbox{}).
		Where("id = ? AND status = ?", id, StatusDead).
		Updates(map[string]any{
			"status":        StatusPending,
			"attempts":      0,
			"next_retry_at": now,
			"locked_until":  now,
			"last_error":    "",
			"published_at":  0,
			"updated_at":    now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}
