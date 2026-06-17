package errors

import (
	stderrors "errors"
	"fmt"
	"net/http"
)

type Code int

// 说明：
// 1. 错误码设计遵循 HTTP 状态码分类，前三位表示 HTTP 状态码，后三位表示具体业务错误码。
// 2. 每个错误码常量的命名遵循 RC + 具体错误描述 的格式，便于识别和维护。
// 3. 错误码从各自类别的基数开始递增，确保唯一性和可扩展性。
// 4. 特别注意：不要为了分组而随意调整已有错误码顺序，避免影响现有系统的稳定性。
const (
	RCOK Code = 0
)

// RCInvalid 请求错误类，400
const (
	RCInvalid Code = 400000 + iota
	RCInsufficientPoints
	RCInvalidVerifyCode
	RCInvalidMiddleToken
	RCInvalidRemainQuota
	RCFileEmpty
	RCFileTooLarge
	RCUnsupportedAudioFormat
	RCInvalidAudioInfo
	RCInvalidAudioDuration
	RCOnlyDemoVoicesAllowed
	RCTextTooLong
	RCInvalidPendingToken
	RCDailyVideoGenLimitExceeded
	RCConcurrentVideoGenLimitExceeded
	RCConcurrentLimitExceeded
	RCSomeExists
	RCSomeNotExists
	RCNotAllowDelete
	RCInvalidAudioFile
	RCEmptyText
	RCInvalidText
	RCTextContainsUnsupportedCharacters
	RCAccountInArrears

	// 支付相关错误
	RCInvalidTradeType
	RCInvalidTradeMethod
	RCInvalidSubMonths
	RCSubscribeNotFound
	RCTradeRecordNotFound
	RCSubscribePriceNotFound
	RCResourcePackageNotFound
	RCSpreadPriceCalcFailed
	RCSubscribeLevelLtCurrentLevel
	RCSubscribePlanIsDisabled
	RCTradeStatusNotPending
	RCOnlySuccessfulTradeCanBeRefund
	RCOnlyTradeRefundOfRechargeType
	RCTradeRefundNotAllowed
	RCTradeSubscribeRefundNotAllowed
	RCTradeSubscribeNotLastCycle
	RCTradeSubscribePlanExpired
	RCTransactionTypeNotSupportRefund
	RCTradeSubscribeRefundExceedGracePeriod
	RCTradeSubscribeRefundHasUsageWithinGracePeriod
	RCTradeRefundAmountExceedPaidAmount
	RCTradeSubscribeUpgradeNeedManualRefund
	RCRefundAmountMustGtZero

	// 认证相关
	RCCertFailed
	RCCertEnterpriseAlreadyCompleted
	RCCertPersonalAlreadyCompleted
	RCCertProcessingExists
	RCCertEnterpriseLicenseVerifyFailed
	RCCertEnterpriseNotAllowModify
	RCCertEnterpriseLicenseNotFound
	RCCertEnterpriseTransferCanAudit
	RCCertEnterpriseTransferNotWaitAudit
	RCCertIDCardBindOverLimit
	RCCertEnterpriseLicenseBindOverLimit
	RCCertEnterpriseLicenseImageInvalid

	// 邀请码 / 音色 / 模型相关
	RCInvalidUserInviteCode
	RCGenerateInviteCodeFailed
	RCVoiceSlotInactive
	RCNoAvailableVoiceSlots
	RCExceedReselectVoiceSlotsLimit
	RCNoEmptyVoiceSlots
	RCVoiceUnavailable
	RCModelNotFound
	RCModelUnsupportedCapability
	RCModelMissing
	RCModelCallFailed

	// 邮箱 / Token / 活动类通用请求错误
	RCInvalidEmail
	RCSeckillActivityNotStarted
)

// RCUnauthorized 认证类错误，401
const (
	RCUnauthorized Code = 401000 + iota
	RCUserDisabled
	RCUserPendingDelete
	RCUserDeleted
	RCSessionExpired
	RCAbnormalUserStatus
	RCInvalidToken
	RCInvalidTokenMalformed
	RCInvalidTokenSignatureInvalid
	RCInvalidTokenUserNotFound
	RCInvalidPassword
	RCLoginUserNotFound
	RCLoginPasswordMismatch
	RCLoginUserDisabled
	RCSeckillTokenInvalid
	RCSeckillTokenMalformed
	RCSeckillTokenExpired
	RCSeckillTokenActivityMismatch
)

// RCForbidden 权限类错误，403
const (
	RCForbidden Code = 403000 + iota
	RCNoPermission
	RCNoAccess
	RCNoVoiceAccess
	RCNeedCert
	RCMallProductDisabled
	RCMallSkuDisabled
)

// RCNotFound 资源未找到类错误，404
const (
	RCNotFound Code = 404000 + iota
	RCRoomNotExists
	RCConversationNotExists
	RCApiKeyNotExists
	RCVoiceNotExists
	RCVoicePlanNotExists
	RCUserNotExists
	RCVoiceSlotNotExists
	RCSeckillActivityNotFound
	RCSeckillSkuNotFound
	RCSeckillOrderNotFound
	RCMallProductNotFound
	RCMallSkuNotFound
	RCMallOrderNotFound
	RCDatabaseRecordNotFound
	RCBannerNotFound
)

// RCConflict 资源冲突类错误，409
const (
	RCConflict Code = 409000 + iota
	RCBannerTimeConflictNewUser
	RCBannerTimeConflictRegularUser
	RCSysNotificationTimeConflictNewUser
	RCSomeVoicesAreMissing
	RCVoicePlanAlreadyExists
	RCPhoneAlreadyRegistered
	RCSeckillStockNotEnough
	RCSeckillStockDeductFailed
	RCSeckillRedisStockMissing
	RCSeckillAlreadyPurchased
	RCSeckillDuplicateSubmit
	RCMallStockNotEnough
	RCMallStockDeductFailed
	RCMallStockRollbackFailed
	RCMallInvalidOrderTransition
	RCMallOrderStatusMismatch
	RCMallOrderAlreadyPaid
	RCMallOrderCannotCancel
	RCMallDuplicateOrder
	RCMallDuplicateIdempotencyKey
	RCMallDuplicatePendingOrder
	RCBannerAlreadyPublished
	RCBannerAlreadyUnpublished
)

