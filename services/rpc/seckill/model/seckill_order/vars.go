package seckill_order

// Status 秒杀订单状态
const (
	StatusQueued   int64 = 0 // 排队中
	StatusSuccess  int64 = 1 // 成功
	StatusFailed   int64 = 2 // 失败
	StatusPaid     int64 = 3 // 已支付
	StatusClosed   int64 = 4 // 已关闭
)
