package errors

import (
	stderrors "errors"
	"fmt"
	"net/http"
)

type Code int

const ECOK Code = 0

// 400xxx: invalid request or user-correctable input errors.
const (
	ECInvalidRequest Code = 400000 + iota
	ECInvalidEmail
	ECCodeInvalid
	ECCodeExpired
	ECSeckillActivityNotStart
)

// 401xxx: authentication, credentials, and token errors.
const (
	ECUnauthorized Code = 401000 + iota
	ECInvalidPassword
	ECInvalidToken
	ECSeckillTokenInvalid
)

// 404xxx: requested resource does not exist.
const (
	ECNotFound Code = 404000 + iota
	ECUserNotFound
	ECSeckillActivityNotFound
	ECSeckillSkuNotFound
	ECSeckillOrderNotFound
	ECMallProductNotFound
	ECMallSkuNotFound
	ECMallOrderNotFound
)

// 409xxx: valid request conflicts with current business state.
const (
	ECConflict Code = 409000 + iota
	ECUserExists
	ECSeckillStockNotEnough
	ECSeckillAlreadyPurchased
	ECMallStockNotEnough
	ECMallInvalidOrderTransition
	ECMallOrderCannotCancel
	ECMallDuplicateOrder
)

// 410xxx: resource existed but is no longer available for this operation.
const (
	ECGone Code = 410000 + iota
	ECSeckillActivityEnded
)

// 429xxx: rate limit and abuse-prevention errors.
const (
	ECTooManyRequests Code = 429000 + iota
	ECLimitExceeded
)

// 500xxx: internal system and dependency errors.
const (
	ECInternal Code = 500000 + iota
	ECDatabase
	ECTokenGeneration
	ECEmailSendFailed
	ECSeckillSubmitFailed
)

const (
	LocaleInvalidDefault        = "invalid.default"
	LocaleInvalidEmail          = "invalid.invalid_email"
	LocaleCodeInvalid           = "verification.code_invalid"
	LocaleCodeExpired           = "verification.code_expired"
	LocaleSeckillActivityToWait = "seckill.activity_not_started"

	LocaleAuthUnauthorized    = "auth.unauthorized"
	LocaleAuthInvalidPassword = "auth.invalid_password"
	LocaleAuthInvalidToken    = "auth.invalid_token"

	LocaleNotFoundDefault         = "notfound.default"
	LocaleUserNotFound            = "user.not_found"
	LocaleSeckillActivityNotFound = "seckill.activity_not_found"
	LocaleSeckillSkuNotFound      = "seckill.sku_not_found"
	LocaleSeckillOrderNotFound    = "seckill.order_not_found"
	LocaleMallProductNotFound     = "mall.product_not_found"
	LocaleMallSkuNotFound         = "mall.sku_not_found"
	LocaleMallOrderNotFound       = "mall.order_not_found"

	LocaleConflictDefault            = "conflict.default"
	LocaleUserExists                 = "user.exists"
	LocaleSeckillStockNotEnough      = "seckill.stock_not_enough"
	LocaleSeckillAlreadyPurchased    = "seckill.already_purchased"
	LocaleMallStockNotEnough         = "mall.stock_not_enough"
	LocaleMallInvalidOrderTransition = "mall.invalid_order_transition"
	LocaleMallOrderCannotCancel      = "mall.order_cannot_cancel"
	LocaleMallDuplicateOrder         = "mall.duplicate_order"
	LocaleGoneDefault                = "gone.default"
	LocaleSeckillActivityEnded       = "seckill.activity_ended"
	LocaleTooManyRequestsDefault     = "too_many_requests.default"
	LocaleLimitExceeded              = "too_many_requests.limit_exceeded"
	LocaleInternalDefault            = "internal.default"
	LocaleDatabase                   = "internal.database"
	LocaleTokenGeneration            = "internal.token_generation"
	LocaleEmailSendFailed            = "internal.email_send_failed"
	LocaleSeckillSubmitFailed        = "internal.seckill_submit_failed"
)

// Compatibility aliases for older locale identifiers used by existing code/tests.
const (
	LocaleInvalidVerifyCode = LocaleCodeInvalid

	LocaleUnauthorizedDefault        = LocaleAuthUnauthorized
	LocaleUnauthorizedSessionExpired = LocaleAuthInvalidToken
)

// AppError is the unified application error.
//
// Code is the public, stable, numeric business code for frontend clients.
// DetailCode can be more precise for backend logs when callers use WithDetail.
// Reason is a stable backend-readable reason string, not intended for frontend display.
// LocaleKey is used by locale files such as locale.zh.toml / locale.en.toml.
type AppError struct {
	Code       Code
	DetailCode Code
	Reason     string
	Message    string

	LocaleKey  string
	LocaleArgs any

	HTTPStatus int
}

