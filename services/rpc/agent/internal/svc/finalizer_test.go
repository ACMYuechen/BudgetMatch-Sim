package svc

import (
	"context"
	"testing"

	recommendagent "budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	"budgetmatch-sim/services/rpc/mall/pb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type checkClientStub struct{}

func (checkClientStub) CheckProductCandidates(context.Context, *pb.CheckProductCandidatesReq, ...grpc.CallOption) (*pb.CheckProductCandidatesResp, error) {
	panic("construction must not call Mall")
}

func TestProductionFinalizerPolicyFollowsMallConfiguration(t *testing.T) {
	require.IsType(t, recommendagent.DemoFinalizer{}, newResultFinalizer(nil))
	require.IsType(t, &recommendagent.CandidateFinalizer{}, newResultFinalizer(checkClientStub{}))
}
