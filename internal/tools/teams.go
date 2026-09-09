package tools

import (
	"context"
	"fmt"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

func registerTeams(r *registry) {
	add(r, read("list_teams", "List teams (a team is a group of users within an organization)."), list("teams"))
	add(r, read("get_team", "Get a team by numeric ID or exact name."), get("teams"))

	add(r, write("create_team",
		"Create a team in an organization. Use grant_team_role to give it access to resources and add_team_user to populate it."),
		func(ctx context.Context, c *ascender.Client, in struct {
			Name           string `json:"name"`
			OrganizationID int    `json:"organization_id"`
			Description    string `json:"description,omitempty"`
		}) (map[string]any, error) {
			p := map[string]any{"name": in.Name, "organization": in.OrganizationID}
			setIf(p, "description", in.Description)
			return c.Post(ctx, "teams", p)
		})

	add(r, writeIdempotent("update_team", "Partially update a team."),
		func(ctx context.Context, c *ascender.Client, in struct {
			TeamID      int     `json:"team_id"`
			Name        *string `json:"name,omitempty"`
			Description *string `json:"description,omitempty"`
		}) (map[string]any, error) {
			p := map[string]any{}
			set(p, "name", in.Name)
			set(p, "description", in.Description)
			return c.Patch(ctx, fmt.Sprintf("teams/%d", in.TeamID), p)
		})

	add(r, writeIdempotent("delete_team",
		"Delete a team. Members remain as users; their team-derived role grants are revoked."),
		func(ctx context.Context, c *ascender.Client, in struct {
			TeamID int `json:"team_id"`
		}) (deleted, error) {
			return del(ctx, c, "teams", in.TeamID)
		})

	add(r, read("list_team_users", "List users on a team."),
		func(ctx context.Context, c *ascender.Client, in struct {
			TeamID   int `json:"team_id"`
			PageSize int `json:"page_size,omitempty"`
		}) (ascender.Page, error) {
			return c.List(ctx, fmt.Sprintf("teams/%d/users", in.TeamID), query(in.PageSize, 50))
		})

	add(r, writeIdempotent("add_team_user", "Add a user to a team. Pass disassociate=true to remove."),
		func(ctx context.Context, c *ascender.Client, in struct {
			TeamID       int  `json:"team_id"`
			UserID       int  `json:"user_id"`
			Disassociate bool `json:"disassociate,omitempty"`
		}) (map[string]any, error) {
			return associate(ctx, c, fmt.Sprintf("teams/%d/users", in.TeamID), in.UserID, in.Disassociate)
		})
}
