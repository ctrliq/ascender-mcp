package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

// Templates and workflow templates are distinct AWX objects with near
// identical read paths, so the read tools take kind="template" or
// kind="workflow" and share handlers.

type templateKindArgs struct {
	IDOrName string `json:"id_or_name" jsonschema:"numeric ID or exact (case-sensitive) name"`
	Kind     string `json:"kind,omitempty" jsonschema:"\"template\" (default, for job templates) or \"workflow\" (for workflow templates)"`
}

type templateListArgs struct {
	listArgs
	Kind string `json:"kind,omitempty" jsonschema:"\"template\" (default, job templates) or \"workflow\" (workflow job templates)"`
}

func templateEndpoint(kind string) (string, error) {
	switch kind {
	case "", "template":
		return "job_templates", nil
	case "workflow":
		return "workflow_job_templates", nil
	}
	return "", fmt.Errorf(`invalid kind %q: expected "template" or "workflow"`, kind)
}

// templateSub GETs a fixed sub-resource (survey_spec, launch) of a
// template or workflow template.
func templateSub(sub string) handler[templateKindArgs, map[string]any] {
	return func(ctx context.Context, c *ascender.Client, in templateKindArgs) (map[string]any, error) {
		endpoint, err := templateEndpoint(in.Kind)
		if err != nil {
			return nil, err
		}
		id, err := resolveID(ctx, c, endpoint, in.IDOrName)
		if err != nil {
			return nil, err
		}
		return c.Get(ctx, fmt.Sprintf("%s/%d/%s", endpoint, id, sub), nil)
	}
}

type launchArgs struct {
	IDOrName       string         `json:"id_or_name" jsonschema:"template numeric ID or exact name"`
	ExtraVars      map[string]any `json:"extra_vars,omitempty" jsonschema:"Ansible variables to pass (dropped if ask_variables_on_launch is false)"`
	InventoryID    int            `json:"inventory_id,omitempty" jsonschema:"override the default inventory (dropped if ask_inventory_on_launch is false)"`
	Limit          string         `json:"limit,omitempty" jsonschema:"Ansible host limit pattern (dropped if ask_limit_on_launch is false)"`
	InstanceGroups []int          `json:"instance_groups,omitempty" jsonschema:"instance group IDs for execution-mesh routing (dropped if ask_instance_groups_on_launch is false)"`
}

func launch(endpoint string) handler[launchArgs, map[string]any] {
	return func(ctx context.Context, c *ascender.Client, in launchArgs) (map[string]any, error) {
		id, err := resolveID(ctx, c, endpoint, in.IDOrName)
		if err != nil {
			return nil, err
		}
		body := map[string]any{}
		if len(in.ExtraVars) > 0 {
			body["extra_vars"] = in.ExtraVars
		}
		setIf(body, "inventory", in.InventoryID)
		setIf(body, "limit", in.Limit)
		if len(in.InstanceGroups) > 0 {
			body["instance_groups"] = in.InstanceGroups
		}
		return c.Post(ctx, fmt.Sprintf("%s/%d/launch", endpoint, id), body)
	}
}

