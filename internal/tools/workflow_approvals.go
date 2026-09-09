package tools

import (
	"context"
	"fmt"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

func registerWorkflowApprovals(r *registry) {
	add(r, read("list_workflow_approvals",
		"List workflow approvals. Use search or status filters to find approvals waiting on a human gate."),
		list("workflow_approvals"))
	add(r, read("get_workflow_approval",
		"Get a workflow approval by numeric ID or exact name. Inspect status, timed_out, canceled_on, and approved_or_denied_by."),
		get("workflow_approvals"))

	// Approval gates exist to interrupt automation. Requiring the caller
	// to echo the live node name back stops an LLM from approving by ID
	// alone without having looked at what it is approving.
	type approvalArgs struct {
		ApprovalID   int    `json:"approval_id"`
		ExpectedName string `json:"expected_name" jsonschema:"the approval node's name as returned by get_workflow_approval; the call is refused if it disagrees with the live record"`
	}
	act := func(name, desc, action string) {
		add(r, writeIdempotent(name, desc), func(ctx context.Context, c *ascender.Client, in approvalArgs) (map[string]any, error) {
			path := fmt.Sprintf("workflow_approvals/%d", in.ApprovalID)
			live, err := c.Get(ctx, path, nil)
			if err != nil {
				return nil, err
			}
			liveName, _ := live["name"].(string)
			if liveName == "" || liveName != in.ExpectedName {
				return nil, fmt.Errorf("expected_name %q does not match live approval name %q; refusing to %s", in.ExpectedName, liveName, action)
			}
			if _, err := c.Post(ctx, path+"/"+action, nil); err != nil {
				return nil, err
			}
			return c.Get(ctx, path, nil)
		})
	}
	act("deny_workflow_approval",
		"Deny a pending workflow approval; the workflow follows its failure branch. expected_name must match the live record.",
		"deny")
	act("approve_workflow_approval",
		"Approve a pending workflow approval; the workflow proceeds. expected_name must match the live record. Prefer deny_workflow_approval if uncertain.",
		"approve")
}
