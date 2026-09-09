package tools

import (
	"context"
	"fmt"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

// Node create/delete/link live in templates.go with the workflow
// template tools; this file holds node editing and its relations.
func registerWorkflowNodes(r *registry) {
	add(r, writeIdempotent("update_workflow_node",
		"Partially update a workflow node: flip all_parents_must_converge, change extra_data, or adjust the node's launch overrides."),
		func(ctx context.Context, c *ascender.Client, in struct {
			NodeID                 int            `json:"node_id"`
			Identifier             *string        `json:"identifier,omitempty"`
			AllParentsMustConverge *bool          `json:"all_parents_must_converge,omitempty"`
			ExtraData              map[string]any `json:"extra_data,omitempty" jsonschema:"pass {} to clear"`
			InventoryID            *int           `json:"inventory_id,omitempty"`
			Limit                  *string        `json:"limit,omitempty"`
			ScmBranch              *string        `json:"scm_branch,omitempty"`
			JobTags                *string        `json:"job_tags,omitempty"`
			SkipTags               *string        `json:"skip_tags,omitempty"`
			Verbosity              *int           `json:"verbosity,omitempty"`
			Forks                  *int           `json:"forks,omitempty"`
			JobSliceCount          *int           `json:"job_slice_count,omitempty"`
			Timeout                *int           `json:"timeout,omitempty"`
			DiffMode               *bool          `json:"diff_mode,omitempty"`
		}) (map[string]any, error) {
			p := map[string]any{}
			set(p, "identifier", in.Identifier)
			set(p, "all_parents_must_converge", in.AllParentsMustConverge)
			if in.ExtraData != nil {
				p["extra_data"] = in.ExtraData
			}
			set(p, "inventory", in.InventoryID)
			set(p, "limit", in.Limit)
			set(p, "scm_branch", in.ScmBranch)
			set(p, "job_tags", in.JobTags)
			set(p, "skip_tags", in.SkipTags)
			set(p, "verbosity", in.Verbosity)
			set(p, "forks", in.Forks)
			set(p, "job_slice_count", in.JobSliceCount)
			set(p, "timeout", in.Timeout)
			set(p, "diff_mode", in.DiffMode)
			return c.Patch(ctx, fmt.Sprintf("workflow_job_template_nodes/%d", in.NodeID), p)
		})

	type relationArgs struct {
		NodeID int   `json:"node_id"`
		IDs    []int `json:"ids" jsonschema:"replace the relation with this exact ID list; pass [] to clear"`
	}
	setRelation := func(name, desc, sub string) {
		add(r, writeIdempotent(name, desc), func(ctx context.Context, c *ascender.Client, in relationArgs) (ascender.Page, error) {
			return replaceRelation(ctx, c, fmt.Sprintf("workflow_job_template_nodes/%d/%s", in.NodeID, sub), in.IDs)
		})
	}
	setRelation("set_workflow_node_credentials",
		"Set the credentials a workflow node passes at launch. Full-replace semantics; pass [] to clear.",
		"credentials")
	setRelation("set_workflow_node_labels",
		"Set the labels on a workflow node. Full-replace semantics; pass [] to clear.",
		"labels")
	setRelation("set_workflow_node_instance_groups",
		"Set the instance groups a workflow node routes through at launch. Same semantics as set_template_instance_groups, scoped to one node.",
		"instance_groups")
}