type PublicError struct {
	Code       Code   `json:"code"`
	MessageKey string `json:"messageKey"`
	MessageArg any    `json:"messageArg,omitempty"`
	RequestID  string `json:"requestId,omitempty"`
}

func NewAppError(
	code Code,
	detailCode Code,
	reason string,
	message string,
	localeKey string,
	localeArgs any,
	httpStatus int,
) *AppError {
	return &AppError{
		Code:       code,
		DetailCode: detailCode,
		Reason:     reason,
		Message:    message,
		LocaleKey:  localeKey,
		LocaleArgs: localeArgs,
		HTTPStatus: httpStatus,
	}
}

func newAppError(code Code, reason string, message string, localeKey string) *AppError {
	return NewAppError(code, code, reason, message, localeKey, nil, getStatusCode(code))
}

func getStatusCode(code Code) int {
	if code == ECOK {
		return http.StatusOK
	}

	return int(code / 1000)
}

func (e *AppError) Error() string {
	if e == nil {
		return ""
	}

	return fmt.Sprintf("%d:%s", e.DetailCode, e.Message)
}

func (e *AppError) PublicError(requestID string) PublicError {
	if e == nil {
		return PublicError{
			Code:       ECInternal,
			MessageKey: LocaleInternalDefault,
			RequestID:  requestID,
		}
	}

	return PublicError{
		Code:       e.Code,
		MessageKey: e.LocaleKey,
		MessageArg: e.LocaleArgs,
		RequestID:  requestID,
	}
}

func (e *AppError) LogFields() map[string]any {
	if e == nil {
		return map[string]any{
			"code":       ECInternal,
			"detailCode": ECInternal,
			"reason":     "INTERNAL_SERVER_ERROR",
			"message":    "Internal server error",
			"localeKey":  LocaleInternalDefault,
		}
	}

	return map[string]any{
		"code":       e.Code,
		"detailCode": e.DetailCode,
		"reason":     e.Reason,
		"message":    e.Message,
		"localeKey":  e.LocaleKey,
		"localeArgs": e.LocaleArgs,
		"httpStatus": e.HTTPStatus,
	}
}

// WithDetail returns a copied AppError with a more precise backend-only reason.
// It keeps the public Code and LocaleKey unchanged.
func (e *AppError) WithDetail(detailCode Code, reason string, message string) *AppError {
	if e == nil {
		return nil
	}

	copied := *e
	copied.DetailCode = detailCode
	copied.Reason = reason
	copied.Message = message

	return &copied
}

// WithLocale returns a copied AppError with a different frontend locale key.
func (e *AppError) WithLocale(localeKey string, localeArgs any) *AppError {
	if e == nil {
		return nil
	}

	copied := *e
	copied.LocaleKey = localeKey
	copied.LocaleArgs = localeArgs

	return &copied
}

