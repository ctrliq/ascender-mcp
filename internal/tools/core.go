package tools

import (
	"context"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

// pingResult drops pod names, node UUIDs, and install_uuid from the
// raw ping response so infrastructure detail stays off the model.
type pingResult struct {
	OK      bool   `json:"ok"`
	HA      bool   `json:"ha"`
	Version string `json:"version"`
}

// registerCore adds the tools present regardless of toolset selection.
func registerCore(r *registry) {
	add(r, read("ping",
		"Check connectivity to the Ascender server. Returns ok/ha/version only."),
		func(ctx context.Context, c *ascender.Client, _ noArgs) (pingResult, error) {
			raw, err := c.Ping(ctx)
			if err != nil {
				return pingResult{}, err
			}
			ha, _ := raw["ha"].(bool)
			version, _ := raw["version"].(string)
			return pingResult{OK: true, HA: ha, Version: version}, nil
		})

	add(r, read("whoami",
		"Return the authenticated user's profile: username, email, is_superuser, and other identity fields. Call first to confirm the token's identity and permissions before launching anything."),
		func(ctx context.Context, c *ascender.Client, _ noArgs) (map[string]any, error) {
			return c.Me(ctx)
		})
}