// RCGone 资源已删除类错误，410
const (
	RCGone Code = 410000 + iota
	RCShouldNotGrantVoicePlanIndividually
	RCShouldNotRevokeVoicePlanIndividually
	RCSeckillActivityEnded
	RCMallProductDeleted
	RCMallSkuDeleted
	RCMallOrderAlreadyCancelled
)

// RCTooManyRequests 请求过多类错误，429
const (
	RCTooManyRequests Code = 429000 + iota
	RCTooManyPreservePoints
	RCVerifyCodeAttemptsTooMany
	RCRateLimitExceeded
	RCRateIPLimited
	RCRateUserLimited
	RCRateRouteLimited
	RCRateGlobalLimited
)

// RCInternal 系统类错误，500
const (
	RCInternal Code = 500000 + iota
	RCSystemPanic
	RCSystemConfigMissing
	RCSystemConfigInvalid
	RCSystemDependencyFailure
	RCDatabaseError
	RCDatabaseQueryFailed
	RCDatabaseInsertFailed
	RCDatabaseUpdateFailed
	RCDatabaseDeleteFailed
	RCDatabaseTransactionFailed
	RCTokenGenerationFailed
	RCTokenSigningFailed
	RCTokenSecretMissing
	RCMailSendFailed
	RCMailSMTPAuthFailed
	RCMailSMTPConnectionFailed
	RCMailProviderRejected
	RCSeckillSubmitFailed
	RCSeckillSubmitQueueFailed
	RCSeckillSubmitLockFailed
	RCSeckillSubmitPersistenceFailed
)

const (
	LocaleInvalidDefault                    = "invalid.default"
	LocaleInsufficientPoints                = "invalid.insufficient_points"
	LocaleInvalidVerifyCode                 = "invalid.invalid_verify_code"
	LocaleInvalidMiddleToken                = "invalid.invalid_middle_token"
	LocaleInvalidRemainQuota                = "invalid.invalid_remain_quota"
	LocaleFileEmpty                         = "invalid.file_empty"
	LocaleFileTooLarge                      = "invalid.file_too_large"
	LocaleFileTooLargeParam                 = "invalid.file_too_large_param"
	LocaleUnsupportedAudioFormat            = "invalid.unsupported_audio_format"
	LocaleInvalidAudioInfo                  = "invalid.invalid_audio_info"
	LocaleInvalidAudioDuration              = "invalid.invalid_audio_duration"
	LocaleOnlyDemoVoicesAllowed             = "invalid.only_demo_voices_allowed"
	LocaleTextTooLong                       = "invalid.text_too_long"
	LocaleTextTooLongParam                  = "invalid.text_too_long_param"
	LocaleInvalidPendingToken               = "invalid.invalid_pending_token"
	LocaleDailyVideoGenLimitExceeded        = "invalid.daily_video_gen_limit_exceeded"
	LocaleConcurrentVideoGenLimitExceeded   = "invalid.concurrent_video_gen_limit_exceeded"
	LocaleConcurrentLimitExceeded           = "invalid.concurrent_limit_exceeded"
	LocaleSomeExists                        = "invalid.some_exists"
	LocaleSomeNotExists                     = "invalid.some_not_exists"
	LocaleNotAllowDelete                    = "invalid.not_allow_delete"
	LocaleInvalidAudioFile                  = "invalid.invalid_audio_file"
	LocaleEmptyText                         = "invalid.empty_text"
	LocaleInvalidText                       = "invalid.invalid_text"
	LocaleTextContainsUnsupportedCharacters = "invalid.text_contains_unsupported_characters"
	LocaleAccountInArrears                  = "invalid.account_in_arrears"
	LocaleInvalidInviteCode                 = "invalid.invalid_invite_code"
	LocaleGenerateInviteCodeFailed          = "invalid.generate_invite_code_failed"
	LocaleVoiceSlotInactive                 = "invalid.voice_slot_inactive"
	LocaleNoAvailableVoiceSlots             = "invalid.no_available_voice_slots"
	LocaleExceedReselectVoiceSlotsLimit     = "invalid.exceed_reselect_voice_slots_limit"
	LocaleNoEmptyVoiceSlots                 = "invalid.no_empty_voice_slots"
	LocaleVoiceUnavailable                  = "invalid.voice_unavailable"
	LocaleModelNotFound                     = "invalid.model_not_found"
	LocaleModelUnsupportedCapability        = "invalid.model_unsupported_capability"
	LocaleModelMissing                      = "invalid.model_missing"
	LocaleModelCallFailed                   = "invalid.model_call_failed"
	LocaleInvalidParam                      = "invalid.invalid_param"
	LocaleInvalidEmail                      = "invalid.invalid_email"
	LocaleSeckillActivityNotStarted         = "invalid.seckill_activity_not_started"

	LocaleInvalidTradeType                              = "invalid.invalid_trade_type"
	LocaleInvalidTradeMethod                            = "invalid.invalid_trade_method"
	LocaleInvalidSubMonths                              = "invalid.invalid_sub_months"
	LocaleSubscribeNotFound                             = "invalid.subscribe_not_found"
	LocaleTradeRecordNotFound                           = "invalid.trade_record_not_found"
	LocaleSubscribePriceNotFound                        = "invalid.subscribe_price_not_found"
	LocaleResourcePackageNotFound                       = "invalid.resource_package_not_found"
	LocaleSpreadPriceCalcFailed                         = "invalid.spread_price_calc_failed"
	LocaleSubscribeLevelLtCurrentLevel                  = "invalid.subscribe_level_lt_current_level"
	LocaleSubscribePlanIsDisabled                       = "invalid.subscribe_plan_is_disabled"
	LocaleTradeStatusNotPending                         = "invalid.trade_status_not_pending"
	LocaleOnlySuccessfulTradeCanBeRefund                = "invalid.only_successful_trade_can_be_refund"
	LocaleOnlyTradeRefundOfRechargeType                 = "invalid.only_trade_refund_of_recharge_type"
	LocaleTradeRefundNotAllowed                         = "invalid.trade_refund_not_allowed"
	LocaleTradeSubscribeRefundNotAllowed                = "invalid.trade_subscribe_refund_not_allowed"
	LocaleTradeSubscribeNotLastCycle                    = "invalid.trade_subscribe_not_last_cycle"
	LocaleTradeSubscribePlanExpired                     = "invalid.trade_subscribe_plan_expired"
	LocaleTransactionTypeNotSupportRefund               = "invalid.transaction_type_not_support_refund"
	LocaleTradeSubscribeRefundExceedGracePeriod         = "invalid.trade_subscribe_refund_exceed_grace_period"
	LocaleTradeSubscribeRefundHasUsageWithinGracePeriod = "invalid.trade_subscribe_refund_has_usage_within_grace_period"
	LocaleTradeRefundAmountExceedPaidAmount             = "invalid.trade_refund_amount_exceed_paid_amount"
	LocaleTradeSubscribeUpgradeNeedManualRefund         = "invalid.trade_subscribe_upgrade_need_manual_refund"
	LocaleRefundAmountMustGtZero                        = "invalid.refund_amount_must_gt_zero"

	LocaleCertFailed                         = "invalid.cert_failed"
	LocaleCertEnterpriseAlreadyCompleted     = "invalid.cert_enterprise_already_completed"
	LocaleCertPersonalAlreadyCompleted       = "invalid.cert_personal_already_completed"
	LocaleCertProcessingExists               = "invalid.cert_processing_exists"
	LocaleCertEnterpriseLicenseVerifyFailed  = "invalid.cert_enterprise_license_verify_failed"
	LocaleCertEnterpriseNotAllowModify       = "invalid.cert_enterprise_not_allow_modify"
	LocaleCertEnterpriseLicenseNotFound      = "invalid.cert_enterprise_license_not_found"
	LocaleCertEnterpriseTransferCanAudit     = "invalid.cert_enterprise_transfer_can_audit"
	LocaleCertEnterpriseTransferNotWaitAudit = "invalid.cert_enterprise_transfer_not_wait_audit"
	LocaleCertIDCardBindOverLimit            = "invalid.cert_id_card_bind_over_limit"
	LocaleCertEnterpriseLicenseBindOverLimit = "invalid.cert_enterprise_license_bind_over_limit"
	LocaleCertEnterpriseLicenseImageInvalid  = "invalid.cert_enterprise_license_image_invalid"

	LocaleUnauthorizedDefault            = "unauthorized.default"
	LocaleUnauthorizedUserDisabled       = "unauthorized.user_disabled"
	LocaleUnauthorizedUserPendingDelete  = "unauthorized.user_pending_delete"
	LocaleUnauthorizedUserDeleted        = "unauthorized.user_deleted"
	LocaleUnauthorizedSessionExpired     = "unauthorized.session_expired"
	LocaleUnauthorizedAbnormalUserStatus = "unauthorized.abnormal_user_status"

	LocaleForbiddenDefault       = "forbidden.default"
	LocaleForbiddenNoPermission  = "forbidden.no_permission"
	LocaleForbiddenNoAccess      = "forbidden.no_access"
	LocaleForbiddenNoVoiceAccess = "forbidden.no_voice_access"
	LocaleForbiddenNeedCert      = "forbidden.need_cert"

	LocaleNotFoundDefault               = "notfound.default"
	LocaleNotFoundRoomNotExists         = "notfound.room_not_exists"
	LocaleNotFoundConversationNotExists = "notfound.conversation_not_exists"
	LocaleNotFoundAPIKeyNotExists       = "notfound.api_key_not_exists"
	LocaleNotFoundVoiceNotExists        = "notfound.voice_not_exists"
	LocaleNotFoundVoicePlanNotExists    = "notfound.voice_plan_not_exists"
	LocaleNotFoundUserNotExists         = "notfound.user_not_exists"
	LocaleNotFoundVoiceSlotNotExists    = "notfound.voice_slot_not_exists"

	LocaleConflictDefault                = "conflict.default"
	LocaleConflictSomeVoicesAreMissing   = "conflict.some_voices_are_missing"
	LocaleConflictVoicePlanAlreadyExists = "conflict.voice_plan_already_exists"
	LocaleConflictPhoneAlreadyRegistered = "conflict.phone_already_registered"

	LocaleGoneDefault                              = "gone.default"
	LocaleGoneShouldNotGrantVoicePlanIndividually  = "gone.should_not_grant_voice_plan_individually"
	LocaleGoneShouldNotRevokeVoicePlanIndividually = "gone.should_not_revoke_voice_plan_individually"

	LocaleTooManyRequestsDefault = "too_many_requests.default"
	LocaleTooManyPreservePoints  = "too_many_requests.too_many_preserve_points"

	LocaleInternalDefault = "internal.default"

	LocaleBannerTimeConflictNewUser          = "invalid.banner_time_conflict_new_user"
	LocaleBannerTimeConflictRegularUser      = "invalid.banner_time_conflict_normal_user"
	LocaleSysNotificationTimeConflictNewUser = "invalid.notification_time_conflict_new_user"
	LocaleBannerNotFound                     = "notfound.banner_not_found"
	LocaleBannerAlreadyPublished             = "conflict.banner_already_published"
	LocaleBannerAlreadyUnpublished           = "conflict.banner_already_unpublished"
)

