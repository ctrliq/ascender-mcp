package tools

import (
	"context"
	"fmt"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

func registerOrganizations(r *registry) {
	add(r, read("list_organizations",
		"List organizations. Every inventory, project, template, and credential belongs to one."),
		list("organizations"))
	add(r, read("get_organization", "Get an organization by numeric ID or exact name."), get("organizations"))

	add(r, write("create_organization", "Create an organization. max_hosts=0 means unlimited."),
		func(ctx context.Context, c *ascender.Client, in struct {
			Name        string `json:"name"`
			Description string `json:"description,omitempty"`
			MaxHosts    int    `json:"max_hosts,omitempty"`
		}) (map[string]any, error) {
			p := map[string]any{"name": in.Name}
			setIf(p, "description", in.Description)
			setIf(p, "max_hosts", in.MaxHosts)
			return c.Post(ctx, "organizations", p)
		})

	add(r, writeIdempotent("update_organization", "Partially update an organization."),
		func(ctx context.Context, c *ascender.Client, in struct {
			OrganizationID int     `json:"organization_id"`
			Name           *string `json:"name,omitempty"`
			Description    *string `json:"description,omitempty"`
			MaxHosts       *int    `json:"max_hosts,omitempty"`
		}) (map[string]any, error) {
			p := map[string]any{}
			set(p, "name", in.Name)
			set(p, "description", in.Description)
			set(p, "max_hosts", in.MaxHosts)
			return c.Patch(ctx, fmt.Sprintf("organizations/%d", in.OrganizationID), p)
		})

	add(r, writeIdempotent("delete_organization",
		"Delete an organization. Cascades to its inventories, projects, templates, and credentials."),
		func(ctx context.Context, c *ascender.Client, in struct {
			OrganizationID int `json:"organization_id"`
		}) (deleted, error) {
			return del(ctx, c, "organizations", in.OrganizationID)
		})

	type memberListArgs struct {
		OrganizationID int `json:"organization_id"`
		PageSize       int `json:"page_size,omitempty"`
	}
	add(r, read("list_organization_members", "List users who are members of an organization (not necessarily admins)."),
		func(ctx context.Context, c *ascender.Client, in memberListArgs) (ascender.Page, error) {
			return c.List(ctx, fmt.Sprintf("organizations/%d/users", in.OrganizationID), query(in.PageSize, 50))
		})
	add(r, read("list_organization_admins", "List users who are administrators of an organization."),
		func(ctx context.Context, c *ascender.Client, in memberListArgs) (ascender.Page, error) {
			return c.List(ctx, fmt.Sprintf("organizations/%d/admins", in.OrganizationID), query(in.PageSize, 50))
		})

	type memberArgs struct {
		OrganizationID int  `json:"organization_id"`
		UserID         int  `json:"user_id"`
		Disassociate   bool `json:"disassociate,omitempty" jsonschema:"true to remove instead of add"`
	}
	add(r, writeIdempotent("add_organization_user",
		"Add a user as a member of an organization. Pass disassociate=true to remove. Membership grants visibility; for elevated access use add_organization_admin or grant_user_role."),
		func(ctx context.Context, c *ascender.Client, in memberArgs) (map[string]any, error) {
			return associate(ctx, c, fmt.Sprintf("organizations/%d/users", in.OrganizationID), in.UserID, in.Disassociate)
		})
	add(r, writeIdempotent("add_organization_admin",
		"Add a user as an administrator of an organization. Pass disassociate=true to remove."),
		func(ctx context.Context, c *ascender.Client, in memberArgs) (map[string]any, error) {
			return associate(ctx, c, fmt.Sprintf("organizations/%d/admins", in.OrganizationID), in.UserID, in.Disassociate)
		})
}
