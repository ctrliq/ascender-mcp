package tools

import (
	"context"
	"fmt"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

type userFields struct {
	Email           *string `json:"email,omitempty"`
	FirstName       *string `json:"first_name,omitempty"`
	LastName        *string `json:"last_name,omitempty"`
	IsSuperuser     *bool   `json:"is_superuser,omitempty"`
	IsSystemAuditor *bool   `json:"is_system_auditor,omitempty"`
}

func (f userFields) payload() map[string]any {
	p := map[string]any{}
	set(p, "email", f.Email)
	set(p, "first_name", f.FirstName)
	set(p, "last_name", f.LastName)
	set(p, "is_superuser", f.IsSuperuser)
	set(p, "is_system_auditor", f.IsSystemAuditor)
	return p
}

func registerUsers(r *registry) {
	add(r, read("list_users", "List users."), list("users"))
	add(r, read("get_user", "Get a user by numeric ID or exact username."), get("users"))

	add(r, write("create_user",
		"Create a user. The password is sent on this call only; rotate it later with set_user_password."),
		func(ctx context.Context, c *ascender.Client, in struct {
			Username string `json:"username"`
			Password string `json:"password"`
			userFields
		}) (map[string]any, error) {
			p := in.payload()
			p["username"], p["password"] = in.Username, in.Password
			return c.Post(ctx, "users", p)
		})

	add(r, writeIdempotent("update_user",
		"Partially update a user's profile. Password changes go through set_user_password so a rotation is never a side effect of editing other fields."),
		func(ctx context.Context, c *ascender.Client, in struct {
			UserID   int     `json:"user_id"`
			Username *string `json:"username,omitempty"`
			userFields
		}) (map[string]any, error) {
			p := in.payload()
			set(p, "username", in.Username)
			return c.Patch(ctx, fmt.Sprintf("users/%d", in.UserID), p)
		})

	add(r, writeIdempotent("set_user_password",
		"Rotate a user's password. Kept separate from update_user so a password change is a deliberate, single-purpose action."),
		func(ctx context.Context, c *ascender.Client, in struct {
			UserID   int    `json:"user_id"`
			Password string `json:"password"`
		}) (map[string]any, error) {
			return c.Patch(ctx, fmt.Sprintf("users/%d", in.UserID), map[string]any{"password": in.Password})
		})

	add(r, writeIdempotent("delete_user",
		"Delete a user. Jobs and audit records authored by the user stay intact."),
		func(ctx context.Context, c *ascender.Client, in struct {
			UserID int `json:"user_id"`
		}) (deleted, error) {
			return del(ctx, c, "users", in.UserID)
		})
}
