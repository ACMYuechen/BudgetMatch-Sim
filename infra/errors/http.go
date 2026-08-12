package errors

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var appErrorPattern = regexp.MustCompile(`^(\d{6}):([A-Za-z0-9_.-]+)$`)

// HTTPResponse 是 REST 网关返回给客户端的统一业务错误结构。
type HTTPResponse struct {
	Code    int64       `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// HTTPErrorHandler 把本地 AppError 或经 gRPC 传输的 code:msgId 恢复为 HTTP 响应。
// 无法识别的 gRPC 错误统一收敛为 500；普通解析与校验错误保持 go-zero 的 400 语义。
func HTTPErrorHandler(err error) (int, any) {
	if appErr, ok := AsAppError(err); ok {
		return appErr.HTTPStatusCode(), responseFromAppError(appErr)
	}
	if grpcStatus, ok := status.FromError(err); ok {
		appErr := defaultAppErrorForGRPCCode(grpcStatus.Code())
		return appErr.HTTPStatusCode(), responseFromAppError(appErr)
	}
	// go-zero 生成的参数解析和 validator 错误是普通 error；保持 400 语义并返回统一结构。
	return http.StatusBadRequest, HTTPResponse{Code: Invalid.Code(), Message: err.Error()}
}

// responseFromAppError 统一组装对外 JSON，避免各入口重复选择本地化文案。
func responseFromAppError(err *AppError) HTTPResponse {
	return HTTPResponse{Code: err.Code(), Message: err.Message(), Data: err.Data()}
}

// defaultAppErrorForGRPCCode 为非业务 gRPC status 选择不泄露内部细节且状态一致的通用错误。
func defaultAppErrorForGRPCCode(code codes.Code) *AppError {
	switch code {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.OutOfRange:
		return Invalid
	case codes.Unauthenticated, codes.PermissionDenied:
		return Unauthorized
	case codes.NotFound:
		return NotFound
	case codes.AlreadyExists, codes.Aborted:
		return Conflict
	case codes.ResourceExhausted:
		return TooManyRequests
	default:
		return Internal
	}
}

// AsAppError 同时识别本地 AppError 与 gRPC status 中的 code:msgId 业务载荷。
func AsAppError(err error) (*AppError, bool) {
	if err == nil {
		return nil, false
	}
	var appErr *AppError
	if errors.As(err, &appErr) && appErr != nil {
		return appErr, true
	}
	grpcStatus, ok := status.FromError(err)
	if !ok {
		return nil, false
	}
	return parseAppError(grpcStatus.Message())
}

// parseAppError 只接受严格的六位业务码和受限 msgId，防止误解析任意内部错误文本。
func parseAppError(message string) (*AppError, bool) {
	matches := appErrorPattern.FindStringSubmatch(message)
	if len(matches) != 3 {
		return nil, false
	}
	code, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil || getStatusCode(code) < 400 || getStatusCode(code) > 599 {
		return nil, false
	}
	return newAppError(code, matches[2]), true
}