// jobTemplateFields are the optional settings shared by create and
// update. Required-on-create fields live on the create args.
type jobTemplateFields struct {
	InventoryID                     *int           `json:"inventory_id,omitempty"`
	ExecutionEnvironmentID          *int           `json:"execution_environment_id,omitempty"`
	Description                     *string        `json:"description,omitempty"`
	ExtraVars                       map[string]any `json:"extra_vars,omitempty" jsonschema:"pass {} to clear"`
	Limit                           *string        `json:"limit,omitempty"`
	JobTags                         *string        `json:"job_tags,omitempty"`
	SkipTags                        *string        `json:"skip_tags,omitempty"`
	ScmBranch                       *string        `json:"scm_branch,omitempty"`
	Verbosity                       *int           `json:"verbosity,omitempty"`
	DiffMode                        *bool          `json:"diff_mode,omitempty"`
	BecomeEnabled                   *bool          `json:"become_enabled,omitempty"`
	AllowSimultaneous               *bool          `json:"allow_simultaneous,omitempty"`
	Forks                           *int           `json:"forks,omitempty"`
	JobSliceCount                   *int           `json:"job_slice_count,omitempty"`
	Timeout                         *int           `json:"timeout,omitempty"`
	SurveyEnabled                   *bool          `json:"survey_enabled,omitempty"`
	AskInventoryOnLaunch            *bool          `json:"ask_inventory_on_launch,omitempty"`
	AskCredentialOnLaunch           *bool          `json:"ask_credential_on_launch,omitempty"`
	AskVariablesOnLaunch            *bool          `json:"ask_variables_on_launch,omitempty"`
	AskLimitOnLaunch                *bool          `json:"ask_limit_on_launch,omitempty"`
	AskTagsOnLaunch                 *bool          `json:"ask_tags_on_launch,omitempty"`
	AskSkipTagsOnLaunch             *bool          `json:"ask_skip_tags_on_launch,omitempty"`
	AskJobTypeOnLaunch              *bool          `json:"ask_job_type_on_launch,omitempty"`
	AskScmBranchOnLaunch            *bool          `json:"ask_scm_branch_on_launch,omitempty"`
	AskVerbosityOnLaunch            *bool          `json:"ask_verbosity_on_launch,omitempty"`
	AskDiffModeOnLaunch             *bool          `json:"ask_diff_mode_on_launch,omitempty"`
	AskInstanceGroupsOnLaunch       *bool          `json:"ask_instance_groups_on_launch,omitempty"`
	AskExecutionEnvironmentOnLaunch *bool          `json:"ask_execution_environment_on_launch,omitempty"`
	AskLabelsOnLaunch               *bool          `json:"ask_labels_on_launch,omitempty"`
	AskForksOnLaunch                *bool          `json:"ask_forks_on_launch,omitempty"`
	AskJobSliceCountOnLaunch        *bool          `json:"ask_job_slice_count_on_launch,omitempty"`
	AskTimeoutOnLaunch              *bool          `json:"ask_timeout_on_launch,omitempty"`
}

func (f jobTemplateFields) payload() map[string]any {
	p := map[string]any{}
	set(p, "inventory", f.InventoryID)
	set(p, "execution_environment", f.ExecutionEnvironmentID)
	set(p, "description", f.Description)
	setJSON(p, "extra_vars", f.ExtraVars)
	set(p, "limit", f.Limit)
	set(p, "job_tags", f.JobTags)
	set(p, "skip_tags", f.SkipTags)
	set(p, "scm_branch", f.ScmBranch)
	set(p, "verbosity", f.Verbosity)
	set(p, "diff_mode", f.DiffMode)
	set(p, "become_enabled", f.BecomeEnabled)
	set(p, "allow_simultaneous", f.AllowSimultaneous)
	set(p, "forks", f.Forks)
	set(p, "job_slice_count", f.JobSliceCount)
	set(p, "timeout", f.Timeout)
	set(p, "survey_enabled", f.SurveyEnabled)
	set(p, "ask_inventory_on_launch", f.AskInventoryOnLaunch)
	set(p, "ask_credential_on_launch", f.AskCredentialOnLaunch)
	set(p, "ask_variables_on_launch", f.AskVariablesOnLaunch)
	set(p, "ask_limit_on_launch", f.AskLimitOnLaunch)
	set(p, "ask_tags_on_launch", f.AskTagsOnLaunch)
	set(p, "ask_skip_tags_on_launch", f.AskSkipTagsOnLaunch)
	set(p, "ask_job_type_on_launch", f.AskJobTypeOnLaunch)
	set(p, "ask_scm_branch_on_launch", f.AskScmBranchOnLaunch)
	set(p, "ask_verbosity_on_launch", f.AskVerbosityOnLaunch)
	set(p, "ask_diff_mode_on_launch", f.AskDiffModeOnLaunch)
	set(p, "ask_instance_groups_on_launch", f.AskInstanceGroupsOnLaunch)
	set(p, "ask_execution_environment_on_launch", f.AskExecutionEnvironmentOnLaunch)
	set(p, "ask_labels_on_launch", f.AskLabelsOnLaunch)
	set(p, "ask_forks_on_launch", f.AskForksOnLaunch)
	set(p, "ask_job_slice_count_on_launch", f.AskJobSliceCountOnLaunch)
	set(p, "ask_timeout_on_launch", f.AskTimeoutOnLaunch)
	return p
}