// AppError is the unified application error.
//
// Code is a public, stable, numeric business code for frontend.
// DetailCode is a detailed numeric code for backend logs and troubleshooting.
// Reason is a stable backend-readable reason string, not intended for frontend display.
// LocaleKey is used by locale files such as locale.zh.toml.
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

func newAppError(code Code, localeKey string) *AppError {
	return NewAppError(
		code,
		code,
		defaultReason(code),
		defaultMessage(code),
		localeKey,
		nil,
		getStatusCode(code),
	)
}

func newAppErrorData(code Code, localeKey string, data any) *AppError {
	err := newAppError(code, localeKey)
	err.LocaleArgs = data
	return err
}

func getStatusCode(code Code) int {
	if code == RCOK {
		return http.StatusOK
	}

	return int(code / 1000)
}

func defaultReason(code Code) string {
	switch getStatusCode(code) {
	case http.StatusBadRequest:
		return "INVALID_REQUEST"
	case http.StatusUnauthorized:
		return "UNAUTHORIZED"
	case http.StatusForbidden:
		return "FORBIDDEN"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusConflict:
		return "CONFLICT"
	case http.StatusGone:
		return "GONE"
	case http.StatusTooManyRequests:
		return "TOO_MANY_REQUESTS"
	case http.StatusInternalServerError:
		return "INTERNAL_SERVER_ERROR"
	default:
		return "APP_ERROR"
	}
}

