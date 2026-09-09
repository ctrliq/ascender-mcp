package tools

import (
	"context"
	"fmt"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

type executionEnvironmentFields struct {
	OrganizationID *int    `json:"organization_id,omitempty"`
	CredentialID   *int    `json:"credential_id,omitempty" jsonschema:"registry credential for private images"`
	Pull           *string `json:"pull,omitempty" jsonschema:"\"always\", \"missing\", or \"never\""`
	Description    *string `json:"description,omitempty"`
}

func (f executionEnvironmentFields) payload() map[string]any {
	p := map[string]any{}
	set(p, "organization", f.OrganizationID)
	set(p, "credential", f.CredentialID)
	set(p, "pull", f.Pull)
	set(p, "description", f.Description)
	return p
}

func registerExecutionEnvironments(r *registry) {
	add(r, read("list_execution_environments",
		"List execution environments (the container images Ascender runs playbooks inside)."),
		list("execution_environments"))
	add(r, read("get_execution_environment", "Get an execution environment by numeric ID or exact name."),
		get("execution_environments"))

	add(r, write("create_execution_environment",
		"Register a container image as an execution environment. Provide credential_id when the image lives in a private registry."),
		func(ctx context.Context, c *ascender.Client, in struct {
			Name  string `json:"name"`
			Image string `json:"image" jsonschema:"container image reference, e.g. quay.io/ansible/ee:latest"`
			executionEnvironmentFields
		}) (map[string]any, error) {
			p := in.payload()
			p["name"], p["image"] = in.Name, in.Image
			return c.Post(ctx, "execution_environments", p)
		})

	add(r, writeIdempotent("update_execution_environment",
		"Partially update an execution environment (typically image or credential rotation)."),
		func(ctx context.Context, c *ascender.Client, in struct {
			ExecutionEnvironmentID int     `json:"execution_environment_id"`
			Name                   *string `json:"name,omitempty"`
			Image                  *string `json:"image,omitempty"`
			executionEnvironmentFields
		}) (map[string]any, error) {
			p := in.payload()
			set(p, "name", in.Name)
			set(p, "image", in.Image)
			return c.Patch(ctx, fmt.Sprintf("execution_environments/%d", in.ExecutionEnvironmentID), p)
		})

	add(r, writeIdempotent("delete_execution_environment",
		"Delete an execution environment. Templates that reference it fall back to the cluster default."),
		func(ctx context.Context, c *ascender.Client, in struct {
			ExecutionEnvironmentID int `json:"execution_environment_id"`
		}) (deleted, error) {
			return del(ctx, c, "execution_environments", in.ExecutionEnvironmentID)
		})
}
