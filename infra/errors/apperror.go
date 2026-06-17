package errors

import "fmt"

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
