package db

import (
	"context"
	"fmt"

	"github.com/zif-terminal/lib/models"
)

// EventValue type aliases
type EventValue = models.EventValue
type EventValueInput = models.EventValueInput
type MissingEventValue = models.MissingEventValue

// ListSupportedDenominations returns all supported denominations.
func (c *Client) ListSupportedDenominations(ctx context.Context) ([]string, error) {
	query := `
		query ListSupportedDenominations {
			supported_denominations {
				asset
			}
		}
	`

	req := c.graphqlRequest(query)

	var resp struct {
		SupportedDenominations []struct {
			Asset string `json:"asset"`
		} `json:"supported_denominations"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to list supported denominations: %w", err)
	}

	result := make([]string, len(resp.SupportedDenominations))
	for i, d := range resp.SupportedDenominations {
		result[i] = d.Asset
	}
	return result, nil
}

// ListMissingEventValues returns events that are missing values for supported denominations.
// Results are limited by the limit parameter to support batch processing.
func (c *Client) ListMissingEventValues(ctx context.Context, limit int) ([]*MissingEventValue, error) {
	query := `
		query ListMissingEventValues($limit: Int!) {
			missing_event_values(limit: $limit) {
				event_id
				event_type
				denomination
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"limit": limit,
	})

	var resp struct {
		MissingEventValues []*MissingEventValue `json:"missing_event_values"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to list missing event values: %w", err)
	}

	return resp.MissingEventValues, nil
}

// AddEventValues upserts event value records: on conflict with the unique
// (event_id, event_type, denomination) key, the existing row's quantity is
// updated to the new value. This is a real upsert, not a silent skip — it
// lets the processor refresh event values when quantities are recomputed.
// Inputs are written in chunks of 1000 to avoid Hasura payload limits.
func (c *Client) AddEventValues(ctx context.Context, inputs []*EventValueInput) (int, error) {
	if len(inputs) == 0 {
		return 0, nil
	}

	const chunkSize = 1000
	totalAffected := 0

	for start := 0; start < len(inputs); start += chunkSize {
		end := start + chunkSize
		if end > len(inputs) {
			end = len(inputs)
		}
		chunk := inputs[start:end]

		affected, err := c.addEventValuesChunk(ctx, chunk)
		if err != nil {
			return totalAffected, err
		}
		totalAffected += affected
	}

	return totalAffected, nil
}

func (c *Client) addEventValuesChunk(ctx context.Context, inputs []*EventValueInput) (int, error) {
	query := `
		mutation AddEventValues($objects: [event_values_insert_input!]!) {
			insert_event_values(
				objects: $objects
				on_conflict: {
					constraint: event_values_unique
					update_columns: [quantity]
				}
			) {
				affected_rows
			}
		}
	`

	objects := make([]map[string]interface{}, len(inputs))
	for i, input := range inputs {
		objects[i] = map[string]interface{}{
			"event_id":     input.EventID.String(),
			"event_type":   input.EventType,
			"denomination": input.Denomination,
			"quantity":     input.Quantity,
		}
	}

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"objects": objects,
	})

	var resp struct {
		InsertEventValues struct {
			AffectedRows int `json:"affected_rows"`
		} `json:"insert_event_values"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return 0, fmt.Errorf("failed to add event values: %w", err)
	}

	return resp.InsertEventValues.AffectedRows, nil
}
