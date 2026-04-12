package db

import (
	"context"
	"fmt"
	"time"

	"github.com/zif-terminal/lib/models"
)

// SystemFlag type alias
type SystemFlag = models.SystemFlag

// GetFlag reads a system flag by name. Returns nil if not found.
func (c *Client) GetFlag(ctx context.Context, name string) (*SystemFlag, error) {
	query := `
		query GetFlag($name: String!) {
			system_flags_by_pk(name: $name) {
				name
				value
				updated_at
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"name": name,
	})

	var resp struct {
		Flag *SystemFlag `json:"system_flags_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get flag %s: %w", name, err)
	}

	return resp.Flag, nil
}

// SetFlag upserts a flag (unconditional write, updates updated_at to now()).
func (c *Client) SetFlag(ctx context.Context, name string, value interface{}) error {
	query := `
		mutation SetFlag($object: system_flags_insert_input!) {
			insert_system_flags_one(
				object: $object
				on_conflict: {
					constraint: system_flags_pkey
					update_columns: [value, updated_at]
				}
			) {
				name
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"object": map[string]interface{}{
			"name":       name,
			"value":      value,
			"updated_at": "now()",
		},
	})

	var resp struct {
		InsertSystemFlagsOne *struct {
			Name string `json:"name"`
		} `json:"insert_system_flags_one"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to set flag %s: %w", name, err)
	}

	return nil
}

// CompareAndSetFlag updates a flag only if updated_at matches the flag's UpdatedAt (race-safe).
// Returns true if the update succeeded, false if the flag was touched by someone else.
func (c *Client) CompareAndSetFlag(ctx context.Context, name string, value interface{}, flag *SystemFlag) (bool, error) {
	query := `
		mutation CasFlag($name: String!, $value: jsonb!, $expected: timestamptz!) {
			update_system_flags(
				where: {
					name: { _eq: $name }
					updated_at: { _eq: $expected }
				}
				_set: {
					value: $value
					updated_at: "now()"
				}
			) {
				affected_rows
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"name":     name,
		"value":    value,
		"expected": flag.UpdatedAt.Format(time.RFC3339Nano),
	})

	var resp struct {
		UpdateSystemFlags struct {
			AffectedRows int `json:"affected_rows"`
		} `json:"update_system_flags"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return false, fmt.Errorf("failed to CAS flag %s: %w", name, err)
	}

	return resp.UpdateSystemFlags.AffectedRows == 1, nil
}