func AsAppError(err error) (*AppError, bool) {
	if err == nil {
		return nil, false
	}

	var appErr *AppError
	if stderrors.As(err, &appErr) {
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
	ErrInvalid = newAppError(
		ECInvalidRequest,
		"INVALID_REQUEST",
		"Invalid request",
		LocaleInvalidDefault,
	)
	ErrInvalidEmail = newAppError(
		ECInvalidEmail,
		"INVALID_EMAIL",
		"Invalid email address",
		LocaleInvalidEmail,
	)
	ErrCodeInvalid = newAppError(
		ECCodeInvalid,
		"VERIFY_CODE_INVALID",
		"Invalid verification code",
		LocaleCodeInvalid,
	)
	ErrCodeExpired = newAppError(
		ECCodeExpired,
		"VERIFY_CODE_EXPIRED",
		"Verification code expired",
		LocaleCodeExpired,
	)
	ErrSeckillActivityNotStart = newAppError(
		ECSeckillActivityNotStart,
		"SECKILL_ACTIVITY_NOT_STARTED",
		"Seckill activity has not started",
		LocaleSeckillActivityToWait,
	)
)

var (
	ErrUnauthorized = newAppError(
		ECUnauthorized,
		"UNAUTHORIZED",
		"Unauthorized",
		LocaleAuthUnauthorized,
	)
	ErrInvalidPassword = newAppError(
		ECInvalidPassword,
		"AUTH_INVALID_PASSWORD",
		"Invalid password",
		LocaleAuthInvalidPassword,
	)
	ErrInvalidToken = newAppError(
		ECInvalidToken,
		"AUTH_INVALID_TOKEN",
		"Invalid token",
		LocaleAuthInvalidToken,
	)
	ErrSeckillTokenInvalid = newAppError(
		ECSeckillTokenInvalid,
		"SECKILL_TOKEN_INVALID",
		"Seckill token invalid or expired",
		LocaleAuthInvalidToken,
	)
)

var (
	ErrNotFound = newAppError(
		ECNotFound,
		"NOT_FOUND",
		"Resource not found",
		LocaleNotFoundDefault,
	)
	ErrUserNotFound = newAppError(
		ECUserNotFound,
		"USER_NOT_FOUND",
		"User not found",
		LocaleUserNotFound,
	)
	ErrSeckillActivityNotFound = newAppError(
		ECSeckillActivityNotFound,
		"SECKILL_ACTIVITY_NOT_FOUND",
		"Seckill activity not found",
		LocaleSeckillActivityNotFound,
	)
	ErrSeckillSkuNotFound = newAppError(
		ECSeckillSkuNotFound,
		"SECKILL_SKU_NOT_FOUND",
		"Seckill SKU not found",
		LocaleSeckillSkuNotFound,
	)
	ErrSeckillOrderNotFound = newAppError(
		ECSeckillOrderNotFound,
		"SECKILL_ORDER_NOT_FOUND",
		"Seckill order not found",
		LocaleSeckillOrderNotFound,
	)
	ErrMallProductNotFound = newAppError(
		ECMallProductNotFound,
		"MALL_PRODUCT_NOT_FOUND",
		"Mall product not found",
		LocaleMallProductNotFound,
	)
	ErrMallSkuNotFound = newAppError(
		ECMallSkuNotFound,
		"MALL_SKU_NOT_FOUND",
		"Mall SKU not found",
		LocaleMallSkuNotFound,
	)
	ErrMallOrderNotFound = newAppError(
		ECMallOrderNotFound,
		"MALL_ORDER_NOT_FOUND",
		"Mall order not found",
		LocaleMallOrderNotFound,
	)
)

var (
	ErrConflict = newAppError(
		ECConflict,
		"CONFLICT",
		"Resource conflict",
		LocaleConflictDefault,
	)
	ErrUserExists = newAppError(
		ECUserExists,
		"USER_EXISTS",
		"User already exists",
		LocaleUserExists,
	)
	ErrSeckillStockNotEnough = newAppError(
		ECSeckillStockNotEnough,
		"SECKILL_STOCK_NOT_ENOUGH",
		"Seckill stock not enough",
		LocaleSeckillStockNotEnough,
	)
	ErrSeckillAlreadyPurchased = newAppError(
		ECSeckillAlreadyPurchased,
		"SECKILL_ALREADY_PURCHASED",
		"Already purchased this seckill item",
		LocaleSeckillAlreadyPurchased,
	)
	ErrMallStockNotEnough = newAppError(
		ECMallStockNotEnough,
		"MALL_STOCK_NOT_ENOUGH",
		"Mall stock not enough",
		LocaleMallStockNotEnough,
	)
	ErrMallInvalidOrderTransition = newAppError(
		ECMallInvalidOrderTransition,
		"MALL_INVALID_ORDER_TRANSITION",
		"Invalid order status transition",
		LocaleMallInvalidOrderTransition,
	)
	ErrMallOrderCannotCancel = newAppError(
		ECMallOrderCannotCancel,
		"MALL_ORDER_CANNOT_CANCEL",
		"Order cannot be cancelled",
		LocaleMallOrderCannotCancel,
	)
	ErrMallDuplicateOrder = newAppError(
		ECMallDuplicateOrder,
		"MALL_DUPLICATE_ORDER",
		"Duplicate order request",
		LocaleMallDuplicateOrder,
	)
)

var (
	ErrGone = newAppError(
		ECGone,
		"GONE",
		"Resource gone",
		LocaleGoneDefault,
	)
	ErrSeckillActivityEnded = newAppError(
		ECSeckillActivityEnded,
		"SECKILL_ACTIVITY_ENDED",
		"Seckill activity has ended",
		LocaleSeckillActivityEnded,
	)
)

var (
	ErrTooManyRequests = newAppError(
		ECTooManyRequests,
		"TOO_MANY_REQUESTS",
		"Too many requests",
		LocaleTooManyRequestsDefault,
	)
	ErrLimitExceeded = newAppError(
		ECLimitExceeded,
		"LIMIT_EXCEEDED",
		"Request limit exceeded",
		LocaleLimitExceeded,
	)
)

var (
	ErrInternal = newAppError(
		ECInternal,
		"INTERNAL_SERVER_ERROR",
		"Internal server error",
		LocaleInternalDefault,
	)
	ErrDatabase = newAppError(
		ECDatabase,
		"DATABASE_ERROR",
		"Database error",
		LocaleDatabase,
	)
	ErrTokenGeneration = newAppError(
		ECTokenGeneration,
		"TOKEN_GENERATION_FAILED",
		"Failed to generate token",
		LocaleTokenGeneration,
	)
	ErrEmailSendFailed = newAppError(
		ECEmailSendFailed,
		"EMAIL_SEND_FAILED",
		"Failed to send email",
		LocaleEmailSendFailed,
	)
	ErrSeckillSubmitFailed = newAppError(
		ECSeckillSubmitFailed,
		"SECKILL_SUBMIT_FAILED",
		"Seckill order submit failed",
		LocaleSeckillSubmitFailed,
	)
)
