package models

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// EventValue represents a computed monetary value for an event in a specific denomination.
// Maps to the 'event_values' table.
type EventValue struct {
	ID           uuid.UUID `json:"id"`
	EventID      uuid.UUID `json:"event_id"`
	EventType    string    `json:"event_type"`    // "trade", "transfer", "settlement"
	Denomination string    `json:"denomination"`  // e.g. "USDC"
	Quantity     string    `json:"quantity"`       // NUMERIC(36,18)
}

// UnmarshalJSON handles Hasura returning NUMERIC fields as JSON numbers.
func (ev *EventValue) UnmarshalJSON(data []byte) error {
	type Alias EventValue
	aux := &struct {
		Quantity interface{} `json:"quantity"`
		*Alias
	}{
		Alias: (*Alias)(ev),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.Quantity != nil {
		switch val := aux.Quantity.(type) {
		case string:
			ev.Quantity = val
		case float64:
			ev.Quantity = strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.18f", val), "0"), ".")
		default:
			ev.Quantity = fmt.Sprintf("%v", val)
		}
	}
	return nil
}

// EventValueInput represents input for creating an event value record.
type EventValueInput struct {
	EventID      uuid.UUID `json:"event_id"`
	EventType    string    `json:"event_type"`
	Denomination string    `json:"denomination"`
	Quantity     string    `json:"quantity"`
}

// MissingEventValue represents a gap: an event that does not yet have
// a value row for a given denomination. Returned by the missing_event_values view.
type MissingEventValue struct {
	EventID      uuid.UUID `json:"event_id"`
	EventType    string    `json:"event_type"`
	Denomination string    `json:"denomination"`
}
