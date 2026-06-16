package errors

import (
	stderrors "errors"
	"fmt"
	"net/http"
)

type Code int

const (
	CodeOK Code = 0

	// Auth public errors: 400001 - 400999
	CodeAuthUnauthorized          Code = 400001
	CodeAuthInvalidToken          Code = 400002
	CodeAuthTokenExpired          Code = 400003
	CodeAuthTokenGenerationFailed Code = 400004
	CodeAuthPermissionDenied      Code = 400005
	CodeAuthInvalidPassword       Code = 400006

	// Auth detail errors: 400xxx
	CodeAuthInvalidTokenMalformed        Code = 400201
	CodeAuthInvalidTokenSignatureInvalid Code = 400202
	CodeAuthInvalidTokenUserNotFound     Code = 400203
	CodeAuthInvalidTokenExpired          Code = 400204
	CodeAuthLoginUserNotFound            Code = 400601
	CodeAuthLoginPasswordMismatch        Code = 400602
	CodeAuthLoginUserDisabled            Code = 400603
	CodeAuthTokenSigningFailed           Code = 400401
	CodeAuthTokenSecretMissing           Code = 400402

	// User public errors: 410001 - 410999
	CodeUserNotFound      Code = 410001
	CodeUserAlreadyExists Code = 410002
	CodeUserDisabled      Code = 410003

	// User detail errors: 410xxx
	CodeUserDeletedDetail  Code = 410101
	CodeUserDisabledDetail Code = 410102

	// Verification code public errors: 420001 - 420999
	CodeVerifyCodeInvalid Code = 420001
	CodeVerifyCodeExpired Code = 420002

	// Verification code detail errors: 420xxx
	CodeVerifyCodeNotFound        Code = 420101
	CodeVerifyCodeMismatch        Code = 420102
	CodeVerifyCodeExpiredDetail   Code = 420201
	CodeVerifyCodeUsed            Code = 420202
	CodeVerifyCodeAttemptsTooMany Code = 420203

	// Mail public errors: 430001 - 430999
	CodeMailInvalidEmail Code = 430001
	CodeMailSendFailed   Code = 430002

	// Mail detail errors: 430xxx
	CodeMailSMTPAuthFailed       Code = 430201
	CodeMailSMTPConnectionFailed Code = 430202
	CodeMailProviderRejected     Code = 430203

	// Seckill public errors: 500001 - 599999
	CodeSeckillActivityNotFound Code = 500001
	CodeSeckillSkuNotFound      Code = 500002
	CodeSeckillOrderNotFound    Code = 500003
	CodeSeckillStockNotEnough   Code = 500004
	CodeSeckillActivityNotStart Code = 500005
	CodeSeckillActivityEnded    Code = 500006
	CodeSeckillTokenInvalid     Code = 500007
	CodeSeckillAlreadyPurchased Code = 500008
	CodeSeckillSubmitFailed     Code = 500009

	// Seckill detail errors: 500xxx
	CodeSeckillTokenMalformed          Code = 500701
	CodeSeckillTokenExpired            Code = 500702
	CodeSeckillTokenActivityMismatch   Code = 500703
	CodeSeckillStockDeductFailed       Code = 500401
	CodeSeckillRedisStockMissing       Code = 500402
	CodeSeckillDuplicateSubmit         Code = 500801
	CodeSeckillSubmitQueueFailed       Code = 500901
	CodeSeckillSubmitLockFailed        Code = 500902
	CodeSeckillSubmitPersistenceFailed Code = 500903

	// Mall public errors: 600001 - 699999
	CodeMallProductNotFound        Code = 600001
	CodeMallSkuNotFound            Code = 600002
	CodeMallOrderNotFound          Code = 600003
	CodeMallStockNotEnough         Code = 600004
	CodeMallInvalidOrderTransition Code = 600005
	CodeMallOrderCannotCancel      Code = 600006
	CodeMallDuplicateOrder         Code = 600007

	// Mall detail errors: 600xxx
	CodeMallProductDeleted          Code = 600101
	CodeMallProductDisabled         Code = 600102
	CodeMallSkuDeleted              Code = 600201
	CodeMallSkuDisabled             Code = 600202
	CodeMallStockDeductFailed       Code = 600401
	CodeMallStockRollbackFailed     Code = 600402
	CodeMallOrderStatusMismatch     Code = 600501
	CodeMallOrderAlreadyPaid        Code = 600502
	CodeMallOrderAlreadyCancelled   Code = 600503
	CodeMallDuplicateIdempotencyKey Code = 600701
	CodeMallDuplicatePendingOrder   Code = 600702

	// Rate limit public errors: 700001 - 700999
	CodeRateTooManyRequests Code = 700001
	CodeRateLimitExceeded   Code = 700002

	// Rate limit detail errors: 700xxx
	CodeRateIPLimited     Code = 700101
	CodeRateUserLimited   Code = 700102
	CodeRateRouteLimited  Code = 700103
	CodeRateGlobalLimited Code = 700104

	// System public errors: 900001 - 900999
	CodeSystemInternal Code = 900001

	// System detail errors: 900xxx
	CodeSystemPanic             Code = 900101
	CodeSystemConfigMissing     Code = 900102
	CodeSystemConfigInvalid     Code = 900103
	CodeSystemDependencyFailure Code = 900104

	// Database public errors: 910001 - 910999
	CodeDatabaseError Code = 910001

	// Database detail errors: 910xxx
	CodeDatabaseQueryFailed       Code = 910101
	CodeDatabaseInsertFailed      Code = 910102
	CodeDatabaseUpdateFailed      Code = 910103
	CodeDatabaseDeleteFailed      Code = 910104
	CodeDatabaseTransactionFailed Code = 910105
	CodeDatabaseRecordNotFound    Code = 910106
)

