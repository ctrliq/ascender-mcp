package tools

import (
	"context"
	"fmt"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

func registerLabels(r *registry) {
	add(r, read("list_labels", "List labels."), list("labels"))
	add(r, read("get_label", "Get a label by numeric ID or exact name."), get("labels"))

	add(r, write("create_label",
		"Create a label scoped to an organization. Labels attach to templates, schedules, workflows, and inventories by reference."),
		func(ctx context.Context, c *ascender.Client, in struct {
			Name           string `json:"name"`
			OrganizationID int    `json:"organization_id"`
		}) (map[string]any, error) {
			return c.Post(ctx, "labels", map[string]any{"name": in.Name, "organization": in.OrganizationID})
		})

	type labelArgs struct {
		ResourceID   int  `json:"resource_id" jsonschema:"the template, workflow, schedule, or inventory ID"`
		LabelID      int  `json:"label_id"`
		Disassociate bool `json:"disassociate,omitempty"`
	}
	attach := func(name, desc, resource string) {
		add(r, writeIdempotent(name, desc), func(ctx context.Context, c *ascender.Client, in labelArgs) (map[string]any, error) {
			return associate(ctx, c, fmt.Sprintf("%s/%d/labels", resource, in.ResourceID), in.LabelID, in.Disassociate)
		})
	}
	attach("label_job_template", "Attach a label to a job template. Pass disassociate=true to remove.", "job_templates")
	attach("label_workflow_template", "Attach a label to a workflow template. Pass disassociate=true to remove.", "workflow_job_templates")
	attach("label_schedule", "Attach a label to a schedule. Pass disassociate=true to remove.", "schedules")
	attach("label_inventory", "Attach a label to an inventory. Pass disassociate=true to remove.", "inventories")
}
