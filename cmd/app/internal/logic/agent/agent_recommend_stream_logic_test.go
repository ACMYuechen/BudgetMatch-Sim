package agent

import (
	"testing"

	apperrors "budgetmatch-sim/infra/errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestPublicStreamErrorRestoresAppError(t *testing.T) {
	data := publicStreamError(status.Error(codes.InvalidArgument, apperrors.AgentContextTooLarge.Error()))
	response, ok := data.(apperrors.HTTPResponse)
	if !ok {
		t.Fatalf("publicStreamError() type = %T, want errors.HTTPResponse", data)
	}
	if response.Code != apperrors.AgentContextTooLarge.Code() {
		t.Fatalf("publicStreamError() code = %d, want %d", response.Code, apperrors.AgentContextTooLarge.Code())
	}
	if response.Message != apperrors.AgentContextTooLarge.Message() {
		t.Fatalf("publicStreamError() message = %q, want %q", response.Message, apperrors.AgentContextTooLarge.Message())
	}
}

func TestPublicStreamErrorHidesUnknownGRPCMessage(t *testing.T) {
	data := publicStreamError(status.Error(codes.Unknown, "database password leaked"))
	response, ok := data.(apperrors.HTTPResponse)
	if !ok {
		t.Fatalf("publicStreamError() type = %T, want errors.HTTPResponse", data)
	}
	if response.Code != apperrors.Internal.Code() || response.Message != apperrors.Internal.Message() {
		t.Fatalf("publicStreamError() = %#v, want generic internal error", response)
	}
}