// AppError is the unified application error.
//
// Code is a public, stable, numeric business code for frontend.
// DetailCode is a detailed numeric code for backend logs and troubleshooting.
// Reason is a stable backend-readable reason string, not intended for frontend display.
type AppError struct {
	Code          Code
	DetailCode    Code
	Reason        string
	MessageZh     string
	MessageEn     string
	UserMessageZh string
	UserMessageEn string
	HTTPStatus    int
	Retryable     bool
	SafeToShow    bool
}

type PublicError struct {
	Code      Code   `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
}

func NewAppError(
	code Code,
	detailCode Code,
	reason string,
	messageZh string,
	messageEn string,
	userMessageZh string,
	userMessageEn string,
	httpStatus int,
	retryable bool,
	safeToShow bool,
) *AppError {
	return &AppError{
		Code:          code,
		DetailCode:    detailCode,
		Reason:        reason,
		MessageZh:     messageZh,
		MessageEn:     messageEn,
		UserMessageZh: userMessageZh,
		UserMessageEn: userMessageEn,
		HTTPStatus:    httpStatus,
		Retryable:     retryable,
		SafeToShow:    safeToShow,
	}
}

func (e *AppError) Error() string {
	if e == nil {
		return ""
	}

	return fmt.Sprintf("%d:%s", e.DetailCode, e.MessageEn)
}

func (e *AppError) PublicMessage(lang string) string {
	if e == nil {
		return "系统繁忙，请稍后重试"
	}

	if lang == "en" && e.UserMessageEn != "" {
		return e.UserMessageEn
	}

	if e.UserMessageZh != "" {
		return e.UserMessageZh
	}

	if e.SafeToShow && e.MessageZh != "" {
		return e.MessageZh
	}

	return "系统繁忙，请稍后重试"
}

func (e *AppError) PublicError(lang string, requestID string) PublicError {
	if e == nil {
		return PublicError{
			Code:      CodeSystemInternal,
			Message:   "系统繁忙，请稍后重试",
			RequestID: requestID,
		}
	}

	return PublicError{
		Code:      e.Code,
		Message:   e.PublicMessage(lang),
		RequestID: requestID,
	}
}

func (e *AppError) LogFields() map[string]any {
	if e == nil {
		return map[string]any{
			"code":       CodeSystemInternal,
			"detailCode": CodeSystemInternal,
			"reason":     "SYSTEM_INTERNAL_ERROR",
			"messageZh":  "系统内部错误",
			"messageEn":  "Internal server error",
		}
	}

	return map[string]any{
		"code":          e.Code,
		"detailCode":    e.DetailCode,
		"reason":        e.Reason,
		"messageZh":     e.MessageZh,
		"messageEn":     e.MessageEn,
		"httpStatus":    e.HTTPStatus,
		"retryable":     e.Retryable,
		"safeToShow":    e.SafeToShow,
		"userMessageZh": e.UserMessageZh,
		"userMessageEn": e.UserMessageEn,
	}
}

// WithDetail returns a copied AppError with a more detailed backend-only code and reason.
// It is useful when frontend should see the same fuzzy Code, but backend logs need finer details.
func (e *AppError) WithDetail(detailCode Code, reason string, messageZh string, messageEn string) *AppError {
	if e == nil {
		return nil
	}

	copied := *e
	copied.DetailCode = detailCode
	copied.Reason = reason
	copied.MessageZh = messageZh
	copied.MessageEn = messageEn

	return &copied
}

func AsAppError(err error) (*AppError, bool) {
	if err == nil {
		return nil, false
	}

	if appErr, ok := stderrors.AsType[*AppError](err); ok {
		return appErr, true
	}

	return nil, false
}

func MustAppError(err error) *AppError {
	if appErr, ok := AsAppError(err); ok {
		return appErr
	}

	return ErrInternal
}

var (
	// System errors: 900001 - 900999
	ErrInternal = NewAppError(
		CodeSystemInternal,
		CodeSystemInternal,
		"SYSTEM_INTERNAL_ERROR",
		"系统内部错误",
		"Internal server error",
		"系统繁忙，请稍后重试",
		"System is busy, please try again later",
		http.StatusInternalServerError,
		true,
		false,
	)

	ErrSystemPanic = ErrInternal.WithDetail(
		CodeSystemPanic,
		"SYSTEM_PANIC",
		"系统发生 panic",
		"System panic occurred",
	)

	ErrSystemConfigMissing = ErrInternal.WithDetail(
		CodeSystemConfigMissing,
		"SYSTEM_CONFIG_MISSING",
		"系统配置缺失",
		"System configuration is missing",
	)

	ErrSystemConfigInvalid = ErrInternal.WithDetail(
		CodeSystemConfigInvalid,
		"SYSTEM_CONFIG_INVALID",
		"系统配置不合法",
		"System configuration is invalid",
	)

	ErrSystemDependencyFailure = ErrInternal.WithDetail(
		CodeSystemDependencyFailure,
		"SYSTEM_DEPENDENCY_FAILURE",
		"系统依赖服务异常",
		"System dependency service failure",
	)
)

var (
	// Database errors: 910001 - 910999
	ErrDatabase = NewAppError(
		CodeDatabaseError,
		CodeDatabaseError,
		"DATABASE_ERROR",
		"数据库错误",
		"Database error",
		"系统繁忙，请稍后重试",
		"System is busy, please try again later",
		http.StatusInternalServerError,
		true,
		false,
	)

	ErrDatabaseQueryFailed = ErrDatabase.WithDetail(
		CodeDatabaseQueryFailed,
		"DATABASE_QUERY_FAILED",
		"数据库查询失败",
		"Database query failed",
	)

	ErrDatabaseInsertFailed = ErrDatabase.WithDetail(
		CodeDatabaseInsertFailed,
		"DATABASE_INSERT_FAILED",
		"数据库写入失败",
		"Database insert failed",
	)

	ErrDatabaseUpdateFailed = ErrDatabase.WithDetail(
		CodeDatabaseUpdateFailed,
		"DATABASE_UPDATE_FAILED",
		"数据库更新失败",
		"Database update failed",
	)

	ErrDatabaseDeleteFailed = ErrDatabase.WithDetail(
		CodeDatabaseDeleteFailed,
		"DATABASE_DELETE_FAILED",
		"数据库删除失败",
		"Database delete failed",
	)

	ErrDatabaseTransactionFailed = ErrDatabase.WithDetail(
		CodeDatabaseTransactionFailed,
		"DATABASE_TRANSACTION_FAILED",
		"数据库事务执行失败",
		"Database transaction failed",
	)

	ErrDatabaseRecordNotFound = ErrDatabase.WithDetail(
		CodeDatabaseRecordNotFound,
		"DATABASE_RECORD_NOT_FOUND",
		"数据库记录不存在",
		"Database record not found",
	)
)

var (
	// Auth errors: 400001 - 400999
	ErrUnauthorized = NewAppError(
		CodeAuthUnauthorized,
		CodeAuthUnauthorized,
		"AUTH_UNAUTHORIZED",
		"用户未登录或登录状态无效",
		"Unauthorized",
		"请先登录",
		"Please sign in first",
		http.StatusUnauthorized,
		false,
		true,
	)

	ErrInvalidToken = NewAppError(
		CodeAuthInvalidToken,
		CodeAuthInvalidToken,
		"AUTH_INVALID_TOKEN",
		"Token 无效",
		"Invalid token",
		"登录状态已失效，请重新登录",
		"Login session is invalid, please sign in again",
		http.StatusUnauthorized,
		false,
		true,
	)

	ErrTokenExpired = NewAppError(
		CodeAuthTokenExpired,
		CodeAuthInvalidTokenExpired,
		"AUTH_TOKEN_EXPIRED",
		"Token 已过期",
		"Token has expired",
		"登录状态已过期，请重新登录",
		"Login session has expired, please sign in again",
		http.StatusUnauthorized,
		false,
		true,
	)

	ErrInvalidTokenMalformed = ErrInvalidToken.WithDetail(
		CodeAuthInvalidTokenMalformed,
		"AUTH_TOKEN_MALFORMED",
		"Token 格式错误",
		"Token is malformed",
	)

	ErrInvalidTokenSignatureInvalid = ErrInvalidToken.WithDetail(
		CodeAuthInvalidTokenSignatureInvalid,
		"AUTH_TOKEN_SIGNATURE_INVALID",
		"Token 签名校验失败",
		"Token signature validation failed",
	)

	ErrInvalidTokenUserNotFound = ErrInvalidToken.WithDetail(
		CodeAuthInvalidTokenUserNotFound,
		"AUTH_TOKEN_USER_NOT_FOUND",
		"Token 关联用户不存在",
		"Token related user not found",
	)

	ErrTokenGeneration = NewAppError(
		CodeAuthTokenGenerationFailed,
		CodeAuthTokenGenerationFailed,
		"AUTH_TOKEN_GENERATION_FAILED",
		"Token 生成失败",
		"Failed to generate token",
		"登录失败，请稍后重试",
		"Sign in failed, please try again later",
		http.StatusInternalServerError,
		true,
		false,
	)

	ErrTokenSigningFailed = ErrTokenGeneration.WithDetail(
		CodeAuthTokenSigningFailed,
		"AUTH_TOKEN_SIGNING_FAILED",
		"Token 签名生成失败",
		"Failed to sign token",
	)

	ErrTokenSecretMissing = ErrTokenGeneration.WithDetail(
		CodeAuthTokenSecretMissing,
		"AUTH_TOKEN_SECRET_MISSING",
		"Token 签名密钥缺失",
		"Token signing secret is missing",
	)

	ErrInvalidPassword = NewAppError(
		CodeAuthInvalidPassword,
		CodeAuthInvalidPassword,
		"AUTH_INVALID_PASSWORD",
		"账号或密码错误",
		"Invalid account or password",
		"账号或密码错误",
		"Invalid account or password",
		http.StatusUnauthorized,
		false,
		true,
	)

	ErrLoginUserNotFound = ErrInvalidPassword.WithDetail(
		CodeAuthLoginUserNotFound,
		"AUTH_LOGIN_USER_NOT_FOUND",
		"登录用户不存在",
		"Login user not found",
	)

	ErrLoginPasswordMismatch = ErrInvalidPassword.WithDetail(
		CodeAuthLoginPasswordMismatch,
		"AUTH_LOGIN_PASSWORD_MISMATCH",
		"登录密码不匹配",
		"Login password does not match",
	)

	ErrLoginUserDisabled = ErrInvalidPassword.WithDetail(
		CodeAuthLoginUserDisabled,
		"AUTH_LOGIN_USER_DISABLED",
		"登录用户已被禁用",
		"Login user has been disabled",
	)

	ErrPermissionDenied = NewAppError(
		CodeAuthPermissionDenied,
		CodeAuthPermissionDenied,
		"AUTH_PERMISSION_DENIED",
		"权限不足",
		"Permission denied",
		"无权限执行该操作",
		"You do not have permission to perform this operation",
		http.StatusForbidden,
		false,
		true,
	)
)

var (
	// User errors: 410001 - 410999
	ErrUserNotFound = NewAppError(
		CodeUserNotFound,
		CodeUserNotFound,
		"USER_NOT_FOUND",
		"用户不存在",
		"User not found",
		"用户不存在或不可用",
		"User does not exist or is unavailable",
		http.StatusNotFound,
		false,
		true,
	)

	ErrUserDeleted = ErrUserNotFound.WithDetail(
		CodeUserDeletedDetail,
		"USER_DELETED",
		"用户已删除",
		"User has been deleted",
	)

	ErrUserDisabled = NewAppError(
		CodeUserDisabled,
		CodeUserDisabled,
		"USER_DISABLED",
		"用户已被禁用",
		"User has been disabled",
		"账号状态异常，请联系客服",
		"Account status is abnormal, please contact support",
		http.StatusForbidden,
		false,
		true,
	)

	ErrUserExists = NewAppError(
		CodeUserAlreadyExists,
		CodeUserAlreadyExists,
		"USER_ALREADY_EXISTS",
		"用户已存在",
		"User already exists",
		"该账号已被注册",
		"This account has already been registered",
		http.StatusConflict,
		false,
		true,
	)
)

var (
	// Verification code errors: 420001 - 420999
	ErrCodeInvalid = NewAppError(
		CodeVerifyCodeInvalid,
		CodeVerifyCodeInvalid,
		"VERIFY_CODE_INVALID",
		"验证码错误",
		"Invalid verification code",
		"验证码错误",
		"Invalid verification code",
		http.StatusBadRequest,
		false,
		true,
	)

	ErrCodeNotFound = ErrCodeInvalid.WithDetail(
		CodeVerifyCodeNotFound,
		"VERIFY_CODE_NOT_FOUND",
		"验证码不存在",
		"Verification code not found",
	)

	ErrCodeMismatch = ErrCodeInvalid.WithDetail(
		CodeVerifyCodeMismatch,
		"VERIFY_CODE_MISMATCH",
		"验证码不匹配",
		"Verification code does not match",
	)

	ErrCodeAttemptsTooMany = ErrCodeInvalid.WithDetail(
		CodeVerifyCodeAttemptsTooMany,
		"VERIFY_CODE_ATTEMPTS_TOO_MANY",
		"验证码校验尝试次数过多",
		"Too many verification code attempts",
	)

	ErrCodeExpired = NewAppError(
		CodeVerifyCodeExpired,
		CodeVerifyCodeExpiredDetail,
		"VERIFY_CODE_EXPIRED",
		"验证码已过期",
		"Verification code expired",
		"验证码已过期，请重新获取",
		"Verification code expired, please request a new one",
		http.StatusBadRequest,
		false,
		true,
	)

	ErrCodeUsed = ErrCodeExpired.WithDetail(
		CodeVerifyCodeUsed,
		"VERIFY_CODE_USED",
		"验证码已被使用",
		"Verification code has already been used",
	)
)

var (
	// Mail errors: 430001 - 430999
	ErrInvalidEmail = NewAppError(
		CodeMailInvalidEmail,
		CodeMailInvalidEmail,
		"MAIL_INVALID_EMAIL",
		"邮箱地址格式不合法",
		"Invalid email address",
		"邮箱格式不正确",
		"Invalid email address",
		http.StatusBadRequest,
		false,
		true,
	)

	ErrEmailSendFailed = NewAppError(
		CodeMailSendFailed,
		CodeMailSendFailed,
		"MAIL_SEND_FAILED",
		"邮件发送失败",
		"Failed to send email",
		"邮件发送失败，请稍后重试",
		"Failed to send email, please try again later",
		http.StatusInternalServerError,
		true,
		false,
	)

	ErrMailSMTPAuthFailed = ErrEmailSendFailed.WithDetail(
		CodeMailSMTPAuthFailed,
		"MAIL_SMTP_AUTH_FAILED",
		"SMTP 认证失败",
		"SMTP authentication failed",
	)

	ErrMailSMTPConnectionFailed = ErrEmailSendFailed.WithDetail(
		CodeMailSMTPConnectionFailed,
		"MAIL_SMTP_CONNECTION_FAILED",
		"SMTP 连接失败",
		"SMTP connection failed",
	)

	ErrMailProviderRejected = ErrEmailSendFailed.WithDetail(
		CodeMailProviderRejected,
		"MAIL_PROVIDER_REJECTED",
		"邮件服务商拒绝发送",
		"Mail provider rejected sending",
	)
)

var (
	// Rate limit errors: 700001 - 700999
	ErrTooManyRequests = NewAppError(
		CodeRateTooManyRequests,
		CodeRateTooManyRequests,
		"RATE_TOO_MANY_REQUESTS",
		"请求过于频繁",
		"Too many requests",
		"请求过于频繁，请稍后重试",
		"Too many requests, please try again later",
		http.StatusTooManyRequests,
		true,
		true,
	)

	ErrRateIPLimited = ErrTooManyRequests.WithDetail(
		CodeRateIPLimited,
		"RATE_IP_LIMITED",
		"IP 请求被限流",
		"IP request has been rate limited",
	)

	ErrRateUserLimited = ErrTooManyRequests.WithDetail(
		CodeRateUserLimited,
		"RATE_USER_LIMITED",
		"用户请求被限流",
		"User request has been rate limited",
	)

	ErrRateRouteLimited = ErrTooManyRequests.WithDetail(
		CodeRateRouteLimited,
		"RATE_ROUTE_LIMITED",
		"接口请求被限流",
		"Route request has been rate limited",
	)

	ErrRateGlobalLimited = ErrTooManyRequests.WithDetail(
		CodeRateGlobalLimited,
		"RATE_GLOBAL_LIMITED",
		"全局请求被限流",
		"Global request has been rate limited",
	)

	ErrLimitExceeded = NewAppError(
		CodeRateLimitExceeded,
		CodeRateLimitExceeded,
		"RATE_LIMIT_EXCEEDED",
		"请求超过限制",
		"Request limit exceeded",
		"操作太频繁，请稍后再试",
		"Too many operations, please try again later",
		http.StatusTooManyRequests,
		true,
		true,
	)
)

var (
	// Seckill errors: 500001 - 599999
	ErrSeckillActivityNotFound = NewAppError(
		CodeSeckillActivityNotFound,
		CodeSeckillActivityNotFound,
		"SECKILL_ACTIVITY_NOT_FOUND",
		"秒杀活动不存在",
		"Seckill activity not found",
		"活动不存在或已下线",
		"Activity does not exist or has been removed",
		http.StatusNotFound,
		false,
		true,
	)

	ErrSeckillSkuNotFound = NewAppError(
		CodeSeckillSkuNotFound,
		CodeSeckillSkuNotFound,
		"SECKILL_SKU_NOT_FOUND",
		"秒杀 SKU 不存在",
		"Seckill SKU not found",
		"商品不存在或已下线",
		"Item does not exist or has been removed",
		http.StatusNotFound,
		false,
		true,
	)

	ErrSeckillOrderNotFound = NewAppError(
		CodeSeckillOrderNotFound,
		CodeSeckillOrderNotFound,
		"SECKILL_ORDER_NOT_FOUND",
		"秒杀订单不存在",
		"Seckill order not found",
		"订单不存在",
		"Order does not exist",
		http.StatusNotFound,
		false,
		true,
	)

	ErrSeckillStockNotEnough = NewAppError(
		CodeSeckillStockNotEnough,
		CodeSeckillStockNotEnough,
		"SECKILL_STOCK_NOT_ENOUGH",
		"秒杀库存不足",
		"Seckill stock not enough",
		"商品已抢光",
		"Item is sold out",
		http.StatusConflict,
		false,
		true,
	)

	ErrSeckillStockDeductFailed = ErrSeckillStockNotEnough.WithDetail(
		CodeSeckillStockDeductFailed,
		"SECKILL_STOCK_DEDUCT_FAILED",
		"秒杀库存扣减失败",
		"Failed to deduct seckill stock",
	)

	ErrSeckillRedisStockMissing = ErrSeckillStockNotEnough.WithDetail(
		CodeSeckillRedisStockMissing,
		"SECKILL_REDIS_STOCK_MISSING",
		"Redis 秒杀库存不存在",
		"Redis seckill stock is missing",
	)

	ErrSeckillActivityNotStart = NewAppError(
		CodeSeckillActivityNotStart,
		CodeSeckillActivityNotStart,
		"SECKILL_ACTIVITY_NOT_STARTED",
		"秒杀活动尚未开始",
		"Seckill activity has not started yet",
		"活动尚未开始",
		"Activity has not started yet",
		http.StatusBadRequest,
		false,
		true,
	)

	ErrSeckillActivityEnded = NewAppError(
		CodeSeckillActivityEnded,
		CodeSeckillActivityEnded,
		"SECKILL_ACTIVITY_ENDED",
		"秒杀活动已结束",
		"Seckill activity has ended",
		"活动已结束",
		"Activity has ended",
		http.StatusBadRequest,
		false,
		true,
	)

	ErrSeckillTokenInvalid = NewAppError(
		CodeSeckillTokenInvalid,
		CodeSeckillTokenInvalid,
		"SECKILL_TOKEN_INVALID",
		"秒杀令牌无效或已过期",
		"Seckill token invalid or expired",
		"秒杀资格已失效，请重新进入",
		"Seckill qualification is invalid, please enter again",
		http.StatusUnauthorized,
		false,
		true,
	)

	ErrSeckillTokenMalformed = ErrSeckillTokenInvalid.WithDetail(
		CodeSeckillTokenMalformed,
		"SECKILL_TOKEN_MALFORMED",
		"秒杀令牌格式错误",
		"Seckill token is malformed",
	)

	ErrSeckillTokenExpired = ErrSeckillTokenInvalid.WithDetail(
		CodeSeckillTokenExpired,
		"SECKILL_TOKEN_EXPIRED",
		"秒杀令牌已过期",
		"Seckill token has expired",
	)

	ErrSeckillTokenActivityMismatch = ErrSeckillTokenInvalid.WithDetail(
		CodeSeckillTokenActivityMismatch,
		"SECKILL_TOKEN_ACTIVITY_MISMATCH",
		"秒杀令牌活动不匹配",
		"Seckill token activity does not match",
	)

	ErrSeckillAlreadyPurchased = NewAppError(
		CodeSeckillAlreadyPurchased,
		CodeSeckillAlreadyPurchased,
		"SECKILL_ALREADY_PURCHASED",
		"已购买过该秒杀商品",
		"Already purchased this seckill item",
		"您已参与过该商品秒杀",
		"You have already participated in this seckill item",
		http.StatusConflict,
		false,
		true,
	)

	ErrSeckillDuplicateSubmit = ErrSeckillAlreadyPurchased.WithDetail(
		CodeSeckillDuplicateSubmit,
		"SECKILL_DUPLICATE_SUBMIT",
		"秒杀重复提交",
		"Duplicate seckill submit",
	)

	ErrSeckillSubmitFailed = NewAppError(
		CodeSeckillSubmitFailed,
		CodeSeckillSubmitFailed,
		"SECKILL_SUBMIT_FAILED",
		"秒杀订单提交失败",
		"Seckill order submit failed",
		"下单失败，请稍后重试",
		"Failed to place order, please try again later",
		http.StatusInternalServerError,
		true,
		false,
	)

	ErrSeckillSubmitQueueFailed = ErrSeckillSubmitFailed.WithDetail(
		CodeSeckillSubmitQueueFailed,
		"SECKILL_SUBMIT_QUEUE_FAILED",
		"秒杀订单投递队列失败",
		"Failed to enqueue seckill order",
	)

	ErrSeckillSubmitLockFailed = ErrSeckillSubmitFailed.WithDetail(
		CodeSeckillSubmitLockFailed,
		"SECKILL_SUBMIT_LOCK_FAILED",
		"秒杀订单加锁失败",
		"Failed to lock seckill submit",
	)

	ErrSeckillSubmitPersistenceFailed = ErrSeckillSubmitFailed.WithDetail(
		CodeSeckillSubmitPersistenceFailed,
		"SECKILL_SUBMIT_PERSISTENCE_FAILED",
		"秒杀订单持久化失败",
		"Failed to persist seckill order",
	)
)

var (
	// Mall errors: 600001 - 699999
	ErrMallProductNotFound = NewAppError(
		CodeMallProductNotFound,
		CodeMallProductNotFound,
		"MALL_PRODUCT_NOT_FOUND",
		"商城商品不存在",
		"Mall product not found",
		"商品不存在或已下架",
		"Product does not exist or has been removed",
		http.StatusNotFound,
		false,
		true,
	)

	ErrMallProductDeleted = ErrMallProductNotFound.WithDetail(
		CodeMallProductDeleted,
		"MALL_PRODUCT_DELETED",
		"商城商品已删除",
		"Mall product has been deleted",
	)

	ErrMallProductDisabled = ErrMallProductNotFound.WithDetail(
		CodeMallProductDisabled,
		"MALL_PRODUCT_DISABLED",
		"商城商品已禁用",
		"Mall product has been disabled",
	)

	ErrMallSkuNotFound = NewAppError(
		CodeMallSkuNotFound,
		CodeMallSkuNotFound,
		"MALL_SKU_NOT_FOUND",
		"商城 SKU 不存在",
		"Mall SKU not found",
		"商品规格不存在或已下架",
		"Product SKU does not exist or has been removed",
		http.StatusNotFound,
		false,
		true,
	)

	ErrMallSkuDeleted = ErrMallSkuNotFound.WithDetail(
		CodeMallSkuDeleted,
		"MALL_SKU_DELETED",
		"商城 SKU 已删除",
		"Mall SKU has been deleted",
	)

	ErrMallSkuDisabled = ErrMallSkuNotFound.WithDetail(
		CodeMallSkuDisabled,
		"MALL_SKU_DISABLED",
		"商城 SKU 已禁用",
		"Mall SKU has been disabled",
	)

	ErrMallOrderNotFound = NewAppError(
		CodeMallOrderNotFound,
		CodeMallOrderNotFound,
		"MALL_ORDER_NOT_FOUND",
		"商城订单不存在",
		"Mall order not found",
		"订单不存在",
		"Order does not exist",
		http.StatusNotFound,
		false,
		true,
	)

	ErrMallStockNotEnough = NewAppError(
		CodeMallStockNotEnough,
		CodeMallStockNotEnough,
		"MALL_STOCK_NOT_ENOUGH",
		"商城库存不足",
		"Mall stock not enough",
		"商品库存不足",
		"Insufficient product stock",
		http.StatusConflict,
		false,
		true,
	)

	ErrMallStockDeductFailed = ErrMallStockNotEnough.WithDetail(
		CodeMallStockDeductFailed,
		"MALL_STOCK_DEDUCT_FAILED",
		"商城库存扣减失败",
		"Failed to deduct mall stock",
	)

	ErrMallStockRollbackFailed = ErrMallStockNotEnough.WithDetail(
		CodeMallStockRollbackFailed,
		"MALL_STOCK_ROLLBACK_FAILED",
		"商城库存回滚失败",
		"Failed to rollback mall stock",
	)

	ErrMallInvalidOrderTransition = NewAppError(
		CodeMallInvalidOrderTransition,
		CodeMallInvalidOrderTransition,
		"MALL_INVALID_ORDER_TRANSITION",
		"非法订单状态流转",
		"Invalid order status transition",
		"当前订单状态不支持该操作",
		"Current order status does not support this operation",
		http.StatusConflict,
		false,
		true,
	)

	ErrMallOrderStatusMismatch = ErrMallInvalidOrderTransition.WithDetail(
		CodeMallOrderStatusMismatch,
		"MALL_ORDER_STATUS_MISMATCH",
		"订单状态不匹配",
		"Order status does not match",
	)

	ErrMallOrderAlreadyPaid = ErrMallInvalidOrderTransition.WithDetail(
		CodeMallOrderAlreadyPaid,
		"MALL_ORDER_ALREADY_PAID",
		"订单已支付",
		"Order has already been paid",
	)

	ErrMallOrderAlreadyCancelled = ErrMallInvalidOrderTransition.WithDetail(
		CodeMallOrderAlreadyCancelled,
		"MALL_ORDER_ALREADY_CANCELLED",
		"订单已取消",
		"Order has already been cancelled",
	)

	ErrMallOrderCannotCancel = NewAppError(
		CodeMallOrderCannotCancel,
		CodeMallOrderCannotCancel,
		"MALL_ORDER_CANNOT_CANCEL",
		"订单不可取消",
		"Order cannot be cancelled",
		"当前订单不可取消",
		"Current order cannot be cancelled",
		http.StatusConflict,
		false,
		true,
	)

	ErrMallDuplicateOrder = NewAppError(
		CodeMallDuplicateOrder,
		CodeMallDuplicateOrder,
		"MALL_DUPLICATE_ORDER",
		"重复下单请求",
		"Duplicate order request",
		"请勿重复提交订单",
		"Please do not submit the order repeatedly",
		http.StatusConflict,
		false,
		true,
	)

	ErrMallDuplicateIdempotencyKey = ErrMallDuplicateOrder.WithDetail(
		CodeMallDuplicateIdempotencyKey,
		"MALL_DUPLICATE_IDEMPOTENCY_KEY",
		"重复的幂等键",
		"Duplicate idempotency key",
	)

	ErrMallDuplicatePendingOrder = ErrMallDuplicateOrder.WithDetail(
		CodeMallDuplicatePendingOrder,
		"MALL_DUPLICATE_PENDING_ORDER",
		"存在未完成的重复订单",
		"Duplicate pending order exists",
	)
)
