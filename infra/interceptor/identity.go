package interceptor

import (
	"context"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/role"
)

// UserScope resolves resource ownership from the authenticated identity. Only
// explicitly administrative operations may opt into cross-user access.
func UserScope(ctx context.Context, requested string, allowAdmin bool) (string, error) {
	id, ok := ctx.Value(ContextKeyUserId).(string)
	r, roleOK := ctx.Value(ContextKeyRole).(int64)
	if !ok || id == "" || !roleOK || !role.IsGlobalUserRole(r) {
		return "", errors.Unauthorized
	}
	if allowAdmin && role.IsGlobalAdminRole(r) {
		return requested, nil
	}
	if requested != "" && requested != id {
		return "", errors.Unauthorized
	}
	return id, nil
}

func RequireAdmin(ctx context.Context) error {
	if _, err := UserScope(ctx, "", false); err != nil {
		return err
	}
	r, _ := ctx.Value(ContextKeyRole).(int64)
	if !role.IsGlobalAdminRole(r) {
		return errors.Unauthorized
	}
	return nil
}
