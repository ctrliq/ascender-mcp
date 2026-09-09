package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

func TestParseToolsets(t *testing.T) {
	t.Run("empty selects all", func(t *testing.T) {
		for _, spec := range []string{"", "  ", ",", " , "} {
			got, err := ParseToolsets(spec)
			if err != nil {
				t.Fatalf("ParseToolsets(%q) err = %v", spec, err)
			}
			if got != nil {
				t.Fatalf("ParseToolsets(%q) = %v, want nil (all)", spec, got)
			}
		}
	})
	t.Run("valid names pass through deduped", func(t *testing.T) {
		got, err := ParseToolsets(" jobs, templates ,jobs ")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if len(got) != 2 || got[0] != "jobs" || got[1] != "templates" {
			t.Fatalf("got %v, want [jobs templates]", got)
		}
	})
	t.Run("unknown name errors with valid list", func(t *testing.T) {
		_, err := ParseToolsets("jobs,bogus")
		if err == nil {
			t.Fatal("expected error for unknown toolset")
		}
		if !strings.Contains(err.Error(), `"bogus"`) {
			t.Fatalf("error should name the bad toolset: %v", err)
		}
		if !strings.Contains(err.Error(), "jobs") {
			t.Fatalf("error should list valid resource groups: %v", err)
		}
		if !strings.Contains(err.Error(), "job_runner") {
			t.Fatalf("error should list valid bundles: %v", err)
		}
	})
	t.Run("bundle expands to its members deduped", func(t *testing.T) {
		got, err := ParseToolsets("job_runner")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		want := map[string]bool{"templates": true, "jobs": true, "inventories": true, "projects": true}
		if len(got) != len(want) {
			t.Fatalf("job_runner expanded to %v, want %d members", got, len(want))
		}
		for _, name := range got {
			if !want[name] {
				t.Fatalf("job_runner expanded to unexpected member %q (%v)", name, got)
			}
		}
	})
	t.Run("bundle and overlapping group dedupe together", func(t *testing.T) {
		got, err := ParseToolsets("job_runner,jobs,credentials")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		seen := map[string]int{}
		for _, name := range got {
			seen[name]++
		}
		if seen["jobs"] != 1 {
			t.Fatalf("jobs appears %d times, want 1: %v", seen["jobs"], got)
		}
		if seen["credentials"] != 1 {
			t.Fatalf("credentials missing from %v", got)
		}
	})
	t.Run("every registrar name is parseable", func(t *testing.T) {
		names := ToolsetNames()
		if len(names) != len(toolsets) {
			t.Fatalf("ToolsetNames returned %d names, want %d", len(names), len(toolsets))
		}
		got, err := ParseToolsets(strings.Join(names, ","))
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if len(got) != len(names) {
			t.Fatalf("got %d names back, want %d", len(got), len(names))
		}
	})
}

// listToolNames registers the given toolsets on a fresh server and
// returns the resulting tools/list names via an in-memory session.
func listToolNames(t *testing.T, readOnlyMode bool, enabled []string) map[string]bool {
	t.Helper()
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	Register(server, ascender.New("http://ascender.invalid", false), readOnlyMode, enabled)

	ct, st := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	names := map[string]bool{}
	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		names[tool.Name] = true
	}
	return names
}

func TestRegisterToolsetFiltering(t *testing.T) {
	jobsOnly := listToolNames(t, false, []string{"jobs"})
	for _, want := range []string{"ping", "whoami", "list_jobs", "get_job"} {
		if !jobsOnly[want] {
			t.Errorf("jobs-only surface missing %q", want)
		}
	}
	for _, absent := range []string{"list_users", "create_credential", "launch_template"} {
		if jobsOnly[absent] {
			t.Errorf("jobs-only surface should not expose %q", absent)
		}
	}

	all := listToolNames(t, false, nil)
	if len(all) <= len(jobsOnly) {
		t.Fatalf("all toolsets (%d tools) should exceed jobs-only (%d tools)", len(all), len(jobsOnly))
	}
	for _, want := range []string{"ping", "whoami", "list_jobs", "list_users", "launch_template"} {
		if !all[want] {
			t.Errorf("full surface missing %q", want)
		}
	}
}

func TestBundlesAreValid(t *testing.T) {
	groups := map[string]bool{}
	for _, name := range ToolsetNames() {
		groups[name] = true
	}
	for bundle, members := range bundles {
		if groups[bundle] {
			t.Errorf("bundle %q collides with a resource-group name", bundle)
		}
		if len(members) == 0 {
			t.Errorf("bundle %q has no members", bundle)
		}
		for _, m := range members {
			if !groups[m] {
				t.Errorf("bundle %q references unknown resource group %q", bundle, m)
			}
		}
	}
}

func TestJobRunnerBundleCanLaunch(t *testing.T) {
	enabled, err := ParseToolsets("job_runner")
	if err != nil {
		t.Fatalf("ParseToolsets: %v", err)
	}
	names := listToolNames(t, false, enabled)
	// The whole point of the bundle: a client can both start a job and
	// watch it, which "jobs" alone cannot do.
	for _, want := range []string{"launch_template", "list_jobs", "get_job_stdout", "wait_for_job"} {
		if !names[want] {
			t.Errorf("job_runner surface missing %q", want)
		}
	}
}

func TestRegisterReadOnlyStillFilters(t *testing.T) {
	names := listToolNames(t, true, []string{"users"})
	if !names["list_users"] || !names["ping"] {
		t.Errorf("read-only users surface missing read tools: %v", names)
	}
	if names["create_user"] || names["set_user_password"] {
		t.Errorf("read-only surface exposes mutating tools: %v", names)
	}
}
