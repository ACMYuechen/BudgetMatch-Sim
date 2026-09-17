package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/infra/request"
	"budgetmatch-sim/services/rpc/auth/client/authservice"
	"budgetmatch-sim/services/rpc/auth/pb"

	"google.golang.org/grpc"
)

type validatingAuthClient struct {
	authservice.AuthService
	user  *pb.UserInfo
	err   error
	token string
}

func (client *validatingAuthClient) ValidateToken(ctx context.Context, input *pb.ValidateTokenReq, options ...grpc.CallOption) (*pb.ValidateTokenResp, error) {
	client.token = input.Token
	return &pb.ValidateTokenResp{User: client.user}, client.err
}

func TestAuthMiddlewareSuppliesVerifiedRequestIdentity(test *testing.T) {
	client := &validatingAuthClient{user: &pb.UserInfo{Id: "verified-user", Role: 100}}
	handlerCalled := false
	handler := NewAuthMiddleware(client).Handle(func(writer http.ResponseWriter, incoming *http.Request) {
		handlerCalled = true
		userID, err := request.MustUserId(incoming.Context())
		if err != nil || userID != "verified-user" {
			test.Errorf("request identity = %q, error = %v", userID, err)
		}
		if incoming.Context().Value("user_id") != "verified-user" {
			test.Error("legacy user identity was not preserved")
		}
		if interceptor.TokenFromContext(incoming.Context()) != "valid-token" {
			test.Error("RPC token propagation was not preserved")
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	incoming := httptest.NewRequest(http.MethodPost, "/api/agent/recommend", nil)
	incoming.Header.Set("Authorization", "Bearer valid-token")
	incoming = incoming.WithContext(request.WithUserId(incoming.Context(), "unverified-user"))
	response := httptest.NewRecorder()
	handler(response, incoming)
	if !handlerCalled || response.Code != http.StatusNoContent || client.token != "valid-token" {
		test.Fatalf("called = %v, status = %d, validated token = %q", handlerCalled, response.Code, client.token)
	}
}

func TestAuthMiddlewareRejectsUnverifiedIdentity(test *testing.T) {
	for _, testCase := range []struct {
		name   string
		header string
		user   *pb.UserInfo
		err    error
	}{
		{name: "missing token"},
		{name: "invalid token", header: "Bearer invalid", err: errors.New("invalid token")},
		{name: "missing user", header: "Bearer valid"},
		{name: "invalid role", header: "Bearer valid", user: &pb.UserInfo{Id: "user", Role: 200}},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			client := &validatingAuthClient{user: testCase.user, err: testCase.err}
			handler := NewAuthMiddleware(client).Handle(func(writer http.ResponseWriter, incoming *http.Request) {
				test.Error("unauthorized request reached handler")
			})
			incoming := httptest.NewRequest(http.MethodPost, "/api/agent/recommend", nil)
			incoming.Header.Set("Authorization", testCase.header)
			response := httptest.NewRecorder()
			handler(response, incoming)
			if response.Code != http.StatusUnauthorized {
				test.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
			}
		})
	}
}