func defaultMessage(code Code) string {
	switch getStatusCode(code) {
	case http.StatusBadRequest:
		return "Invalid request"
	case http.StatusUnauthorized:
		return "Unauthorized"
	case http.StatusForbidden:
		return "Forbidden"
	case http.StatusNotFound:
		return "Resource not found"
	case http.StatusConflict:
		return "Resource conflict"
	case http.StatusGone:
		return "Resource gone"
	case http.StatusTooManyRequests:
		return "Too many requests"
	case http.StatusInternalServerError:
		return "Internal server error"
	default:
		return "Application error"
	}
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
			Code:       RCInternal,
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
			"code":       RCInternal,
			"detailCode": RCInternal,
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

// WithDetail returns a copied AppError with a more detailed backend-only code and reason.
// It keeps public Code and LocaleKey unchanged.
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
	ErrInvalid                           = newAppError(RCInvalid, LocaleInvalidDefault)
	ErrInsufficientPoints                = newAppError(RCInsufficientPoints, LocaleInsufficientPoints)
	ErrInvalidVerifyCode                 = newAppError(RCInvalidVerifyCode, LocaleInvalidVerifyCode)
	ErrInvalidMiddleToken                = newAppError(RCInvalidMiddleToken, LocaleInvalidMiddleToken)
	ErrInvalidRemainQuota                = newAppError(RCInvalidRemainQuota, LocaleInvalidRemainQuota)
	ErrFileEmpty                         = newAppError(RCFileEmpty, LocaleFileEmpty)
	ErrFileTooLarge                      = newAppError(RCFileTooLarge, LocaleFileTooLarge)
	ErrUnsupportedAudioFormat            = newAppError(RCUnsupportedAudioFormat, LocaleUnsupportedAudioFormat)
	ErrInvalidAudioInfo                  = newAppError(RCInvalidAudioInfo, LocaleInvalidAudioInfo)
	ErrInvalidAudioDuration              = newAppError(RCInvalidAudioDuration, LocaleInvalidAudioDuration)
	ErrOnlyDemoVoicesAllowed             = newAppError(RCOnlyDemoVoicesAllowed, LocaleOnlyDemoVoicesAllowed)
	ErrTextTooLong                       = newAppError(RCTextTooLong, LocaleTextTooLong)
	ErrInvalidPendingToken               = newAppError(RCInvalidPendingToken, LocaleInvalidPendingToken)
	ErrDailyVideoGenLimitExceeded        = newAppError(RCDailyVideoGenLimitExceeded, LocaleDailyVideoGenLimitExceeded)
	ErrConcurrentVideoGenLimitExceeded   = newAppError(RCConcurrentVideoGenLimitExceeded, LocaleConcurrentVideoGenLimitExceeded)
	ErrConcurrentLimitExceeded           = newAppError(RCConcurrentLimitExceeded, LocaleConcurrentLimitExceeded)
	ErrSomeExists                        = newAppError(RCSomeExists, LocaleSomeExists)
	ErrSomeNotExists                     = newAppError(RCSomeNotExists, LocaleSomeNotExists)
	ErrNotAllowDelete                    = newAppError(RCNotAllowDelete, LocaleNotAllowDelete)
	ErrInvalidAudioFile                  = newAppError(RCInvalidAudioFile, LocaleInvalidAudioFile)
	ErrEmptyText                         = newAppError(RCEmptyText, LocaleEmptyText)
	ErrInvalidText                       = newAppError(RCInvalidText, LocaleInvalidText)
	ErrTextContainsUnsupportedCharacters = newAppError(RCTextContainsUnsupportedCharacters, LocaleTextContainsUnsupportedCharacters)
	ErrAccountInArrears                  = newAppError(RCAccountInArrears, LocaleAccountInArrears)

	ErrInvalidTradeType                              = newAppError(RCInvalidTradeType, LocaleInvalidTradeType)
	ErrInvalidTradeMethod                            = newAppError(RCInvalidTradeMethod, LocaleInvalidTradeMethod)
	ErrInvalidSubMonths                              = newAppError(RCInvalidSubMonths, LocaleInvalidSubMonths)
	ErrSubscribeNotFound                             = newAppError(RCSubscribeNotFound, LocaleSubscribeNotFound)
	ErrTradeRecordNotFound                           = newAppError(RCTradeRecordNotFound, LocaleTradeRecordNotFound)
	ErrSubscribePriceNotFound                        = newAppError(RCSubscribePriceNotFound, LocaleSubscribePriceNotFound)
	ErrResourcePackageNotFound                       = newAppError(RCResourcePackageNotFound, LocaleResourcePackageNotFound)
	ErrSpreadPriceCalcFailed                         = newAppError(RCSpreadPriceCalcFailed, LocaleSpreadPriceCalcFailed)
	ErrSubscribeLevelLtCurrentLevel                  = newAppError(RCSubscribeLevelLtCurrentLevel, LocaleSubscribeLevelLtCurrentLevel)
	ErrSubscribePlanIsDisabled                       = newAppError(RCSubscribePlanIsDisabled, LocaleSubscribePlanIsDisabled)
	ErrTradeStatusNotPending                         = newAppError(RCTradeStatusNotPending, LocaleTradeStatusNotPending)
	ErrOnlySuccessfulTradeCanBeRefund                = newAppError(RCOnlySuccessfulTradeCanBeRefund, LocaleOnlySuccessfulTradeCanBeRefund)
	ErrOnlyTradeRefundOfRechargeType                 = newAppError(RCOnlyTradeRefundOfRechargeType, LocaleOnlyTradeRefundOfRechargeType)
	ErrTradeRefundNotAllowed                         = newAppError(RCTradeRefundNotAllowed, LocaleTradeRefundNotAllowed)
	ErrTradeSubscribeRefundNotAllowed                = newAppError(RCTradeSubscribeRefundNotAllowed, LocaleTradeSubscribeRefundNotAllowed)
	ErrTradeSubscribeNotLastCycle                    = newAppError(RCTradeSubscribeNotLastCycle, LocaleTradeSubscribeNotLastCycle)
	ErrTradeSubscribePlanExpired                     = newAppError(RCTradeSubscribePlanExpired, LocaleTradeSubscribePlanExpired)
	ErrTransactionTypeNotSupportRefund               = newAppError(RCTransactionTypeNotSupportRefund, LocaleTransactionTypeNotSupportRefund)
	ErrTradeSubscribeRefundExceedGracePeriod         = newAppError(RCTradeSubscribeRefundExceedGracePeriod, LocaleTradeSubscribeRefundExceedGracePeriod)
	ErrTradeSubscribeRefundHasUsageWithinGracePeriod = newAppError(RCTradeSubscribeRefundHasUsageWithinGracePeriod, LocaleTradeSubscribeRefundHasUsageWithinGracePeriod)
	ErrTradeRefundAmountExceedPaidAmount             = newAppError(RCTradeRefundAmountExceedPaidAmount, LocaleTradeRefundAmountExceedPaidAmount)
	ErrTradeSubscribeUpgradeNeedManualRefund         = newAppError(RCTradeSubscribeUpgradeNeedManualRefund, LocaleTradeSubscribeUpgradeNeedManualRefund)
	ErrRefundAmountMustGtZero                        = newAppError(RCRefundAmountMustGtZero, LocaleRefundAmountMustGtZero)

	ErrCertFailed                         = newAppError(RCCertFailed, LocaleCertFailed)
	ErrCertEnterpriseAlreadyCompleted     = newAppError(RCCertEnterpriseAlreadyCompleted, LocaleCertEnterpriseAlreadyCompleted)
	ErrCertPersonalAlreadyCompleted       = newAppError(RCCertPersonalAlreadyCompleted, LocaleCertPersonalAlreadyCompleted)
	ErrCertProcessingExists               = newAppError(RCCertProcessingExists, LocaleCertProcessingExists)
	ErrCertEnterpriseLicenseVerifyFailed  = newAppError(RCCertEnterpriseLicenseVerifyFailed, LocaleCertEnterpriseLicenseVerifyFailed)
	ErrCertEnterpriseNotAllowModify       = newAppError(RCCertEnterpriseNotAllowModify, LocaleCertEnterpriseNotAllowModify)
	ErrCertEnterpriseLicenseNotFound      = newAppError(RCCertEnterpriseLicenseNotFound, LocaleCertEnterpriseLicenseNotFound)
	ErrCertEnterpriseTransferCanAudit     = newAppError(RCCertEnterpriseTransferCanAudit, LocaleCertEnterpriseTransferCanAudit)
	ErrCertEnterpriseTransferNotWaitAudit = newAppError(RCCertEnterpriseTransferNotWaitAudit, LocaleCertEnterpriseTransferNotWaitAudit)
	ErrCertIDCardBindOverLimit            = newAppError(RCCertIDCardBindOverLimit, LocaleCertIDCardBindOverLimit)
	ErrCertEnterpriseLicenseBindOverLimit = newAppError(RCCertEnterpriseLicenseBindOverLimit, LocaleCertEnterpriseLicenseBindOverLimit)
	ErrCertEnterpriseLicenseImageInvalid  = newAppError(RCCertEnterpriseLicenseImageInvalid, LocaleCertEnterpriseLicenseImageInvalid)

	ErrInvalidUserInviteCode         = newAppError(RCInvalidUserInviteCode, LocaleInvalidInviteCode)
	ErrGenerateInviteCodeFailed      = newAppError(RCGenerateInviteCodeFailed, LocaleGenerateInviteCodeFailed)
	ErrVoiceSlotInactive             = newAppError(RCVoiceSlotInactive, LocaleVoiceSlotInactive)
	ErrNoAvailableVoiceSlots         = newAppError(RCNoAvailableVoiceSlots, LocaleNoAvailableVoiceSlots)
	ErrExceedReselectVoiceSlotsLimit = newAppError(RCExceedReselectVoiceSlotsLimit, LocaleExceedReselectVoiceSlotsLimit)
	ErrNoEmptyVoiceSlots             = newAppError(RCNoEmptyVoiceSlots, LocaleNoEmptyVoiceSlots)
	ErrVoiceUnavailable              = newAppError(RCVoiceUnavailable, LocaleVoiceUnavailable)
	ErrModelNotFound                 = newAppError(RCModelNotFound, LocaleModelNotFound)
	ErrModelUnsupportedCapability    = newAppError(RCModelUnsupportedCapability, LocaleModelUnsupportedCapability)
	ErrModelMissing                  = newAppError(RCModelMissing, LocaleModelMissing)
	ErrModelCallFailed               = newAppError(RCModelCallFailed, LocaleModelCallFailed)
	ErrInvalidEmail                  = newAppError(RCInvalidEmail, LocaleInvalidEmail)
	ErrSeckillActivityNotStart       = newAppError(RCSeckillActivityNotStarted, LocaleSeckillActivityNotStarted)
)

