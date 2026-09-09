package tools

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

type jobIDArgs struct {
	JobID int    `json:"job_id" jsonschema:"job ID"`
	Kind  string `json:"kind,omitempty" jsonschema:"\"job\" (default) or \"workflow\""`
}

func jobEndpoint(kind string) (string, error) {
	switch kind {
	case "", "job":
		return "jobs", nil
	case "workflow":
		return "workflow_jobs", nil
	}
	return "", fmt.Errorf(`invalid kind %q: expected "job" or "workflow"`, kind)
}

type textResult struct {
	Content   string `json:"content"`
	Truncated bool   `json:"truncated,omitempty"`
}

func registerJobs(r *registry) {
	add(r, read("list_jobs",
		"List recent jobs, newest first. By default returns regular jobs and workflow jobs; pass type=\"all\" to include project updates, inventory syncs, ad-hoc commands, and system jobs. template_id matches both job templates and workflow templates."),
		func(ctx context.Context, c *ascender.Client, in struct {
			Status     string `json:"status,omitempty" jsonschema:"status filter (pending, waiting, running, successful, failed, error, canceled)"`
			TemplateID int    `json:"template_id,omitempty" jsonschema:"numeric ID of the job template or workflow template"`
			Type       string `json:"type,omitempty" jsonschema:"\"jobs\" (default: regular + workflow jobs) or \"all\""`
			PageSize   int    `json:"page_size,omitempty" jsonschema:"results per page (default 20, max 200)"`
		}) (ascender.Page, error) {
			q := query(in.PageSize, 20)
			q.Set("order_by", "-created")
			switch in.Type {
			case "", "jobs":
				q.Set("type__in", "job,workflow_job")
			case "all":
			default:
				return ascender.Page{}, fmt.Errorf(`invalid type %q: expected "jobs" or "all"`, in.Type)
			}
			setQuery(q, "status", in.Status)
			if in.TemplateID != 0 {
				q.Set("unified_job_template", strconv.Itoa(in.TemplateID))
			}
			return c.List(ctx, "unified_jobs", q)
		})

	add(r, read("get_job",
		"Get a job's full record and current status. Set kind=\"workflow\" for workflow jobs."),
		func(ctx context.Context, c *ascender.Client, in jobIDArgs) (map[string]any, error) {
			endpoint, err := jobEndpoint(in.Kind)
			if err != nil {
				return nil, err
			}
			return c.Get(ctx, fmt.Sprintf("%s/%d", endpoint, in.JobID), nil)
		})

	add(r, read("get_job_stdout",
		"Get a regular job's stdout as plain text (ANSI stripped), capped at 1MiB with `truncated` set when the cap is hit; fall back to list_job_events for structured triage. Workflow jobs have no stdout of their own: call list_workflow_nodes to find child job IDs, then call this on each."),
		func(ctx context.Context, c *ascender.Client, in jobIDArgs) (textResult, error) {
			if in.Kind == "workflow" {
				return textResult{}, errors.New("workflow jobs have no stdout; call list_workflow_nodes to find child job IDs, then call get_job_stdout on each")
			}
			if _, err := jobEndpoint(in.Kind); err != nil {
				return textResult{}, err
			}
			// txt_download strips the ANSI color codes that ?format=txt keeps.
			q := url.Values{"format": {"txt_download"}}
			text, truncated, err := c.GetText(ctx, fmt.Sprintf("jobs/%d/stdout", in.JobID), q)
			return textResult{Content: text, Truncated: truncated}, err
		})

	add(r, read("list_job_events",
		"List structured per-task events for a regular job. Better than get_job_stdout for failure triage: each event has host, task, event type (runner_on_failed, runner_on_ok, runner_on_unreachable, ...), changed/failed flags, and the result message. Filter with failed_only, host, or task."),
		func(ctx context.Context, c *ascender.Client, in struct {
			JobID      int    `json:"job_id" jsonschema:"regular job ID (workflows have no events of their own)"`
			FailedOnly bool   `json:"failed_only,omitempty" jsonschema:"return only events where failed=true"`
			Host       string `json:"host,omitempty" jsonschema:"filter to events for this hostname"`
			Task       string `json:"task,omitempty" jsonschema:"filter to events for this task name"`
			PageSize   int    `json:"page_size,omitempty" jsonschema:"results per page (default 50, max 200)"`
		}) (ascender.Page, error) {
			q := query(in.PageSize, 50)
			q.Set("order_by", "counter")
			if in.FailedOnly {
				q.Set("failed", "true")
			}
			setQuery(q, "host", in.Host)
			setQuery(q, "task", in.Task)
			return c.List(ctx, fmt.Sprintf("jobs/%d/job_events", in.JobID), q)
		})

	add(r, read("get_job_host_summaries",
		"Per-host pass/fail/changed/skipped/dark counters for a job. Answers 'which hosts succeeded vs failed' without parsing stdout."),
		func(ctx context.Context, c *ascender.Client, in struct {
			JobID    int `json:"job_id" jsonschema:"job ID"`
			PageSize int `json:"page_size,omitempty" jsonschema:"results per page (default 100, max 200)"`
		}) (ascender.Page, error) {
			return c.List(ctx, fmt.Sprintf("jobs/%d/job_host_summaries", in.JobID), query(in.PageSize, 100))
		})

	add(r, read("list_workflow_nodes",
		"List the nodes of a workflow job, each carrying the child job ID it spawned. Use this to walk into a workflow's underlying jobs (e.g. to fetch stdout)."),
		func(ctx context.Context, c *ascender.Client, in struct {
			WorkflowJobID int `json:"workflow_job_id" jsonschema:"workflow job ID"`
			PageSize      int `json:"page_size,omitempty" jsonschema:"results per page (default 50, max 200)"`
		}) (ascender.Page, error) {
			return c.List(ctx, fmt.Sprintf("workflow_jobs/%d/workflow_nodes", in.WorkflowJobID), query(in.PageSize, 50))
		})

	add(r, read("wait_for_job",
		"Poll a job until it reaches a terminal state (successful, failed, error, canceled) or timeout_seconds elapses. Returns the final record; on timeout returns an error so the caller can decide whether to keep polling."),
		func(ctx context.Context, c *ascender.Client, in struct {
			JobID          int    `json:"job_id" jsonschema:"job ID"`
			Kind           string `json:"kind,omitempty" jsonschema:"\"job\" (default) or \"workflow\""`
			TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"max wait in seconds (default 60, capped at 1800)"`
		}) (map[string]any, error) {
			endpoint, err := jobEndpoint(in.Kind)
			if err != nil {
				return nil, err
			}
			timeout := min(max(in.TimeoutSeconds, 1), 1800)
			if in.TimeoutSeconds <= 0 {
				timeout = 60
			}
			return waitForJob(ctx, c, fmt.Sprintf("%s/%d", endpoint, in.JobID), time.Duration(timeout)*time.Second)
		})

	add(r, writeIdempotent("cancel_job",
		"Cancel a running job. No-op if the job is not in a cancellable state (terminal or pre-launch). Set kind=\"workflow\" for workflow jobs."),
		func(ctx context.Context, c *ascender.Client, in jobIDArgs) (map[string]any, error) {
			endpoint, err := jobEndpoint(in.Kind)
			if err != nil {
				return nil, err
			}
			return cancel(ctx, c, fmt.Sprintf("%s/%d", endpoint, in.JobID))
		})

	add(r, write("relaunch_job",
		"Re-run a job with the same parameters the original used. Pass hosts=\"failed\" to re-run only against hosts that failed or were unreachable; hosts=\"all\" repeats against every host; default repeats the original's host set."),
		func(ctx context.Context, c *ascender.Client, in struct {
			JobID int    `json:"job_id" jsonschema:"original job ID to relaunch"`
			Hosts string `json:"hosts,omitempty" jsonschema:"\"all\", \"failed\", or omit to repeat the original"`
		}) (map[string]any, error) {
			switch in.Hosts {
			case "", "all", "failed":
			default:
				return nil, fmt.Errorf(`invalid hosts %q: expected "all" or "failed"`, in.Hosts)
			}
			body := map[string]any{}
			setIf(body, "hosts", in.Hosts)
			return c.Post(ctx, fmt.Sprintf("jobs/%d/relaunch", in.JobID), body)
		})

	add(r, write("relaunch_workflow",
		"Re-run a workflow with the same parameters as the original. AWX does not support failed-hosts-only relaunch for workflows."),
		func(ctx context.Context, c *ascender.Client, in struct {
			WorkflowJobID int `json:"workflow_job_id" jsonschema:"original workflow job ID to relaunch"`
		}) (map[string]any, error) {
			return c.Post(ctx, fmt.Sprintf("workflow_jobs/%d/relaunch", in.WorkflowJobID), nil)
		})
}

func setQuery(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}

// cancel POSTs to path/cancel and returns the fresh record. AWX answers
// 405 when the job is already terminal, which counts as success here.
func cancel(ctx context.Context, c *ascender.Client, path string) (map[string]any, error) {
	_, err := c.Post(ctx, path+"/cancel", nil)
	var apiErr *ascender.Error
	if err != nil && !(errors.As(err, &apiErr) && apiErr.Status == 405) {
		return nil, err
	}
	return c.Get(ctx, path, nil)
}

var terminalJobStatuses = map[string]bool{
	"successful": true, "failed": true, "error": true, "canceled": true,
}

// waitForJob polls path until the job is terminal or timeout passes.
// Upstream errors are tolerated for 90s with backoff, long enough to
// ride out an Ascender restart.
func waitForJob(ctx context.Context, c *ascender.Client, path string, timeout time.Duration) (map[string]any, error) {
	const errorGrace = 90 * time.Second
	deadline := time.Now().Add(timeout)
	sleep := time.Second
	var firstError time.Time
	for {
		job, err := c.Get(ctx, path, nil)
		switch {
		case err == nil:
			if status, _ := job["status"].(string); terminalJobStatuses[status] {
				return job, nil
			}
			firstError, sleep = time.Time{}, 2*time.Second
		case ctx.Err() != nil:
			return nil, ctx.Err()
		default:
			if firstError.IsZero() {
				firstError = time.Now()
			}
			if time.Since(firstError) > errorGrace {
				return nil, fmt.Errorf("upstream unreachable for %s: %w", time.Since(firstError).Round(time.Second), err)
			}
			sleep = min(sleep*2, 30*time.Second)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, fmt.Errorf("timeout after %s; job still non-terminal", timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(min(sleep, remaining)):
		}
	}
}