type workflowTemplateFields struct {
	OrganizationID       *int           `json:"organization_id,omitempty"`
	InventoryID          *int           `json:"inventory_id,omitempty"`
	Description          *string        `json:"description,omitempty"`
	ExtraVars            map[string]any `json:"extra_vars,omitempty" jsonschema:"pass {} to clear"`
	AllowSimultaneous    *bool          `json:"allow_simultaneous,omitempty"`
	SurveyEnabled        *bool          `json:"survey_enabled,omitempty"`
	AskInventoryOnLaunch *bool          `json:"ask_inventory_on_launch,omitempty"`
	AskVariablesOnLaunch *bool          `json:"ask_variables_on_launch,omitempty"`
	AskLimitOnLaunch     *bool          `json:"ask_limit_on_launch,omitempty"`
	AskScmBranchOnLaunch *bool          `json:"ask_scm_branch_on_launch,omitempty"`
	AskTagsOnLaunch      *bool          `json:"ask_tags_on_launch,omitempty"`
	AskSkipTagsOnLaunch  *bool          `json:"ask_skip_tags_on_launch,omitempty"`
	AskLabelsOnLaunch    *bool          `json:"ask_labels_on_launch,omitempty"`
}

func (f workflowTemplateFields) payload() map[string]any {
	p := map[string]any{}
	set(p, "organization", f.OrganizationID)
	set(p, "inventory", f.InventoryID)
	set(p, "description", f.Description)
	setJSON(p, "extra_vars", f.ExtraVars)
	set(p, "allow_simultaneous", f.AllowSimultaneous)
	set(p, "survey_enabled", f.SurveyEnabled)
	set(p, "ask_inventory_on_launch", f.AskInventoryOnLaunch)
	set(p, "ask_variables_on_launch", f.AskVariablesOnLaunch)
	set(p, "ask_limit_on_launch", f.AskLimitOnLaunch)
	set(p, "ask_scm_branch_on_launch", f.AskScmBranchOnLaunch)
	set(p, "ask_tags_on_launch", f.AskTagsOnLaunch)
	set(p, "ask_skip_tags_on_launch", f.AskSkipTagsOnLaunch)
	set(p, "ask_labels_on_launch", f.AskLabelsOnLaunch)
	return p
}

