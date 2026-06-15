package rocketmq

import "github.com/zeromicro/go-zero/core/logx"

// ProducerService 将 Producer 包装为 go-zero service.Service，用于统一生命周期管理
type ProducerService struct {
	producer Producer
}

// NewProducerService 创建生产者生命周期管理服务
func NewProducerService(p Producer) *ProducerService {
	return &ProducerService{producer: p}
}

// Start 生产者已在 NewProducer 中启动，这里无需重复启动
func (s *ProducerService) Start() {
	logx.Info("rocketmq producer service started")
}

// Stop 关闭生产者
func (s *ProducerService) Stop() {
	if s.producer != nil {
		if err := s.producer.Shutdown(); err != nil {
			logx.Errorf("shutdown rocketmq producer failed: %v", err)
		} else {
			logx.Info("rocketmq producer stopped")
		}
	}
}