var (
	ErrUnauthorized       = newAppError(RCUnauthorized, LocaleUnauthorizedDefault)
	ErrUserDisabled       = newAppError(RCUserDisabled, LocaleUnauthorizedUserDisabled)
	ErrUserPendingDelete  = newAppError(RCUserPendingDelete, LocaleUnauthorizedUserPendingDelete)
	ErrUserDeleted        = newAppError(RCUserDeleted, LocaleUnauthorizedUserDeleted)
	ErrSessionExpired     = newAppError(RCSessionExpired, LocaleUnauthorizedSessionExpired)
	ErrAbnormalUserStatus = newAppError(RCAbnormalUserStatus, LocaleUnauthorizedAbnormalUserStatus)

	ErrInvalidToken = newAppError(RCInvalidToken, LocaleUnauthorizedSessionExpired).WithDetail(
		RCInvalidToken,
		"AUTH_INVALID_TOKEN",
		"Invalid token",
	)
	ErrInvalidTokenMalformed = ErrInvalidToken.WithDetail(
		RCInvalidTokenMalformed,
		"AUTH_TOKEN_MALFORMED",
		"Token is malformed",
	)
	ErrInvalidTokenSignatureInvalid = ErrInvalidToken.WithDetail(
		RCInvalidTokenSignatureInvalid,
		"AUTH_TOKEN_SIGNATURE_INVALID",
		"Token signature validation failed",
	)
	ErrInvalidTokenUserNotFound = ErrInvalidToken.WithDetail(
		RCInvalidTokenUserNotFound,
		"AUTH_TOKEN_USER_NOT_FOUND",
		"Token related user not found",
	)
	ErrInvalidPassword = newAppError(RCInvalidPassword, LocaleInvalidDefault).WithDetail(
		RCInvalidPassword,
		"AUTH_INVALID_PASSWORD",
		"Invalid account or password",
	)
	ErrLoginUserNotFound = ErrInvalidPassword.WithDetail(
		RCLoginUserNotFound,
		"AUTH_LOGIN_USER_NOT_FOUND",
		"Login user not found",
	)
	ErrLoginPasswordMismatch = ErrInvalidPassword.WithDetail(
		RCLoginPasswordMismatch,
		"AUTH_LOGIN_PASSWORD_MISMATCH",
		"Login password does not match",
	)
	ErrLoginUserDisabled = ErrInvalidPassword.WithDetail(
		RCLoginUserDisabled,
		"AUTH_LOGIN_USER_DISABLED",
		"Login user has been disabled",
	).WithLocale(LocaleUnauthorizedUserDisabled, nil)

	ErrSeckillTokenInvalid = newAppError(RCSeckillTokenInvalid, LocaleUnauthorizedSessionExpired).WithDetail(
		RCSeckillTokenInvalid,
		"SECKILL_TOKEN_INVALID",
		"Seckill token invalid or expired",
	)
	ErrSeckillTokenMalformed = ErrSeckillTokenInvalid.WithDetail(
		RCSeckillTokenMalformed,
		"SECKILL_TOKEN_MALFORMED",
		"Seckill token is malformed",
	)
	ErrSeckillTokenExpired = ErrSeckillTokenInvalid.WithDetail(
		RCSeckillTokenExpired,
		"SECKILL_TOKEN_EXPIRED",
		"Seckill token has expired",
	)
	ErrSeckillTokenActivityMismatch = ErrSeckillTokenInvalid.WithDetail(
		RCSeckillTokenActivityMismatch,
		"SECKILL_TOKEN_ACTIVITY_MISMATCH",
		"Seckill token activity does not match",
	)
)

