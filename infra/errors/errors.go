package errors

// 400xxx: 请求参数或用户可修正的输入错误。
const (
	ECInvalid = 400000 + iota
	ECInvalidEmail
	ECCodeInvalid
	ECCodeExpired
	ECSeckillActivityNotStart
	ECMallPaymentAmountMismatch
	ECAgentContextTooLarge
)

// 401xxx: 认证、凭据或令牌错误。
const (
	ECUnauthorized = 401000 + iota
	ECInvalidPassword
	ECInvalidToken
	ECSeckillTokenInvalid
)

// 404xxx: 请求的资源不存在。
const (
	ECNotFound = 404000 + iota
	ECUserNotFound
	ECSeckillActivityNotFound
	ECSeckillSkuNotFound
	ECSeckillOrderNotFound
	ECMallProductNotFound
	ECMallSkuNotFound
	ECMallOrderNotFound
)

// 409xxx: 请求与当前业务状态冲突。
const (
	ECConflict = 409000 + iota
	ECUserExists
	ECSeckillStockNotEnough
	ECSeckillAlreadyPurchased
	ECMallStockNotEnough
	ECMallInvalidOrderTransition
	ECMallOrderCannotCancel
	ECMallDuplicateOrder
	ECMallPaymentConfirmationConflict
)

// 410xxx: 资源曾经存在但已不再可用。
const (
	ECGone = 410000 + iota
	ECSeckillActivityEnded
)

// 429xxx: 限流或滥用防护错误。
const (
	ECTooManyRequests = 429000 + iota
	ECLimitExceeded
)

// 500xxx: 系统内部或依赖服务错误。
const (
	ECInternal = 500000 + iota
	ECDatabase
	ECTokenGeneration
	ECEmailSendFailed
	ECSeckillSubmitFailed
)

var (
	Invalid                   = newAppError(ECInvalid, "invalid.default")
	InvalidEmail              = newAppError(ECInvalidEmail, "invalid.invalid_email")
	CodeInvalid               = newAppError(ECCodeInvalid, "invalid.invalid_verify_code")
	CodeExpired               = newAppError(ECCodeExpired, "invalid.code_expired")
	SeckillActivityNotStart   = newAppError(ECSeckillActivityNotStart, "seckill.activity_not_started")
	MallPaymentAmountMismatch = newAppError(ECMallPaymentAmountMismatch, "mall.payment_amount_mismatch")
	AgentContextTooLarge      = newAppError(ECAgentContextTooLarge, "agent.context_too_large")
)

var (
	Unauthorized        = newAppError(ECUnauthorized, "unauthorized.default")
	InvalidPassword     = newAppError(ECInvalidPassword, "unauthorized.invalid_password")
	InvalidToken        = newAppError(ECInvalidToken, "unauthorized.invalid_token")
	SeckillTokenInvalid = newAppError(ECSeckillTokenInvalid, "unauthorized.invalid_token")
)

var (
	NotFound                = newAppError(ECNotFound, "notfound.default")
	UserNotFound            = newAppError(ECUserNotFound, "notfound.user_not_exists")
	SeckillActivityNotFound = newAppError(ECSeckillActivityNotFound, "seckill.activity_not_found")
	SeckillSkuNotFound      = newAppError(ECSeckillSkuNotFound, "seckill.sku_not_found")
	SeckillOrderNotFound    = newAppError(ECSeckillOrderNotFound, "seckill.order_not_found")
	MallProductNotFound     = newAppError(ECMallProductNotFound, "mall.product_not_found")
	MallSkuNotFound         = newAppError(ECMallSkuNotFound, "mall.sku_not_found")
	MallOrderNotFound       = newAppError(ECMallOrderNotFound, "mall.order_not_found")
)

var (
	Conflict                        = newAppError(ECConflict, "conflict.default")
	UserExists                      = newAppError(ECUserExists, "conflict.user_already_exists")
	SeckillStockNotEnough           = newAppError(ECSeckillStockNotEnough, "seckill.stock_not_enough")
	SeckillAlreadyPurchased         = newAppError(ECSeckillAlreadyPurchased, "seckill.already_purchased")
	MallStockNotEnough              = newAppError(ECMallStockNotEnough, "mall.stock_not_enough")
	MallInvalidOrderTransition      = newAppError(ECMallInvalidOrderTransition, "mall.invalid_order_transition")
	MallOrderCannotCancel           = newAppError(ECMallOrderCannotCancel, "mall.order_cannot_cancel")
	MallDuplicateOrder              = newAppError(ECMallDuplicateOrder, "mall.duplicate_order")
	MallPaymentConfirmationConflict = newAppError(ECMallPaymentConfirmationConflict, "mall.payment_confirmation_conflict")
)

var (
	Gone                 = newAppError(ECGone, "gone.default")
	SeckillActivityEnded = newAppError(ECSeckillActivityEnded, "seckill.activity_ended")
)

var (
	TooManyRequests = newAppError(ECTooManyRequests, "too_many_requests.default")
	LimitExceeded   = newAppError(ECLimitExceeded, "too_many_requests.limit_exceeded")
)

var (
	Internal            = newAppError(ECInternal, "internal.default")
	Database            = newAppError(ECDatabase, "internal.database")
	TokenGeneration     = newAppError(ECTokenGeneration, "internal.token_generation")
	EmailSendFailed     = newAppError(ECEmailSendFailed, "internal.email_send_failed")
	SeckillSubmitFailed = newAppError(ECSeckillSubmitFailed, "internal.seckill_submit_failed")
)
