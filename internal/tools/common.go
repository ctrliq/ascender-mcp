package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

// Argument and result shapes shared by several tools.

type noArgs struct{}

type listArgs struct {
	Search   string `json:"search,omitempty" jsonschema:"substring filter on name"`
	PageSize int    `json:"page_size,omitempty" jsonschema:"results per page (default 20, max 200)"`
}

type idOrNameArgs struct {
	IDOrName string `json:"id_or_name" jsonschema:"numeric ID or exact (case-sensitive) name"`
}

type deleted struct {
	Deleted bool `json:"deleted"`
	ID      int  `json:"id"`
}

// Generic handlers for the uniform parts of the AWX API.

func list(endpoint string) handler[listArgs, ascender.Page] {
	return func(ctx context.Context, c *ascender.Client, in listArgs) (ascender.Page, error) {
		q := query(in.PageSize, 20)
		if in.Search != "" {
			q.Set("search", in.Search)
		}
		return c.List(ctx, endpoint, q)
	}
}

func get(endpoint string) handler[idOrNameArgs, map[string]any] {
	return func(ctx context.Context, c *ascender.Client, in idOrNameArgs) (map[string]any, error) {
		return c.Resolve(ctx, endpoint, in.IDOrName)
	}
}

func del(ctx context.Context, c *ascender.Client, endpoint string, id int) (deleted, error) {
	if err := c.Delete(ctx, fmt.Sprintf("%s/%d", endpoint, id)); err != nil {
		return deleted{}, err
	}
	return deleted{Deleted: true, ID: id}, nil
}

// resolveID looks up a record by ID or name and returns its numeric ID.
func resolveID(ctx context.Context, c *ascender.Client, endpoint, idOrName string) (int, error) {
	record, err := c.Resolve(ctx, endpoint, idOrName)
	if err != nil {
		return 0, err
	}
	return ascender.ID(record)
}

// associate toggles one member of an AWX many-to-many relation and
// returns the updated collection.
func associate(ctx context.Context, c *ascender.Client, path string, childID int, disassociate bool) (map[string]any, error) {
	body := map[string]any{"id": childID}
	if disassociate {
		body["disassociate"] = true
	}
	return c.Post(ctx, path, body)
}

// replaceRelation sets an ordered many-to-many relation to exactly ids.
// AWX only offers per-item add and remove, and order matters for
// instance groups, so the only correct strategy is clear then re-add.
// A mid-sequence failure leaves the relation partially applied; the
// error says where it stopped.
func replaceRelation(ctx context.Context, c *ascender.Client, path string, ids []int) (ascender.Page, error) {
	current, err := c.List(ctx, path, nil)
	if err != nil {
		return ascender.Page{}, err
	}
	var currentIDs []int
	for _, item := range current.Results {
		if id, err := ascender.ID(item); err == nil {
			currentIDs = append(currentIDs, id)
		}
	}
	if slices.Equal(currentIDs, ids) {
		return current, nil
	}
	for _, id := range currentIDs {
		if _, err := associate(ctx, c, path, id, true); err != nil {
			return ascender.Page{}, fmt.Errorf("disassociate %d failed: %w", id, err)
		}
	}
	for _, id := range ids {
		if _, err := associate(ctx, c, path, id, false); err != nil {
			return ascender.Page{}, fmt.Errorf("associate %d failed: %w", id, err)
		}
	}
	return c.List(ctx, path, nil)
}

// Request-building helpers.

// query returns url.Values with page_size clamped to [1, 200], AWX's
// server-side cap, or fallback when unset.
func query(pageSize, fallback int) url.Values {
	switch {
	case pageSize <= 0:
		pageSize = fallback
	case pageSize > 200:
		pageSize = 200
	}
	return url.Values{"page_size": {strconv.Itoa(pageSize)}}
}

// set adds key when the optional value was provided.
func set[T any](p map[string]any, key string, v *T) {
	if v != nil {
		p[key] = *v
	}
}

// setIf adds key when v is not its zero value.
func setIf[T comparable](p map[string]any, key string, v T) {
	var zero T
	if v != zero {
		p[key] = v
	}
}

// setJSON adds a map field as the JSON string AWX expects for
// variables, extra_vars, and similar fields. A nil map (key omitted)
// leaves the field alone; an empty map clears it.
func setJSON(p map[string]any, key string, v map[string]any) {
	if v == nil {
		return
	}
	b, _ := json.Marshal(v)
	p[key] = string(b)
}
