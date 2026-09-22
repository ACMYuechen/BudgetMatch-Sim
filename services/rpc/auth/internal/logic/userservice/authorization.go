package userservicelogic

import (
	"context"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/role"
	"budgetmatch-sim/services/rpc/auth/internal/interceptor"
	"budgetmatch-sim/services/rpc/auth/model/user"
)

func requireAdmin(ctx context.Context) (*user.Users, error) {
	u, _ := ctx.Value(interceptor.ContextKeyUser).(*user.Users)
	if u == nil || u.Id == "" || u.Status != user.StatusNormal || !role.IsGlobalAdminRole(int64(u.Role)) {
		return nil, errors.Unauthorized
	}
	return u, nil
}

// Only a super administrator may modify administrator accounts or assign roles.
func authorizeUserMutation(actor, target *user.Users, requestedRole int32) error {
	if actor.Role != role.RoleSuperAdmin && (role.IsGlobalAdminRole(int64(target.Role)) ||
		(requestedRole != 0 && int(requestedRole) != target.Role)) {
		return errors.Unauthorized
	}
	if requestedRole != 0 && !role.IsGlobalUserRole(int64(requestedRole)) {
		return errors.Invalid
	}
	return nil
}
