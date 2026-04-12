package models

import (
	"encoding/json"
	"time"
)

// SystemFlag represents a row from the system_flags table.
type SystemFlag struct {
	Name      string          `json:"name"`
	Value     json.RawMessage `json:"value"`
	UpdatedAt time.Time       `json:"updated_at"`
}
