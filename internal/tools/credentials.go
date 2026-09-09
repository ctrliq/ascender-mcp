package tools

import (
	"context"
	"fmt"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

func registerCredentials(r *registry) {
	add(r, read("list_credentials",
		"List credentials (machine, vault, cloud, source-control, etc.). Secret fields are masked by Ascender."),
		list("credentials"))
	add(r, read("get_credential", "Get a credential by numeric ID or exact name. Secret fields are masked."),
		get("credentials"))

	add(r, read("list_credential_types",
		"List credential types (Machine, Vault, Source Control, Amazon Web Services, etc.). Each type's `inputs.fields` array says which keys a create_credential call should set."),
		list("credential_types"))
	add(r, read("get_credential_type",
		"Get a credential type by numeric ID or exact name. `inputs.fields` describes the keys the credential's `inputs` accepts."),
		get("credential_types"))

	add(r, write("create_credential",
		"Create a credential. Look up credential_type_id via list_credential_types. Pass exactly one owner: organization_id, user_id, or team_id. `inputs` carries the secret material; Ascender encrypts at rest and masks on read."),
		func(ctx context.Context, c *ascender.Client, in struct {
			Name             string         `json:"name"`
			CredentialTypeID int            `json:"credential_type_id"`
			Inputs           map[string]any `json:"inputs" jsonschema:"credential-type-specific fields (e.g. username/password/ssh_key_data); see get_credential_type"`
			OrganizationID   int            `json:"organization_id,omitempty"`
			UserID           int            `json:"user_id,omitempty"`
			TeamID           int            `json:"team_id,omitempty"`
			Description      string         `json:"description,omitempty"`
		}) (map[string]any, error) {
			owners := 0
			for _, id := range []int{in.OrganizationID, in.UserID, in.TeamID} {
				if id != 0 {
					owners++
				}
			}
			if owners != 1 {
				return nil, fmt.Errorf("credential needs exactly one owner; got %d: pass exactly one of organization_id, user_id, or team_id", owners)
			}
			p := map[string]any{
				"name":            in.Name,
				"credential_type": in.CredentialTypeID,
				"inputs":          in.Inputs,
			}
			setIf(p, "organization", in.OrganizationID)
			setIf(p, "user", in.UserID)
			setIf(p, "team", in.TeamID)
			setIf(p, "description", in.Description)
			return c.Post(ctx, "credentials", p)
		})

	add(r, writeIdempotent("update_credential",
		"Partially update a credential. Pass `inputs` to rotate secrets; only the keys you pass change, AWX merges with the stored set."),
		func(ctx context.Context, c *ascender.Client, in struct {
			CredentialID int            `json:"credential_id"`
			Name         *string        `json:"name,omitempty"`
			Description  *string        `json:"description,omitempty"`
			Inputs       map[string]any `json:"inputs,omitempty"`
		}) (map[string]any, error) {
			p := map[string]any{}
			set(p, "name", in.Name)
			set(p, "description", in.Description)
			if len(in.Inputs) > 0 {
				p["inputs"] = in.Inputs
			}
			return c.Patch(ctx, fmt.Sprintf("credentials/%d", in.CredentialID), p)
		})

	add(r, writeIdempotent("delete_credential",
		"Delete a credential. Job templates and inventory sources that reference it will fail to launch; reassign those first."),
		func(ctx context.Context, c *ascender.Client, in struct {
			CredentialID int `json:"credential_id"`
		}) (deleted, error) {
			return del(ctx, c, "credentials", in.CredentialID)
		})
}
