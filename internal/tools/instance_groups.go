package tools

import (
	"context"
	"fmt"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

type instanceGroupFields struct {
	CredentialID             *int           `json:"credential_id,omitempty" jsonschema:"container groups: kubeconfig credential for the target cluster"`
	PodSpecOverride          map[string]any `json:"pod_spec_override,omitempty" jsonschema:"container groups: override the pod spec used to spawn job pods; pass {} to clear"`
	MaxForks                 *int           `json:"max_forks,omitempty" jsonschema:"hard fork cap (0 = unlimited)"`
	MaxConcurrentJobs        *int           `json:"max_concurrent_jobs,omitempty"`
	PolicyInstanceList       []string       `json:"policy_instance_list,omitempty" jsonschema:"execution groups: explicit instance hostnames pinned to this group"`
	PolicyInstancePercentage *int           `json:"policy_instance_percentage,omitempty" jsonschema:"execution groups: auto-membership by percentage"`
	PolicyInstanceMinimum    *int           `json:"policy_instance_minimum,omitempty"`
}

func (f instanceGroupFields) payload() map[string]any {
	p := map[string]any{}
	set(p, "credential", f.CredentialID)
	setJSON(p, "pod_spec_override", f.PodSpecOverride)
	set(p, "max_forks", f.MaxForks)
	set(p, "max_concurrent_jobs", f.MaxConcurrentJobs)
	if f.PolicyInstanceList != nil {
		p["policy_instance_list"] = f.PolicyInstanceList
	}
	set(p, "policy_instance_percentage", f.PolicyInstancePercentage)
	set(p, "policy_instance_minimum", f.PolicyInstanceMinimum)
	return p
}

func registerInstanceGroups(r *registry) {
	add(r, read("list_instance_groups",
		"List instance groups. An instance group is a routable set of execution nodes. When a job must reach hosts the controller cannot (e.g. a private LAN), set the inventory or template's instance_groups so it routes to an in-network node."),
		list("instance_groups"))
	add(r, read("get_instance_group",
		"Get an instance group by numeric ID or exact name. `instances` and `capacity` show what is registered; `is_container_group` distinguishes Kubernetes container groups from Receptor execution groups."),
		get("instance_groups"))

	add(r, write("create_instance_group",
		"Create an instance group. For an execution group (Receptor nodes) leave is_container_group false and register instances separately. For a container group pass is_container_group=true with a kubeconfig credential_id and optionally a pod_spec_override."),
		func(ctx context.Context, c *ascender.Client, in struct {
			Name             string `json:"name"`
			IsContainerGroup bool   `json:"is_container_group,omitempty"`
			instanceGroupFields
		}) (map[string]any, error) {
			p := in.payload()
			p["name"] = in.Name
			setIf(p, "is_container_group", in.IsContainerGroup)
			return c.Post(ctx, "instance_groups", p)
		})

	add(r, writeIdempotent("update_instance_group",
		"Partially update an instance group, e.g. rotate a container group's kubeconfig credential or adjust max_forks."),
		func(ctx context.Context, c *ascender.Client, in struct {
			InstanceGroupID int     `json:"instance_group_id"`
			Name            *string `json:"name,omitempty"`
			instanceGroupFields
		}) (map[string]any, error) {
			p := in.payload()
			set(p, "name", in.Name)
			return c.Patch(ctx, fmt.Sprintf("instance_groups/%d", in.InstanceGroupID), p)
		})

	add(r, writeIdempotent("delete_instance_group",
		"Delete an instance group. Refused while active or pending jobs reference it. Templates and inventories that pinned it fall back to org/global routing."),
		func(ctx context.Context, c *ascender.Client, in struct {
			InstanceGroupID int `json:"instance_group_id"`
		}) (deleted, error) {
			return del(ctx, c, "instance_groups", in.InstanceGroupID)
		})
}
