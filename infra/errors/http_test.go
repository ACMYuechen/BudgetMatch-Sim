package errors

import (
	"errors"
	"net/http"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAppErrorGRPCStatus(t *testing.T) {
	tests := []struct {
		name string
		err  *AppError
		code codes.Code
	}{
		{name: "invalid", err: AgentContextTooLarge, code: codes.InvalidArgument},
		{name: "unauthorized", err: Unauthorized, code: codes.Unauthenticated},
		{name: "not found", err: NotFound, code: codes.NotFound},
		{name: "conflict", err: Conflict, code: codes.AlreadyExists},
		{name: "rate limit", err: TooManyRequests, code: codes.ResourceExhausted},
		{name: "internal", err: Internal, code: codes.Internal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.err.GRPCStatus()
			if got.Code() != test.code || got.Message() != test.err.Error() {
				t.Fatalf("GRPCStatus() = %s/%q, want %s/%q", got.Code(), got.Message(), test.code, test.err.Error())
			}
		})
	}
}

func TestAsAppErrorRestoresGRPCError(t *testing.T) {
	transported := status.Error(codes.InvalidArgument, AgentContextTooLarge.Error())
	restored, ok := AsAppError(transported)
	if !ok {
		t.Fatal("AsAppError() did not restore transported AppError")
	}
	if restored.Code() != AgentContextTooLarge.Code() || restored.MsgId() != AgentContextTooLarge.MsgId() {
		t.Fatalf("AsAppError() = %d/%q", restored.Code(), restored.MsgId())
	}
}

func TestAgentConstraintErrorsSurviveRPCAndHTTP(t *testing.T) {
	for _, tc := range []struct {
		err     *AppError
		code    int64
		message string
	}{
		{AgentBudgetCurrency, 400007, "预算仅支持人民币，请换算后使用人民币金额或 budget_cents 重新提交"},
		{AgentBudgetText, 400008, "预算无效或存在歧义，请提供唯一的正数人民币上限，使用阿拉伯数字且精确到分，最高十亿元"},
		{AgentItemLimitText, 400009, "商品件数无效或存在歧义，请提供唯一的 1 到 10 件上限，不支持数量区间或最低件数"},
	} {
		t.Run(tc.err.MsgId(), func(t *testing.T) {
			transported := tc.err.GRPCStatus().Err()
			statusCode, body := HTTPErrorHandler(transported)
			response := body.(HTTPResponse)
			if status.Code(transported) != codes.InvalidArgument || statusCode != http.StatusBadRequest || response.Code != tc.code || response.Message != tc.message {
				t.Fatalf("constraint error lost in transport: %v %d %+v", transported, statusCode, response)
			}
		})
	}
}

func TestHTTPErrorHandler(t *testing.T) {
	statusCode, body := HTTPErrorHandler(status.Error(codes.NotFound, NotFound.Error()))
	response, ok := body.(HTTPResponse)
	if !ok {
		t.Fatalf("HTTPErrorHandler() body type = %T", body)
	}
	if statusCode != http.StatusNotFound || response.Code != NotFound.Code() || response.Message != NotFound.Message() {
		t.Fatalf("HTTPErrorHandler() = %d/%+v", statusCode, response)
	}

	statusCode, body = HTTPErrorHandler(errors.New("validation failed"))
	response = body.(HTTPResponse)
	if statusCode != http.StatusBadRequest || response.Code != Invalid.Code() || response.Message != "validation failed" {
		t.Fatalf("fallback HTTPErrorHandler() = %d/%+v", statusCode, response)
	}

	statusCode, body = HTTPErrorHandler(status.Error(codes.Unknown, "database password leaked"))
	response = body.(HTTPResponse)
	if statusCode != http.StatusInternalServerError || response.Code != Internal.Code() || response.Message != Internal.Message() {
		t.Fatalf("unknown RPC HTTPErrorHandler() = %d/%+v", statusCode, response)
	}
}

func TestHTTPErrorHandlerKeepsGenericStatusAndBodyConsistent(t *testing.T) {
	tests := []struct {
		name       string
		grpcCode   codes.Code
		httpStatus int
		appErr     *AppError
	}{
		{name: "permission denied", grpcCode: codes.PermissionDenied, httpStatus: http.StatusUnauthorized, appErr: Unauthorized},
		{name: "already exists", grpcCode: codes.AlreadyExists, httpStatus: http.StatusConflict, appErr: Conflict},
		{name: "unavailable", grpcCode: codes.Unavailable, httpStatus: http.StatusInternalServerError, appErr: Internal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			statusCode, body := HTTPErrorHandler(status.Error(test.grpcCode, "internal detail"))
			response := body.(HTTPResponse)
			if statusCode != test.httpStatus || response.Code != test.appErr.Code() || response.Message != test.appErr.Message() {
				t.Fatalf("HTTPErrorHandler() = %d/%+v, want %d/%d", statusCode, response, test.httpStatus, test.appErr.Code())
			}
		})
	}
}

func TestParseAppErrorRejectsUntrustedMessages(t *testing.T) {
	for _, message := range []string{"rpc failed", "400:invalid.default", "400000:invalid default", "200000:ok.default"} {
		if _, ok := parseAppError(message); ok {
			t.Fatalf("parseAppError(%q) unexpectedly succeeded", message)
		}
	}
}
