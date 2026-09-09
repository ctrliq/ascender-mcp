package tools

import (
	"context"
	"fmt"
	"strconv"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

type schedulePreview struct {
	Valid         bool     `json:"valid"`
	NextRunsUTC   []string `json:"next_runs_utc"`
	NextRunsLocal []string `json:"next_runs_local"`
}

type scheduleFields struct {
	Description *string        `json:"description,omitempty"`
	Enabled     *bool          `json:"enabled,omitempty" jsonschema:"default true; false pauses the schedule"`
	ExtraData   map[string]any `json:"extra_data,omitempty" jsonschema:"Ansible variables passed when the schedule fires; pass {} to clear"`
	InventoryID *int           `json:"inventory_id,omitempty" jsonschema:"override inventory (dropped if ask_inventory_on_launch is false)"`
	Limit       *string        `json:"limit,omitempty" jsonschema:"Ansible host limit pattern (dropped if ask_limit_on_launch is false)"`
	JobTags     *string        `json:"job_tags,omitempty"`
	SkipTags    *string        `json:"skip_tags,omitempty"`
	ScmBranch   *string        `json:"scm_branch,omitempty"`
	JobType     *string        `json:"job_type,omitempty" jsonschema:"\"run\" or \"check\""`
	DiffMode    *bool          `json:"diff_mode,omitempty"`
	Verbosity   *int           `json:"verbosity,omitempty" jsonschema:"0..5"`
	Forks       *int           `json:"forks,omitempty"`
	Timeout     *int           `json:"timeout,omitempty" jsonschema:"job timeout in seconds"`
}

func (f scheduleFields) payload() map[string]any {
	p := map[string]any{}
	set(p, "description", f.Description)
	set(p, "enabled", f.Enabled)
	if f.ExtraData != nil {
		p["extra_data"] = f.ExtraData
	}
	set(p, "inventory", f.InventoryID)
	set(p, "limit", f.Limit)
	set(p, "job_tags", f.JobTags)
	set(p, "skip_tags", f.SkipTags)
	set(p, "scm_branch", f.ScmBranch)
	set(p, "job_type", f.JobType)
	set(p, "diff_mode", f.DiffMode)
	set(p, "verbosity", f.Verbosity)
	set(p, "forks", f.Forks)
	set(p, "timeout", f.Timeout)
	return p
}

func registerSchedules(r *registry) {
	add(r, read("list_schedules",
		"List schedules (recurring template/workflow launches). Filter by template_id, or by enabled to see only active or only paused schedules."),
		func(ctx context.Context, c *ascender.Client, in struct {
			TemplateID int    `json:"template_id,omitempty" jsonschema:"job template or workflow template ID"`
			Enabled    string `json:"enabled,omitempty" jsonschema:"\"\" (both), \"true\", or \"false\""`
			listArgs
		}) (ascender.Page, error) {
			switch in.Enabled {
			case "", "true", "false":
			default:
				return ascender.Page{}, fmt.Errorf(`invalid enabled %q: expected "true" or "false"`, in.Enabled)
			}
			q := query(in.PageSize, 20)
			if in.TemplateID != 0 {
				q.Set("unified_job_template", strconv.Itoa(in.TemplateID))
			}
			setQuery(q, "search", in.Search)
			setQuery(q, "enabled", in.Enabled)
			return c.List(ctx, "schedules", q)
		})

	add(r, read("get_schedule",
		"Get a schedule by numeric ID or exact name. `rrule` is the RFC 5545 recurrence rule; `next_run` is the next fire time."),
		get("schedules"))

	add(r, read("preview_schedule",
		"Validate an RFC 5545 RRULE and return the next 10 fire times in UTC and local time. Use before create_schedule."),
		func(ctx context.Context, c *ascender.Client, in struct {
			RRule string `json:"rrule" jsonschema:"RFC 5545 ruleset. DTSTART required with a timezone (Z or ;TZID=America/New_York). RRULE must include INTERVAL. Example: DTSTART;TZID=America/New_York:20260101T090000 RRULE:FREQ=WEEKLY;INTERVAL=1;BYDAY=MO,WE,FR"`
		}) (schedulePreview, error) {
			out, err := c.Post(ctx, "schedules/preview", map[string]any{"rrule": in.RRule})
			if err != nil {
				return schedulePreview{}, err
			}
			return schedulePreview{Valid: true, NextRunsUTC: stringSlice(out["utc"]), NextRunsLocal: stringSlice(out["local"])}, nil
		})

	add(r, write("create_schedule",
		"Create a recurring schedule for a job template or workflow template. Validate the rrule with preview_schedule first. Prompt-on-launch fields (inventory_id, limit, extra_data, ...) are silently dropped if the template's matching ask_*_on_launch flag is false; check get_launch_config."),
		func(ctx context.Context, c *ascender.Client, in struct {
			TemplateID int    `json:"template_id" jsonschema:"unified_job_template ID (job template, workflow template, project, inventory source, or system job template)"`
			Name       string `json:"name" jsonschema:"schedule name (unique per template)"`
			RRule      string `json:"rrule" jsonschema:"RFC 5545 ruleset; see preview_schedule"`
			scheduleFields
		}) (map[string]any, error) {
			p := in.payload()
			p["name"], p["rrule"], p["unified_job_template"] = in.Name, in.RRule, in.TemplateID
			return c.Post(ctx, "schedules", p)
		})

	add(r, writeIdempotent("update_schedule",
		"Partially update a schedule: rotate the rrule, change extra_data, rename, or toggle enabled. Validate a new rrule with preview_schedule first."),
		func(ctx context.Context, c *ascender.Client, in struct {
			ScheduleID int     `json:"schedule_id"`
			Name       *string `json:"name,omitempty"`
			RRule      *string `json:"rrule,omitempty"`
			scheduleFields
		}) (map[string]any, error) {
			p := in.payload()
			set(p, "name", in.Name)
			set(p, "rrule", in.RRule)
			return c.Patch(ctx, fmt.Sprintf("schedules/%d", in.ScheduleID), p)
		})

	add(r, writeIdempotent("delete_schedule",
		"Delete a schedule. Jobs it already launched keep their history. Prefer update_schedule(enabled=false) for a reversible pause."),
		func(ctx context.Context, c *ascender.Client, in struct {
			ScheduleID int `json:"schedule_id"`
		}) (deleted, error) {
			return del(ctx, c, "schedules", in.ScheduleID)
		})
}

// stringSlice converts a decoded JSON array to []string, skipping non-strings.
func stringSlice(v any) []string {
	items, _ := v.([]any)
	var out []string
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
