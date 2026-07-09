package consumer

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
	"gorm.io/gorm"

	"budgetmatch-sim/services/rpc/seckill/internal/stock"
	"budgetmatch-sim/services/rpc/seckill/model/seckill_order"
	"budgetmatch-sim/services/rpc/seckill/model/seckill_sku"
)

const (
	streamKey     = "seckill:order:stream"
	consumerGroup = "seckill-order-group"
	blockTimeout  = 5 * time.Second
	claimInterval = 30 * time.Second
	claimMinIdle  = 60 * time.Second
)

type OrderMessage struct {
	OrderID    string
	ActivityID string
	SkuID      string
	UserID     string
	Quantity   int64
	TotalAmt   int64
}

type OrderConsumer struct {
	redis        redis.UniversalClient
	db           *gorm.DB
	orderStore   seckill_order.SeckillOrdersModel
	skuStore     seckill_sku.SeckillSkusModel
	stockManager *stock.StockManager
	consumerName string
	quit         chan struct{}
}

func NewOrderConsumer(r redis.UniversalClient, db *gorm.DB,
	orderStore seckill_order.SeckillOrdersModel,
	skuStore seckill_sku.SeckillSkusModel,
	stockManager *stock.StockManager) *OrderConsumer {

	hostname, _ := os.Hostname()
	pid := os.Getpid()
	consumerName := fmt.Sprintf("%s-%d", hostname, pid)
	if hostname == "" {
		consumerName = fmt.Sprintf("consumer-%d", pid)
	}

	return &OrderConsumer{
		redis:        r,
		db:           db,
		orderStore:   orderStore,
		skuStore:     skuStore,
		stockManager: stockManager,
		consumerName: consumerName,
		quit:         make(chan struct{}),
	}
}

func (c *OrderConsumer) Start() {
	// create consumer group if not exists
	err := c.redis.XGroupCreateMkStream(context.Background(), streamKey, consumerGroup, "$").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		logx.Errorf("failed to create consumer group: %v", err)
		return
	}

	logx.Infof("order consumer started: group=%s, consumer=%s", consumerGroup, c.consumerName)

	// start claim loop for pending messages
	go c.claimLoop()

	// main read loop
	for {
		select {
		case <-c.quit:
			return
		default:
		}

		streams, err := c.redis.XReadGroup(context.Background(), &redis.XReadGroupArgs{
			Group:    consumerGroup,
			Consumer: c.consumerName,
			Streams:  []string{streamKey, ">"},
			Count:    10,
			Block:    blockTimeout,
		}).Result()

		if err == redis.Nil {
			continue
		}
		if err != nil {
			logx.Errorf("xreadgroup error: %v", err)
			time.Sleep(time.Second)
			continue
		}

		for _, stream := range streams {
			for _, msg := range stream.Messages {
				c.processMessage(msg)
			}
		}
	}
}

func (c *OrderConsumer) Stop() {
	close(c.quit)
	logx.Info("order consumer stopped")
}

func (c *OrderConsumer) claimLoop() {
	ticker := time.NewTicker(claimInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.quit:
			return
		case <-ticker.C:
			c.claimPending()
		}
	}
}

func (c *OrderConsumer) claimPending() {
	// claim pending messages from other consumers that have been idle for a while
	pending, err := c.redis.XPendingExt(context.Background(), &redis.XPendingExtArgs{
		Stream: streamKey,
		Group:  consumerGroup,
		Start:  "-",
		End:    "+",
		Count:  10,
	}).Result()
	if err != nil {
		return
	}

	// 简单处理, 每次判10条, 不符合就跳过
	for _, p := range pending {
		if time.Duration(p.Idle)*time.Millisecond < claimMinIdle {
			continue
		}
		msgs, err := c.redis.XClaim(context.Background(), &redis.XClaimArgs{
			Stream:   streamKey,
			Group:    consumerGroup,
			Consumer: c.consumerName,
			MinIdle:  claimMinIdle,
			Messages: []string{p.ID},
		}).Result()
		if err != nil {
			continue
		}
		for _, msg := range msgs {
			c.processMessage(msg)
		}
	}
}