func registerTemplates(r *registry) {
	add(r, read("list_templates",
		"List job templates (kind=\"template\", default) or workflow job templates (kind=\"workflow\")."),
		func(ctx context.Context, c *ascender.Client, in templateListArgs) (ascender.Page, error) {
			endpoint, err := templateEndpoint(in.Kind)
			if err != nil {
				return ascender.Page{}, err
			}
			return list(endpoint)(ctx, c, in.listArgs)
		})

	add(r, read("get_template",
		"Get a job template (kind=\"template\", default) or workflow job template (kind=\"workflow\") by numeric ID or exact case-sensitive name. Names are not unique across organizations; the first visible match wins."),
		func(ctx context.Context, c *ascender.Client, in templateKindArgs) (map[string]any, error) {
			endpoint, err := templateEndpoint(in.Kind)
			if err != nil {
				return nil, err
			}
			return c.Resolve(ctx, endpoint, in.IDOrName)
		})

	add(r, read("get_survey",
		"Return the survey spec for a job template or workflow template. Empty object when the template has no spec. A non-empty spec does not mean the survey is enforced on launch; check `survey_enabled` via get_template."),
		templateSub("survey_spec"))

	add(r, read("get_launch_config",
		"Return the launch config for a template/workflow. The ask_*_on_launch flags determine which overrides land vs are silently dropped at launch time; can_start_without_user_input=false means a survey response is required."),
		templateSub("launch"))

	add(r, write("launch_template",
		"Launch a job template. Returns the created job; poll get_job or call wait_for_job. Calling twice creates two runs (no native dedup). Overrides land in the response's ignored_fields if the template's matching ask_*_on_launch is false."),
		launch("job_templates"))

	add(r, write("launch_workflow",
		"Launch a workflow job template. Same deduplication caveat and ignored_fields semantics as launch_template. The limit argument is ignored for workflows."),
		launch("workflow_job_templates"))

	add(r, writeIdempotent("set_template_instance_groups",
		"Set the ordered list of instance groups on a job template, replacing the existing list. Routing precedence is template > inventory > organization > global. Prefer set_inventory_instance_groups when the constraint is a property of the target hosts. Pass an empty list to clear."),
		func(ctx context.Context, c *ascender.Client, in struct {
			TemplateID       int   `json:"template_id" jsonschema:"job template numeric ID"`
			InstanceGroupIDs []int `json:"instance_group_ids" jsonschema:"ordered list of instance group IDs; empty list clears all assignments"`
		}) (ascender.Page, error) {
			return replaceRelation(ctx, c, fmt.Sprintf("job_templates/%d/instance_groups", in.TemplateID), in.InstanceGroupIDs)
		})

	add(r, write("create_job_template",
		"Create a job template. job_type is \"run\" or \"check\"; project_id + playbook identify the playbook (use list_projects to enumerate). The ask_*_on_launch flags determine which fields can be overridden at launch. Returns the created template."),
		func(ctx context.Context, c *ascender.Client, in struct {
			Name      string `json:"name"`
			JobType   string `json:"job_type" jsonschema:"\"run\" or \"check\""`
			ProjectID int    `json:"project_id"`
			Playbook  string `json:"playbook" jsonschema:"playbook path within the project"`
			jobTemplateFields
		}) (map[string]any, error) {
			p := in.payload()
			p["name"], p["job_type"], p["project"], p["playbook"] = in.Name, in.JobType, in.ProjectID, in.Playbook
			return c.Post(ctx, "job_templates", p)
		})

	add(r, writeIdempotent("update_job_template",
		"Partially update a job template. Only the fields you pass are modified. Use this to flip ask_*_on_launch flags, change playbook/inventory defaults, or adjust survey wiring."),
		func(ctx context.Context, c *ascender.Client, in struct {
			TemplateID int     `json:"template_id"`
			Name       *string `json:"name,omitempty"`
			JobType    *string `json:"job_type,omitempty"`
			ProjectID  *int    `json:"project_id,omitempty"`
			Playbook   *string `json:"playbook,omitempty"`
			jobTemplateFields
		}) (map[string]any, error) {
			p := in.payload()
			set(p, "name", in.Name)
			set(p, "job_type", in.JobType)
			set(p, "project", in.ProjectID)
			set(p, "playbook", in.Playbook)
			return c.Patch(ctx, fmt.Sprintf("job_templates/%d", in.TemplateID), p)
		})

	add(r, writeIdempotent("delete_job_template",
		"Delete a job template. Workflow nodes that reference it become invalid; check via list_workflow_job_template_nodes before deleting."),
		func(ctx context.Context, c *ascender.Client, in struct {
			TemplateID int `json:"template_id"`
		}) (deleted, error) {
			return del(ctx, c, "job_templates", in.TemplateID)
		})

	add(r, write("copy_template",
		"Clone a job template under a new name. All settings copy over (survey, prompts, instance_groups). Returns the new template."),
		func(ctx context.Context, c *ascender.Client, in struct {
			TemplateID int    `json:"template_id" jsonschema:"source job template ID"`
			Name       string `json:"name" jsonschema:"name of the new template"`
		}) (map[string]any, error) {
			return c.Post(ctx, fmt.Sprintf("job_templates/%d/copy", in.TemplateID), map[string]any{"name": in.Name})
		})

	add(r, write("create_workflow_job_template",
		"Create an empty workflow template (no nodes yet). Then add nodes with create_workflow_node and link them with link_workflow_nodes."),
		func(ctx context.Context, c *ascender.Client, in struct {
			Name string `json:"name"`
			workflowTemplateFields
		}) (map[string]any, error) {
			p := in.payload()
			p["name"] = in.Name
			return c.Post(ctx, "workflow_job_templates", p)
		})

	add(r, writeIdempotent("update_workflow_job_template",
		"Partially update a workflow template. Same field set as create; pass only the fields to change."),
		func(ctx context.Context, c *ascender.Client, in struct {
			WorkflowTemplateID int     `json:"workflow_template_id"`
			Name               *string `json:"name,omitempty"`
			workflowTemplateFields
		}) (map[string]any, error) {
			p := in.payload()
			set(p, "name", in.Name)
			return c.Patch(ctx, fmt.Sprintf("workflow_job_templates/%d", in.WorkflowTemplateID), p)
		})

	add(r, writeIdempotent("delete_workflow_job_template",
		"Delete a workflow template. Cascades to its nodes; references from running workflow jobs block deletion."),
		func(ctx context.Context, c *ascender.Client, in struct {
			WorkflowTemplateID int `json:"workflow_template_id"`
		}) (deleted, error) {
			return del(ctx, c, "workflow_job_templates", in.WorkflowTemplateID)
		})

	add(r, write("copy_workflow_job_template",
		"Clone a workflow template (including all its nodes and links) under a new name."),
		func(ctx context.Context, c *ascender.Client, in struct {
			WorkflowTemplateID int    `json:"workflow_template_id"`
			Name               string `json:"name"`
		}) (map[string]any, error) {
			return c.Post(ctx, fmt.Sprintf("workflow_job_templates/%d/copy", in.WorkflowTemplateID), map[string]any{"name": in.Name})
		})

	add(r, read("list_workflow_job_template_nodes",
		"List the nodes of a workflow template (the structural graph). Each node carries a unified_job_template (the thing it launches), an identifier, and references to its success/failure/always children."),
		func(ctx context.Context, c *ascender.Client, in struct {
			WorkflowTemplateID int `json:"workflow_template_id"`
			PageSize           int `json:"page_size,omitempty"`
		}) (ascender.Page, error) {
			return c.List(ctx, fmt.Sprintf("workflow_job_templates/%d/workflow_nodes", in.WorkflowTemplateID), query(in.PageSize, 50))
		})

	add(r, write("create_workflow_node",
		"Add a node to a workflow template. The node references a unified_job_template (job template, workflow template, project, inventory_source, or system job) which runs when the node executes. Use link_workflow_nodes to wire it into the graph."),
		func(ctx context.Context, c *ascender.Client, in struct {
			WorkflowTemplateID     int            `json:"workflow_template_id"`
			UnifiedJobTemplateID   int            `json:"unified_job_template_id" jsonschema:"the template/workflow/project/etc this node will launch"`
			Identifier             string         `json:"identifier,omitempty" jsonschema:"string handle used in error messages and graph navigation"`
			AllParentsMustConverge bool           `json:"all_parents_must_converge,omitempty"`
			ExtraData              map[string]any `json:"extra_data,omitempty"`
			InventoryID            int            `json:"inventory_id,omitempty"`
			Limit                  string         `json:"limit,omitempty"`
			ScmBranch              string         `json:"scm_branch,omitempty"`
			JobTags                string         `json:"job_tags,omitempty"`
			SkipTags               string         `json:"skip_tags,omitempty"`
		}) (map[string]any, error) {
			p := map[string]any{
				"workflow_job_template": in.WorkflowTemplateID,
				"unified_job_template":  in.UnifiedJobTemplateID,
			}
			setIf(p, "identifier", in.Identifier)
			setIf(p, "all_parents_must_converge", in.AllParentsMustConverge)
			if len(in.ExtraData) > 0 {
				p["extra_data"] = in.ExtraData
			}
			setIf(p, "inventory", in.InventoryID)
			setIf(p, "limit", in.Limit)
			setIf(p, "scm_branch", in.ScmBranch)
			setIf(p, "job_tags", in.JobTags)
			setIf(p, "skip_tags", in.SkipTags)
			return c.Post(ctx, "workflow_job_template_nodes", p)
		})

	add(r, writeIdempotent("delete_workflow_node",
		"Delete a workflow node. Its inbound/outbound graph edges are removed too."),
		func(ctx context.Context, c *ascender.Client, in struct {
			NodeID int `json:"node_id"`
		}) (deleted, error) {
			return del(ctx, c, "workflow_job_template_nodes", in.NodeID)
		})

	add(r, writeIdempotent("link_workflow_nodes",
		"Add a graph edge between two workflow nodes. `on` selects the exit condition: \"success\" runs the child when the parent succeeds, \"failure\" when it fails, \"always\" regardless. Pass disassociate=true to remove an edge."),
		func(ctx context.Context, c *ascender.Client, in struct {
			ParentNodeID int    `json:"parent_node_id"`
			ChildNodeID  int    `json:"child_node_id"`
			On           string `json:"on" jsonschema:"\"success\", \"failure\", or \"always\""`
			Disassociate bool   `json:"disassociate,omitempty" jsonschema:"true to remove the edge instead of adding it"`
		}) (map[string]any, error) {
			switch in.On {
			case "success", "failure", "always":
			default:
				return nil, errors.New(`invalid on: expected "success", "failure", or "always"`)
			}
			return associate(ctx, c, fmt.Sprintf("workflow_job_template_nodes/%d/%s_nodes", in.ParentNodeID, in.On), in.ChildNodeID, in.Disassociate)
		})
}