var (
	ErrForbidden     = newAppError(RCForbidden, LocaleForbiddenDefault)
	ErrNoPermission  = newAppError(RCNoPermission, LocaleForbiddenNoPermission)
	ErrNoAccess      = newAppError(RCNoAccess, LocaleForbiddenNoAccess)
	ErrNoVoiceAccess = newAppError(RCNoVoiceAccess, LocaleForbiddenNoVoiceAccess)
	ErrNeedCert      = newAppError(RCNeedCert, LocaleForbiddenNeedCert)

	ErrMallProductDisabled = newAppError(RCMallProductDisabled, LocaleForbiddenNoAccess).WithDetail(
		RCMallProductDisabled,
		"MALL_PRODUCT_DISABLED",
		"Mall product has been disabled",
	)
	ErrMallSkuDisabled = newAppError(RCMallSkuDisabled, LocaleForbiddenNoAccess).WithDetail(
		RCMallSkuDisabled,
		"MALL_SKU_DISABLED",
		"Mall SKU has been disabled",
	)
)

var (
	ErrNotFound              = newAppError(RCNotFound, LocaleNotFoundDefault)
	ErrRoomNotExists         = newAppError(RCRoomNotExists, LocaleNotFoundRoomNotExists)
	ErrConversationNotExists = newAppError(RCConversationNotExists, LocaleNotFoundConversationNotExists)
	ErrAPIKeyNotExists       = newAppError(RCApiKeyNotExists, LocaleNotFoundAPIKeyNotExists)
	ErrVoiceNotExists        = newAppError(RCVoiceNotExists, LocaleNotFoundVoiceNotExists)
	ErrVoicePlanNotExists    = newAppError(RCVoicePlanNotExists, LocaleNotFoundVoicePlanNotExists)
	ErrUserNotExists         = newAppError(RCUserNotExists, LocaleNotFoundUserNotExists)
	ErrUserNotFound          = ErrUserNotExists
	ErrVoiceSlotNotExists    = newAppError(RCVoiceSlotNotExists, LocaleNotFoundVoiceSlotNotExists)

	ErrSeckillActivityNotFound = newAppError(RCSeckillActivityNotFound, LocaleNotFoundDefault).WithDetail(
		RCSeckillActivityNotFound,
		"SECKILL_ACTIVITY_NOT_FOUND",
		"Seckill activity not found",
	)
	ErrSeckillSkuNotFound = newAppErrorData(RCSeckillSkuNotFound, LocaleSomeNotExists, "商品").WithDetail(
		RCSeckillSkuNotFound,
		"SECKILL_SKU_NOT_FOUND",
		"Seckill SKU not found",
	)
	ErrSeckillOrderNotFound = newAppErrorData(RCSeckillOrderNotFound, LocaleSomeNotExists, "订单").WithDetail(
		RCSeckillOrderNotFound,
		"SECKILL_ORDER_NOT_FOUND",
		"Seckill order not found",
	)
	ErrMallProductNotFound = newAppErrorData(RCMallProductNotFound, LocaleSomeNotExists, "商品").WithDetail(
		RCMallProductNotFound,
		"MALL_PRODUCT_NOT_FOUND",
		"Mall product not found",
	)
	ErrMallSkuNotFound = newAppErrorData(RCMallSkuNotFound, LocaleSomeNotExists, "商品规格").WithDetail(
		RCMallSkuNotFound,
		"MALL_SKU_NOT_FOUND",
		"Mall SKU not found",
	)
	ErrMallOrderNotFound = newAppErrorData(RCMallOrderNotFound, LocaleSomeNotExists, "订单").WithDetail(
		RCMallOrderNotFound,
		"MALL_ORDER_NOT_FOUND",
		"Mall order not found",
	)
	ErrDatabaseRecordNotFound = newAppError(RCDatabaseRecordNotFound, LocaleNotFoundDefault).WithDetail(
		RCDatabaseRecordNotFound,
		"DATABASE_RECORD_NOT_FOUND",
		"Database record not found",
	)
	ErrBannerNotFound = newAppError(RCBannerNotFound, LocaleBannerNotFound)
)

