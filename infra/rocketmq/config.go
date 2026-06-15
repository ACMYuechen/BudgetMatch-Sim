package rocketmq

// Config RocketMQ 客户端配置
type Config struct {
	NameServers    []string `json:"nameServers"`    // NameServer 地址列表
	GroupName      string   `json:"groupName"`      // 生产者/消费者组名
	RetryTimes     int      `json:"retryTimes"`     // 发送重试次数
	SendMsgTimeout int      `json:"sendMsgTimeout"` // 发送超时，毫秒
}
