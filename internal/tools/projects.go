package tools

import (
	"context"
	"fmt"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

type projectFields struct {
	ScmURL               *string `json:"scm_url,omitempty"`
	ScmBranch            *string `json:"scm_branch,omitempty"`
	CredentialID         *int    `json:"credential_id,omitempty" jsonschema:"SCM credential for private repos"`
	ScmUpdateOnLaunch    *bool   `json:"scm_update_on_launch,omitempty" jsonschema:"refresh from SCM at the start of every job that uses this project"`
	ScmDeleteOnUpdate    *bool   `json:"scm_delete_on_update,omitempty"`
	ScmTrackSubmodules   *bool   `json:"scm_track_submodules,omitempty"`
	AllowOverride        *bool   `json:"allow_override,omitempty" jsonschema:"allow templates to override scm_branch at launch"`
	Description          *string `json:"description,omitempty"`
	DefaultEnvironmentID *int    `json:"default_environment_id,omitempty" jsonschema:"default execution environment for jobs based on this project"`
}

func (f projectFields) payload() map[string]any {
	p := map[string]any{}
	set(p, "scm_url", f.ScmURL)
	set(p, "scm_branch", f.ScmBranch)
	set(p, "credential", f.CredentialID)
	set(p, "scm_update_on_launch", f.ScmUpdateOnLaunch)
	set(p, "scm_delete_on_update", f.ScmDeleteOnUpdate)
	set(p, "scm_track_submodules", f.ScmTrackSubmodules)
	set(p, "allow_override", f.AllowOverride)
	set(p, "description", f.Description)
	set(p, "default_environment", f.DefaultEnvironmentID)
	return p
}

func registerProjects(r *registry) {
	add(r, read("list_projects",
		"List projects. A project is an SCM-backed source of playbooks; job templates reference one by project_id plus a playbook path."),
		list("projects"))
	add(r, read("get_project",
		"Get a project by numeric ID or exact name. Useful fields: scm_url, scm_branch, scm_revision, status, last_job_run."),
		get("projects"))

	add(r, write("create_project",
		"Create a project. The initial sync runs automatically; track it via list_jobs(type=\"all\", template_id=<project_id>). scm_type is \"\" (manual), \"git\", \"archive\", or \"insights\"."),
		func(ctx context.Context, c *ascender.Client, in struct {
			Name           string `json:"name"`
			OrganizationID int    `json:"organization_id"`
			ScmType        string `json:"scm_type" jsonschema:"\"\" (manual), \"git\", \"archive\", or \"insights\""`
			projectFields
		}) (map[string]any, error) {
			p := in.payload()
			p["name"], p["organization"], p["scm_type"] = in.Name, in.OrganizationID, in.ScmType
			return c.Post(ctx, "projects", p)
		})

	add(r, writeIdempotent("update_project", "Partially update a project. Only the fields you pass are modified."),
		func(ctx context.Context, c *ascender.Client, in struct {
			ProjectID int     `json:"project_id"`
			Name      *string `json:"name,omitempty"`
			projectFields
		}) (map[string]any, error) {
			p := in.payload()
			set(p, "name", in.Name)
			return c.Patch(ctx, fmt.Sprintf("projects/%d", in.ProjectID), p)
		})

	add(r, writeIdempotent("delete_project",
		"Delete a project. Refused while job templates still reference it; delete those first."),
		func(ctx context.Context, c *ascender.Client, in struct {
			ProjectID int `json:"project_id"`
		}) (deleted, error) {
			return del(ctx, c, "projects", in.ProjectID)
		})

	add(r, write("sync_project",
		"Trigger an immediate SCM sync. Returns the project_update job; poll with get_job or wait_for_job. Each call starts a new sync."),
		func(ctx context.Context, c *ascender.Client, in struct {
			ProjectID int `json:"project_id"`
		}) (map[string]any, error) {
			return c.Post(ctx, fmt.Sprintf("projects/%d/update", in.ProjectID), nil)
		})
}
