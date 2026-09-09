package tools

import (
	"context"
	"fmt"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

func registerRoles(r *registry) {
	add(r, read("list_roles",
		"List all roles. Each role pairs a name (\"Admin\", \"Execute\", \"Read\", ...) with a resource. Use list_resource_roles for the roles on a single resource."),
		func(ctx context.Context, c *ascender.Client, in listArgs) (ascender.Page, error) {
			q := query(in.PageSize, 50)
			setQuery(q, "search", in.Search)
			return c.List(ctx, "roles", q)
		})

	add(r, read("list_resource_roles",
		"List the role bindings on one resource. resource_type is job_templates, inventories, projects, credentials, organizations, workflow_job_templates, teams, or instance_groups."),
		func(ctx context.Context, c *ascender.Client, in struct {
			ResourceType string `json:"resource_type"`
			ResourceID   int    `json:"resource_id"`
			PageSize     int    `json:"page_size,omitempty"`
		}) (ascender.Page, error) {
			return c.List(ctx, fmt.Sprintf("%s/%d/object_roles", in.ResourceType, in.ResourceID), query(in.PageSize, 50))
		})

	type grantArgs struct {
		SubjectID    int  `json:"subject_id" jsonschema:"user or team numeric ID"`
		RoleID       int  `json:"role_id" jsonschema:"role numeric ID (find via list_roles or list_resource_roles)"`
		Disassociate bool `json:"disassociate,omitempty" jsonschema:"true to revoke instead of grant"`
	}
	add(r, writeIdempotent("grant_user_role",
		"Grant a role to a user. Pass disassociate=true to revoke. Find the role_id with list_resource_roles (e.g. \"Execute\" on job template 42)."),
		func(ctx context.Context, c *ascender.Client, in grantArgs) (map[string]any, error) {
			return associate(ctx, c, fmt.Sprintf("users/%d/roles", in.SubjectID), in.RoleID, in.Disassociate)
		})
	add(r, writeIdempotent("grant_team_role",
		"Grant a role to a team (every member inherits it). Pass disassociate=true to revoke."),
		func(ctx context.Context, c *ascender.Client, in grantArgs) (map[string]any, error) {
			return associate(ctx, c, fmt.Sprintf("teams/%d/roles", in.SubjectID), in.RoleID, in.Disassociate)
		})
}
