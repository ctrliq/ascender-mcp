// Package tools registers the MCP tool surface. Each tool is a thin
// pass-through to the Ascender REST API.
package tools

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

// toolsets maps a selectable name to its registrar, one per source
// file. ping and whoami are always on and live in registerCore.
var toolsets = map[string]func(*registry){
	"templates":              registerTemplates,
	"inventories":            registerInventories,
	"credentials":            registerCredentials,
	"instance_groups":        registerInstanceGroups,
	"jobs":                   registerJobs,
	"schedules":              registerSchedules,
	"projects":               registerProjects,
	"ad_hoc":                 registerAdHoc,
	"organizations":          registerOrganizations,
	"teams":                  registerTeams,
	"users":                  registerUsers,
	"roles":                  registerRoles,
	"execution_environments": registerExecutionEnvironments,
	"labels":                 registerLabels,
	"workflow_approvals":     registerWorkflowApprovals,
	"workflow_nodes":         registerWorkflowNodes,
}

// bundles are task-oriented shortcuts. Launching a job needs tools from
// several resource groups, so "jobs" alone would watch jobs it cannot
// start; a bundle names the whole correct set.
var bundles = map[string][]string{
	"job_runner":      {"templates", "jobs", "inventories", "projects"},
	"workflow_runner": {"templates", "jobs", "workflow_nodes", "workflow_approvals", "inventories", "projects"},
}

// ToolsetNames returns the valid resource-group names, sorted.
func ToolsetNames() []string {
	return slices.Sorted(maps.Keys(toolsets))
}

// ParseToolsets turns a comma-separated list of resource groups and
// bundles into deduped resource-group names. Empty input means all
// (returned as nil). An unknown name is an error so a typo in deploy
// config fails startup instead of silently serving fewer tools.
func ParseToolsets(spec string) ([]string, error) {
	var selected []string
	add := func(name string) {
		if !slices.Contains(selected, name) {
			selected = append(selected, name)
		}
	}
	for _, raw := range strings.Split(spec, ",") {
		name := strings.TrimSpace(raw)
		switch {
		case name == "":
		case bundles[name] != nil:
			for _, member := range bundles[name] {
				add(member)
			}
		case toolsets[name] != nil:
			add(name)
		default:
			return nil, fmt.Errorf("unknown toolset %q: valid bundles are %s; valid resource groups are %s",
				name, strings.Join(slices.Sorted(maps.Keys(bundles)), ", "), strings.Join(ToolsetNames(), ", "))
		}
	}
	return selected, nil
}

// Register attaches the core tools plus the selected toolsets to s.
// A nil or empty selection means every toolset.
func Register(s *mcp.Server, c *ascender.Client, readOnly bool, enabled []string) {
	r := &registry{server: s, client: c, readOnly: readOnly}
	registerCore(r)
	if len(enabled) == 0 {
		enabled = ToolsetNames()
	}
	for _, name := range enabled {
		toolsets[name](r)
	}
}