var (
	ErrConflict                           = newAppError(RCConflict, LocaleConflictDefault)
	ErrBannerTimeConflictNewUser          = newAppError(RCBannerTimeConflictNewUser, LocaleBannerTimeConflictNewUser)
	ErrBannerTimeConflictRegularUser      = newAppError(RCBannerTimeConflictRegularUser, LocaleBannerTimeConflictRegularUser)
	ErrSysNotificationTimeConflictNewUser = newAppError(RCSysNotificationTimeConflictNewUser, LocaleSysNotificationTimeConflictNewUser)
	ErrSomeVoicesAreMissing               = newAppError(RCSomeVoicesAreMissing, LocaleConflictSomeVoicesAreMissing)
	ErrVoicePlanAlreadyExists             = newAppError(RCVoicePlanAlreadyExists, LocaleConflictVoicePlanAlreadyExists)
	ErrPhoneAlreadyRegistered             = newAppError(RCPhoneAlreadyRegistered, LocaleConflictPhoneAlreadyRegistered)

	ErrSeckillStockNotEnough = newAppError(RCSeckillStockNotEnough, LocaleConflictDefault).WithDetail(
		RCSeckillStockNotEnough,
		"SECKILL_STOCK_NOT_ENOUGH",
		"Seckill stock not enough",
	)
	ErrSeckillStockDeductFailed = ErrSeckillStockNotEnough.WithDetail(
		RCSeckillStockDeductFailed,
		"SECKILL_STOCK_DEDUCT_FAILED",
		"Failed to deduct seckill stock",
	)
	ErrSeckillRedisStockMissing = ErrSeckillStockNotEnough.WithDetail(
		RCSeckillRedisStockMissing,
		"SECKILL_REDIS_STOCK_MISSING",
		"Redis seckill stock is missing",
	)
	ErrSeckillAlreadyPurchased = newAppError(RCSeckillAlreadyPurchased, LocaleConflictDefault).WithDetail(
		RCSeckillAlreadyPurchased,
		"SECKILL_ALREADY_PURCHASED",
		"Already purchased this seckill item",
	)
	ErrSeckillDuplicateSubmit = ErrSeckillAlreadyPurchased.WithDetail(
		RCSeckillDuplicateSubmit,
		"SECKILL_DUPLICATE_SUBMIT",
		"Duplicate seckill submit",
	)
	ErrMallStockNotEnough = newAppError(RCMallStockNotEnough, LocaleConflictDefault).WithDetail(
		RCMallStockNotEnough,
		"MALL_STOCK_NOT_ENOUGH",
		"Mall stock not enough",
	)
	ErrMallStockDeductFailed = ErrMallStockNotEnough.WithDetail(
		RCMallStockDeductFailed,
		"MALL_STOCK_DEDUCT_FAILED",
		"Failed to deduct mall stock",
	)
	ErrMallStockRollbackFailed = ErrMallStockNotEnough.WithDetail(
		RCMallStockRollbackFailed,
		"MALL_STOCK_ROLLBACK_FAILED",
		"Failed to rollback mall stock",
	)
	ErrMallInvalidOrderTransition = newAppError(RCMallInvalidOrderTransition, LocaleConflictDefault).WithDetail(
		RCMallInvalidOrderTransition,
		"MALL_INVALID_ORDER_TRANSITION",
		"Invalid order status transition",
	)
	ErrMallOrderStatusMismatch = ErrMallInvalidOrderTransition.WithDetail(
		RCMallOrderStatusMismatch,
		"MALL_ORDER_STATUS_MISMATCH",
		"Order status does not match",
	)
	ErrMallOrderAlreadyPaid = ErrMallInvalidOrderTransition.WithDetail(
		RCMallOrderAlreadyPaid,
		"MALL_ORDER_ALREADY_PAID",
		"Order has already been paid",
	)
	ErrMallOrderCannotCancel = newAppError(RCMallOrderCannotCancel, LocaleConflictDefault).WithDetail(
		RCMallOrderCannotCancel,
		"MALL_ORDER_CANNOT_CANCEL",
		"Order cannot be cancelled",
	)
	ErrMallDuplicateOrder = newAppError(RCMallDuplicateOrder, LocaleConflictDefault).WithDetail(
		RCMallDuplicateOrder,
		"MALL_DUPLICATE_ORDER",
		"Duplicate order request",
	)
	ErrMallDuplicateIdempotencyKey = ErrMallDuplicateOrder.WithDetail(
		RCMallDuplicateIdempotencyKey,
		"MALL_DUPLICATE_IDEMPOTENCY_KEY",
		"Duplicate idempotency key",
	)
	ErrMallDuplicatePendingOrder = ErrMallDuplicateOrder.WithDetail(
		RCMallDuplicatePendingOrder,
		"MALL_DUPLICATE_PENDING_ORDER",
		"Duplicate pending order exists",
	)

	ErrBannerAlreadyPublished   = newAppError(RCBannerAlreadyPublished, LocaleBannerAlreadyPublished)
	ErrBannerAlreadyUnpublished = newAppError(RCBannerAlreadyUnpublished, LocaleBannerAlreadyUnpublished)
)

var (
	ErrGone                                 = newAppError(RCGone, LocaleGoneDefault)
	ErrShouldNotGrantVoicePlanIndividually  = newAppError(RCShouldNotGrantVoicePlanIndividually, LocaleGoneShouldNotGrantVoicePlanIndividually)
	ErrShouldNotRevokeVoicePlanIndividually = newAppError(RCShouldNotRevokeVoicePlanIndividually, LocaleGoneShouldNotRevokeVoicePlanIndividually)
	ErrSeckillActivityEnded                 = newAppError(RCSeckillActivityEnded, LocaleGoneDefault).WithDetail(RCSeckillActivityEnded, "SECKILL_ACTIVITY_ENDED", "Seckill activity has ended")
	ErrMallProductDeleted                   = newAppError(RCMallProductDeleted, LocaleGoneDefault).WithDetail(RCMallProductDeleted, "MALL_PRODUCT_DELETED", "Mall product has been deleted")
	ErrMallSkuDeleted                       = newAppError(RCMallSkuDeleted, LocaleGoneDefault).WithDetail(RCMallSkuDeleted, "MALL_SKU_DELETED", "Mall SKU has been deleted")
	ErrMallOrderAlreadyCancelled            = newAppError(RCMallOrderAlreadyCancelled, LocaleGoneDefault).WithDetail(RCMallOrderAlreadyCancelled, "MALL_ORDER_ALREADY_CANCELLED", "Order has already been cancelled")
)

var (
	ErrTooManyRequests       = newAppError(RCTooManyRequests, LocaleTooManyRequestsDefault)
	ErrTooManyPreservePoints = newAppError(RCTooManyPreservePoints, LocaleTooManyPreservePoints)
	ErrCodeAttemptsTooMany   = newAppError(RCVerifyCodeAttemptsTooMany, LocaleTooManyRequestsDefault).WithDetail(RCVerifyCodeAttemptsTooMany, "VERIFY_CODE_ATTEMPTS_TOO_MANY", "Too many verification code attempts")
	ErrLimitExceeded         = newAppError(RCRateLimitExceeded, LocaleTooManyRequestsDefault).WithDetail(RCRateLimitExceeded, "RATE_LIMIT_EXCEEDED", "Request limit exceeded")
	ErrRateIPLimited         = ErrTooManyRequests.WithDetail(RCRateIPLimited, "RATE_IP_LIMITED", "IP request has been rate limited")
	ErrRateUserLimited       = ErrTooManyRequests.WithDetail(RCRateUserLimited, "RATE_USER_LIMITED", "User request has been rate limited")
	ErrRateRouteLimited      = ErrTooManyRequests.WithDetail(RCRateRouteLimited, "RATE_ROUTE_LIMITED", "Route request has been rate limited")
	ErrRateGlobalLimited     = ErrTooManyRequests.WithDetail(RCRateGlobalLimited, "RATE_GLOBAL_LIMITED", "Global request has been rate limited")
)

