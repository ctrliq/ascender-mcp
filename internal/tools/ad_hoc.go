package tools

import (
	"context"
	"fmt"
	"os"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

// safeAdHocModules is the proxy-side allowlist for launch_ad_hoc_command.
// AWX's own AD_HOC_COMMANDS list includes shell and command, which is
// arbitrary remote code execution on any reachable host. Deploy with
// ASCENDER_AD_HOC_ALLOW_SHELL=true to defer entirely to AWX's list.
var safeAdHocModules = map[string]bool{"ping": true, "setup": true, "gather_facts": true}

func registerAdHoc(r *registry) {
	add(r, read("list_ad_hoc_commands", "List ad-hoc command runs."), list("ad_hoc_commands"))
	add(r, read("get_ad_hoc_command", "Get an ad-hoc command run by numeric ID."), get("ad_hoc_commands"))

	allowShell := os.Getenv("ASCENDER_AD_HOC_ALLOW_SHELL") == "true"
	desc := "Run a one-off Ansible module against an inventory. The proxy restricts module_name to read-only diagnostic modules: ping, setup, gather_facts. Returns the created ad_hoc_command record; poll with get_ad_hoc_command."
	if allowShell {
		desc = "Run a one-off Ansible module against an inventory (the `ansible -m <module> -a <args>` equivalent). The module list is enforced server-side via AD_HOC_COMMANDS. Returns the created ad_hoc_command record; poll with get_ad_hoc_command."
	}
	add(r, write("launch_ad_hoc_command", desc),
		func(ctx context.Context, c *ascender.Client, in struct {
			InventoryID   int            `json:"inventory_id"`
			CredentialID  int            `json:"credential_id" jsonschema:"machine credential for SSH"`
			ModuleName    string         `json:"module_name" jsonschema:"e.g. ping, setup, gather_facts"`
			ModuleArgs    string         `json:"module_args,omitempty" jsonschema:"raw module argument string"`
			Limit         string         `json:"limit,omitempty" jsonschema:"Ansible host limit pattern"`
			JobType       string         `json:"job_type,omitempty" jsonschema:"\"run\" (default) or \"check\""`
			BecomeEnabled bool           `json:"become_enabled,omitempty"`
			Verbosity     int            `json:"verbosity,omitempty" jsonschema:"0..5"`
			Forks         int            `json:"forks,omitempty"`
			DiffMode      bool           `json:"diff_mode,omitempty"`
			ExtraVars     map[string]any `json:"extra_vars,omitempty"`
		}) (map[string]any, error) {
			if !allowShell && !safeAdHocModules[in.ModuleName] {
				return nil, fmt.Errorf("module %q is not in the proxy-side allowlist (ping, setup, gather_facts); use a job template with a reviewed playbook, or deploy with ASCENDER_AD_HOC_ALLOW_SHELL=true", in.ModuleName)
			}
			p := map[string]any{
				"inventory":   in.InventoryID,
				"credential":  in.CredentialID,
				"module_name": in.ModuleName,
			}
			setIf(p, "module_args", in.ModuleArgs)
			setIf(p, "limit", in.Limit)
			setIf(p, "job_type", in.JobType)
			setIf(p, "become_enabled", in.BecomeEnabled)
			setIf(p, "verbosity", in.Verbosity)
			setIf(p, "forks", in.Forks)
			setIf(p, "diff_mode", in.DiffMode)
			setJSON(p, "extra_vars", in.ExtraVars)
			return c.Post(ctx, "ad_hoc_commands", p)
		})

	add(r, writeIdempotent("cancel_ad_hoc_command",
		"Cancel a running ad-hoc command. No-op if it already reached a terminal state."),
		func(ctx context.Context, c *ascender.Client, in struct {
			AdHocCommandID int `json:"ad_hoc_command_id"`
		}) (map[string]any, error) {
			return cancel(ctx, c, fmt.Sprintf("ad_hoc_commands/%d", in.AdHocCommandID))
		})
}
