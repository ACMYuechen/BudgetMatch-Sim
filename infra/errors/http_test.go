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