func (c *OrderConsumer) processMessage(msg redis.XMessage) {
	orderMsg := parseMessage(msg)
	if orderMsg == nil {
		// 无法解析的脏消息：直接 ack 丢弃，避免无限重投
		logx.Errorf("drop unparseable order message: %s", msg.ID)
		c.ack(msg.ID)
		return
	}

	logx.Infof("processing order: %s, activity=%s, sku=%s, user=%s, qty=%d",
		orderMsg.OrderID, orderMsg.ActivityID, orderMsg.SkuID, orderMsg.UserID, orderMsg.Quantity)

	// start DB transaction
	err := c.db.Transaction(func(tx *gorm.DB) error {
		// 1. try insert order (status=1 success)
		order := &seckill_order.SeckillOrders{
			Id:          orderMsg.OrderID,
			ActivityId:  orderMsg.ActivityID,
			SkuId:       orderMsg.SkuID,
			UserId:      orderMsg.UserID,
			Quantity:    int(orderMsg.Quantity),
			TotalAmount: orderMsg.TotalAmt,
			Status:      1,
			Snapshot:    "{}",
		}
		if err := tx.Create(order).Error; err != nil {
			if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
				// 订单已存在：说明该消息此前已被成功处理过（重复投递）。
				// 库存已在首次处理时结算，这里绝不能再回滚，否则会把已扣库存凭空加回导致超卖。
				logx.Infof("duplicate order, already processed, skip: %s", orderMsg.OrderID)
				return nil // ack
			}
			return err
		}

		// 2. update sku: sold += qty, lock_stock -= qty, check sold+qty <= stock
		result := tx.Exec(
			"UPDATE seckill_skus SET sold = sold + ?, lock_stock = lock_stock - ?, updated_at = ? WHERE id = ? AND sold + ? <= stock",
			orderMsg.Quantity, orderMsg.Quantity, time.Now(), orderMsg.SkuID, orderMsg.Quantity,
		)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			// stock constraint failed -> rollback redis stock, mark order as failed
			c.stockManager.Rollback(orderMsg.ActivityID, orderMsg.SkuID, orderMsg.Quantity)
			// mark order status = 2 (failed)
			tx.Model(&seckill_order.SeckillOrders{}).Where("id = ?", orderMsg.OrderID).Update("status", 2)
			return nil // ack
		}

		return nil
	})

	if err != nil {
		logx.Errorf("order processing failed: %s, error: %v", orderMsg.OrderID, err)
		// do not ack on error; message will be redelivered
		return
	}

	c.ack(msg.ID)
	logx.Infof("order processed successfully: %s", orderMsg.OrderID)
}

func (c *OrderConsumer) ack(msgID string) {
	if err := c.redis.XAck(context.Background(), streamKey, consumerGroup, msgID).Err(); err != nil {
		// ack 失败会导致消息后续被重新投递；依赖订单唯一键幂等避免重复落单
		logx.Errorf("failed to ack message %s: %v", msgID, err)
	}
}

func parseMessage(msg redis.XMessage) *OrderMessage {
	m := &OrderMessage{}
	for k, v := range msg.Values {
		switch k {
		case "order_id":
			m.OrderID = fmt.Sprintf("%v", v)
		case "activity_id":
			m.ActivityID = fmt.Sprintf("%v", v)
		case "sku_id":
			m.SkuID = fmt.Sprintf("%v", v)
		case "user_id":
			m.UserID = fmt.Sprintf("%v", v)
		case "quantity":
			n, err := strconv.ParseInt(fmt.Sprintf("%v", v), 10, 64)
			if err != nil {
				logx.Errorf("invalid quantity in order message: %v", v)
				return nil
			}
			m.Quantity = n
		case "total_amount":
			n, err := strconv.ParseInt(fmt.Sprintf("%v", v), 10, 64)
			if err != nil {
				logx.Errorf("invalid total_amount in order message: %v", v)
				return nil
			}
			m.TotalAmt = n
		}
	}
	if m.OrderID == "" || m.ActivityID == "" || m.SkuID == "" || m.UserID == "" {
		return nil
	}
	if m.Quantity <= 0 {
		logx.Errorf("invalid order message, non-positive quantity: order=%s qty=%d", m.OrderID, m.Quantity)
		return nil
	}
	return m
}

// Ensure OrderConsumer implements service.Service
var _ service.Service = (*OrderConsumer)(nil)