var (
	ErrInternal = newAppError(RCInternal, LocaleInternalDefault).WithDetail(
		RCInternal,
		"INTERNAL_SERVER_ERROR",
		"Internal server error",
	)
	ErrSystemPanic             = ErrInternal.WithDetail(RCSystemPanic, "SYSTEM_PANIC", "System panic occurred")
	ErrSystemConfigMissing     = ErrInternal.WithDetail(RCSystemConfigMissing, "SYSTEM_CONFIG_MISSING", "System configuration is missing")
	ErrSystemConfigInvalid     = ErrInternal.WithDetail(RCSystemConfigInvalid, "SYSTEM_CONFIG_INVALID", "System configuration is invalid")
	ErrSystemDependencyFailure = ErrInternal.WithDetail(RCSystemDependencyFailure, "SYSTEM_DEPENDENCY_FAILURE", "System dependency service failure")

	ErrDatabase                  = newAppError(RCDatabaseError, LocaleInternalDefault).WithDetail(RCDatabaseError, "DATABASE_ERROR", "Database error")
	ErrDatabaseQueryFailed       = ErrDatabase.WithDetail(RCDatabaseQueryFailed, "DATABASE_QUERY_FAILED", "Database query failed")
	ErrDatabaseInsertFailed      = ErrDatabase.WithDetail(RCDatabaseInsertFailed, "DATABASE_INSERT_FAILED", "Database insert failed")
	ErrDatabaseUpdateFailed      = ErrDatabase.WithDetail(RCDatabaseUpdateFailed, "DATABASE_UPDATE_FAILED", "Database update failed")
	ErrDatabaseDeleteFailed      = ErrDatabase.WithDetail(RCDatabaseDeleteFailed, "DATABASE_DELETE_FAILED", "Database delete failed")
	ErrDatabaseTransactionFailed = ErrDatabase.WithDetail(RCDatabaseTransactionFailed, "DATABASE_TRANSACTION_FAILED", "Database transaction failed")

	ErrTokenGeneration    = newAppError(RCTokenGenerationFailed, LocaleInternalDefault).WithDetail(RCTokenGenerationFailed, "AUTH_TOKEN_GENERATION_FAILED", "Failed to generate token")
	ErrTokenSigningFailed = ErrTokenGeneration.WithDetail(RCTokenSigningFailed, "AUTH_TOKEN_SIGNING_FAILED", "Failed to sign token")
	ErrTokenSecretMissing = ErrTokenGeneration.WithDetail(RCTokenSecretMissing, "AUTH_TOKEN_SECRET_MISSING", "Token signing secret is missing")

	ErrEmailSendFailed          = newAppError(RCMailSendFailed, LocaleInternalDefault).WithDetail(RCMailSendFailed, "MAIL_SEND_FAILED", "Failed to send email")
	ErrMailSMTPAuthFailed       = ErrEmailSendFailed.WithDetail(RCMailSMTPAuthFailed, "MAIL_SMTP_AUTH_FAILED", "SMTP authentication failed")
	ErrMailSMTPConnectionFailed = ErrEmailSendFailed.WithDetail(RCMailSMTPConnectionFailed, "MAIL_SMTP_CONNECTION_FAILED", "SMTP connection failed")
	ErrMailProviderRejected     = ErrEmailSendFailed.WithDetail(RCMailProviderRejected, "MAIL_PROVIDER_REJECTED", "Mail provider rejected sending")

	ErrSeckillSubmitFailed            = newAppError(RCSeckillSubmitFailed, LocaleInternalDefault).WithDetail(RCSeckillSubmitFailed, "SECKILL_SUBMIT_FAILED", "Seckill order submit failed")
	ErrSeckillSubmitQueueFailed       = ErrSeckillSubmitFailed.WithDetail(RCSeckillSubmitQueueFailed, "SECKILL_SUBMIT_QUEUE_FAILED", "Failed to enqueue seckill order")
	ErrSeckillSubmitLockFailed        = ErrSeckillSubmitFailed.WithDetail(RCSeckillSubmitLockFailed, "SECKILL_SUBMIT_LOCK_FAILED", "Failed to lock seckill submit")
	ErrSeckillSubmitPersistenceFailed = ErrSeckillSubmitFailed.WithDetail(RCSeckillSubmitPersistenceFailed, "SECKILL_SUBMIT_PERSISTENCE_FAILED", "Failed to persist seckill order")
)

var (
	// 兼容旧命名：验证码错误
	ErrCodeInvalid = ErrInvalidVerifyCode
	ErrCodeExpired = newAppError(RCInvalidVerifyCode, LocaleInvalidVerifyCode).WithDetail(
		RCInvalidVerifyCode,
		"VERIFY_CODE_EXPIRED",
		"Verification code expired",
	)
	ErrCodeNotFound = ErrInvalidVerifyCode.WithDetail(
		RCInvalidVerifyCode,
		"VERIFY_CODE_NOT_FOUND",
		"Verification code not found",
	)
	ErrCodeMismatch = ErrInvalidVerifyCode.WithDetail(
		RCInvalidVerifyCode,
		"VERIFY_CODE_MISMATCH",
		"Verification code does not match",
	)
	ErrCodeUsed = ErrInvalidVerifyCode.WithDetail(
		RCInvalidVerifyCode,
		"VERIFY_CODE_USED",
		"Verification code has already been used",
	)

	// 兼容旧命名：认证 / 权限
	ErrPermissionDenied = ErrNoPermission
	ErrTokenExpired     = ErrSessionExpired

	// 兼容旧命名：用户
	ErrUserExists = ErrSomeExists.WithLocale(LocaleSomeExists, "用户")

	// 兼容旧命名：限流
	ErrTooManyRequestsCompat = ErrTooManyRequests
)
