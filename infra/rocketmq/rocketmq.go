package rocketmq

import (
	"context"
	"fmt"
	"time"

	"github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/consumer"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/apache/rocketmq-client-go/v2/producer"
	"github.com/zeromicro/go-zero/core/logx"
)

// Message 通用 RocketMQ 消息
type Message struct {
	Topic string
	Tag   string
	Keys  []string
	Body  []byte
}

// SendResult 发送结果
// 注意：实际底层返回的 primitive.SendResult 包含 MsgId 等字段，
// 这里只暴露业务需要的最小集合，避免上层依赖 primitive 包。
type SendResult struct {
	MsgId string
}

// Producer 通用消息生产者接口
type Producer interface {
	SendSync(ctx context.Context, msg *Message) (*SendResult, error)
	Shutdown() error
}

// Consumer 通用消息消费者接口
type Consumer interface {
	Subscribe(topic, tag string, handler func(ctx context.Context, body []byte) error) error
	Start() error
	Shutdown() error
}

type producerImpl struct {
	p rocketmq.Producer
}

// NewProducer 创建 RocketMQ 生产者
func NewProducer(cfg Config) (Producer, error) {
	if len(cfg.NameServers) == 0 {
		return nil, fmt.Errorf("rocketmq name servers is empty")
	}
	if cfg.GroupName == "" {
		cfg.GroupName = "default-producer-group"
	}
	if cfg.RetryTimes <= 0 {
		cfg.RetryTimes = 2
	}
	if cfg.SendMsgTimeout <= 0 {
		cfg.SendMsgTimeout = 3000
	}

	p, err := rocketmq.NewProducer(
		producer.WithNameServer(cfg.NameServers),
		producer.WithGroupName(cfg.GroupName),
		producer.WithRetry(cfg.RetryTimes),
		producer.WithSendMsgTimeout(time.Duration(cfg.SendMsgTimeout)*time.Millisecond),
	)
	if err != nil {
		return nil, fmt.Errorf("create rocketmq producer failed: %w", err)
	}

	if err := p.Start(); err != nil {
		return nil, fmt.Errorf("start rocketmq producer failed: %w", err)
	}

	return &producerImpl{p: p}, nil
}

func (impl *producerImpl) SendSync(ctx context.Context, msg *Message) (*SendResult, error) {
	if msg == nil {
		return nil, fmt.Errorf("message is nil")
	}

	pm := primitive.NewMessage(msg.Topic, msg.Body)
	if len(msg.Keys) > 0 {
		pm = pm.WithKeys(msg.Keys)
	}
	if msg.Tag != "" {
		pm = pm.WithTag(msg.Tag)
	}

	res, err := impl.p.SendSync(ctx, pm)
	if err != nil {
		return nil, fmt.Errorf("send rocketmq message failed: %w", err)
	}
	if res.Status != primitive.SendOK {
		return nil, fmt.Errorf("send rocketmq message status not ok: %d", res.Status)
	}

	logx.WithContext(ctx).Infof("rocketmq send success: topic=%s, msg_id=%s", msg.Topic, res.MsgID)
	return &SendResult{MsgId: res.MsgID}, nil
}

func (impl *producerImpl) Shutdown() error {
	if impl.p != nil {
		return impl.p.Shutdown()
	}
	return nil
}

type consumerImpl struct {
	c rocketmq.PushConsumer
}

// NewConsumer 创建 RocketMQ 消费者
func NewConsumer(cfg Config) (Consumer, error) {
	if len(cfg.NameServers) == 0 {
		return nil, fmt.Errorf("rocketmq name servers is empty")
	}
	if cfg.GroupName == "" {
		cfg.GroupName = "default-consumer-group"
	}

	c, err := rocketmq.NewPushConsumer(
		consumer.WithNameServer(cfg.NameServers),
		consumer.WithGroupName(cfg.GroupName),
		consumer.WithConsumeTimeout(5*time.Minute),
	)
	if err != nil {
		return nil, fmt.Errorf("create rocketmq consumer failed: %w", err)
	}

	return &consumerImpl{c: c}, nil
}

func (impl *consumerImpl) Subscribe(topic, tag string, handler func(ctx context.Context, body []byte) error) error {
	selector := consumer.MessageSelector{}
	if tag != "" {
		selector = consumer.MessageSelector{Type: consumer.TAG, Expression: tag}
	}

	err := impl.c.Subscribe(topic, selector, func(ctx context.Context, msgs ...*primitive.MessageExt) (consumer.ConsumeResult, error) {
		for _, msg := range msgs {
			logx.Infof("rocketmq consume: topic=%s, msg_id=%s, body_size=%d", topic, msg.MsgId, len(msg.Body))
			if err := handler(ctx, msg.Body); err != nil {
				logx.Errorf("rocketmq consume handler failed: %v", err)
				return consumer.ConsumeRetryLater, err
			}
		}
		return consumer.ConsumeSuccess, nil
	})
	if err != nil {
		return fmt.Errorf("subscribe topic %s failed: %w", topic, err)
	}
	return nil
}

func (impl *consumerImpl) Start() error {
	if err := impl.c.Start(); err != nil {
		return fmt.Errorf("start rocketmq consumer failed: %w", err)
	}
	return nil
}

func (impl *consumerImpl) Shutdown() error {
	if impl.c != nil {
		return impl.c.Shutdown()
	}
	return nil
}

// Ensure interfaces are implemented
var (
	_ Producer = (*producerImpl)(nil)
	_ Consumer = (*consumerImpl)(nil)
)
