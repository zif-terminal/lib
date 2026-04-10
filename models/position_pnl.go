package models

import "github.com/google/uuid"

// PositionPnLInput represents input for inserting a position PnL record.
type PositionPnLInput struct {
	PositionID   uuid.UUID `json:"position_id"`
	Denomination string    `json:"denomination"`
	RealizedPnL  string    `json:"realized_pnl"` // NUMERIC(36,18) as string
}

// MissingPositionPnL represents a closed position that does not yet have
// a PnL row for a given denomination. Returned by the missing_position_pnl view.
type MissingPositionPnL struct {
	PositionID   uuid.UUID `json:"position_id"`
	Denomination string    `json:"denomination"`
}
