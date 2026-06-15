package errors

import "errors"

var (
	// 系统级错误
	ErrInternal = errors.New("internal server error")
	// 数据库相关错误
	ErrDatabase = errors.New("database error")
	// 用户相关错误
	ErrUserNotFound    = errors.New("user not found")
	ErrUserExists      = errors.New("user already exists")
	ErrInvalidPassword = errors.New("invalid password")
	// Token相关错误
	ErrTokenGeneration = errors.New("failed to generate token")
	ErrInvalidToken    = errors.New("invalid token")
	// 权限相关错误
	ErrUnauthorized = errors.New("unauthorized")
	// 限流相关错误
	ErrTooManyRequests = errors.New("too many requests, please try again later")
	ErrLimitExceeded   = errors.New("request limit exceeded, please try again later")
	// 验证码相关错误
	ErrCodeInvalid = errors.New("invalid verification code")
	ErrCodeExpired = errors.New("verification code expired")
	// 邮箱相关错误
	ErrInvalidEmail    = errors.New("invalid email address")
	ErrEmailSendFailed = errors.New("failed to send email")
	// 秒杀相关错误
	ErrSeckillActivityNotFound = errors.New("seckill activity not found")
	ErrSeckillSkuNotFound      = errors.New("seckill sku not found")
	ErrSeckillOrderNotFound    = errors.New("seckill order not found")
	ErrSeckillStockNotEnough   = errors.New("seckill stock not enough")
	ErrSeckillActivityNotStart = errors.New("seckill activity not started yet")
	ErrSeckillActivityEnded    = errors.New("seckill activity has ended")
	ErrSeckillTokenInvalid     = errors.New("seckill token invalid or expired")
	ErrSeckillAlreadyPurchased = errors.New("already purchased this seckill item")
	ErrSeckillSubmitFailed     = errors.New("seckill order submit failed")
	// 商城相关错误
	ErrMallProductNotFound        = errors.New("mall product not found")
	ErrMallSkuNotFound            = errors.New("mall sku not found")
	ErrMallOrderNotFound          = errors.New("mall order not found")
	ErrMallStockNotEnough         = errors.New("mall stock not enough")
	ErrMallInvalidOrderTransition = errors.New("invalid order status transition")
	ErrMallOrderCannotCancel      = errors.New("order cannot be cancelled")
	ErrMallDuplicateOrder         = errors.New("duplicate order request")
)
