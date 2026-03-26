package models

import "github.com/google/uuid"

// EventValue represents a computed monetary value for an event in a specific denomination.
// Maps to the 'event_values' table.
type EventValue struct {
	ID           uuid.UUID `json:"id"`
	EventID      uuid.UUID `json:"event_id"`
	EventType    string    `json:"event_type"`    // "trade", "transfer", "settlement"
	Denomination string    `json:"denomination"`  // e.g. "USDC"
	Quantity     string    `json:"quantity"`       // NUMERIC(36,18)
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
