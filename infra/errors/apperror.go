package errors

import (
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// AppError 是统一的应用错误。
type AppError struct {
	code           int64
	msgId          string
	data           interface{}
	httpStatusCode int
}

func newAppError(code int64, msgId string) *AppError {
	return newAppErrorData(code, msgId, nil)
}

func newAppErrorData(code int64, msgId string, data interface{}) *AppError {
	return &AppError{
		code:           code,
		msgId:          msgId,
		data:           data,
		httpStatusCode: getStatusCode(code),
	}
}

func getStatusCode(code int64) int {
	return int(code / 1000)
}

func (e *AppError) Error() string {
	if e == nil {
		return ""
	}

	return fmt.Sprintf("%d:%s", e.code, e.msgId)
}

// GRPCStatus 把业务错误族映射为标准 gRPC code，同时保留 code:msgId 消息供网关恢复。
// gRPC 只能在网络上传输 status，不能直接保留具体的 AppError Go 类型。
func (e *AppError) GRPCStatus() *status.Status {
	if e == nil {
		return status.New(codes.Internal, "")
	}
	return status.New(grpcCodeFromHTTPStatus(e.httpStatusCode), e.Error())
}

// grpcCodeFromHTTPStatus 使用项目现有错误码分族生成 go-zero 可识别的 gRPC code。
func grpcCodeFromHTTPStatus(httpStatus int) codes.Code {
	switch httpStatus {
	case 400:
		return codes.InvalidArgument
	case 401:
		return codes.Unauthenticated
	case 403:
		return codes.PermissionDenied
	case 404:
		return codes.NotFound
	case 409:
		return codes.AlreadyExists
	case 410:
		return codes.FailedPrecondition
	case 429:
		return codes.ResourceExhausted
	case 503:
		return codes.Unavailable
	case 504:
		return codes.DeadlineExceeded
	default:
		return codes.Internal
	}
}

// Code 返回错误码。
func (e *AppError) Code() int64 {
	if e == nil {
		return 0
	}
	return e.code
}

// MsgId 返回本地化 key。
func (e *AppError) MsgId() string {
	if e == nil {
		return ""
	}
	return e.msgId
}

// HTTPStatusCode 返回 HTTP 状态码。
func (e *AppError) HTTPStatusCode() int {
	if e == nil {
		return 500
	}
	return e.httpStatusCode
}

// Message 返回中文文案。
func (e *AppError) Message() string {
	if e == nil {
		return ""
	}
	return GetMessage(e.msgId)
}

// Data 返回附加数据。
func (e *AppError) Data() interface{} {
	if e == nil {
		return nil
	}
	return e.data
}
