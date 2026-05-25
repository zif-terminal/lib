package models

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SpotBalanceSnapshot represents a point-in-time spot balance for one asset (DB model).
//
// OraclePrice and UsdValue are nullable because some sync paths (e.g. exchanges
// whose APIs don't return per-asset USD pricing) may legitimately omit them.
// They are stored as decimal strings to preserve precision through Hasura's
// numeric(36,18) columns (float64 round-trips lose digits on big balances).
type SpotBalanceSnapshot struct {
	ID                uuid.UUID `json:"id"`
	ExchangeAccountID uuid.UUID `json:"exchange_account_id"`
	Asset             string    `json:"asset"`
	Balance           string    `json:"balance"`
	OraclePrice       *string   `json:"oracle_price,omitempty"`
	UsdValue          *string   `json:"usd_value,omitempty"`
	Timestamp         time.Time `json:"timestamp"`
}

// UnmarshalJSON handles Hasura returning BIGINT timestamp as number and NUMERIC as numbers.
func (s *SpotBalanceSnapshot) UnmarshalJSON(data []byte) error {
	type Alias SpotBalanceSnapshot
	aux := &struct {
		Timestamp   interface{} `json:"timestamp"`
		Balance     interface{} `json:"balance"`
		OraclePrice interface{} `json:"oracle_price"`
		UsdValue    interface{} `json:"usd_value"`
		*Alias
	}{
		Alias: (*Alias)(s),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if aux.Timestamp != nil {
		ts, err := parseTimestamp(aux.Timestamp)
		if err != nil {
			return fmt.Errorf("failed to parse spot balance snapshot timestamp: %w", err)
		}
		s.Timestamp = ts
	}
	if aux.Balance != nil {
		s.Balance = convertToString(aux.Balance)
	}
	// oracle_price and usd_value arrive as JSON numbers from Hasura
	// (numeric(36,18) columns). Round-trip them through convertToString to
	// preserve precision, then keep nil pointers when the columns are NULL.
	if aux.OraclePrice != nil {
		v := convertToString(aux.OraclePrice)
		s.OraclePrice = &v
	}
	if aux.UsdValue != nil {
		v := convertToString(aux.UsdValue)
		s.UsdValue = &v
	}

	return nil
}

// SpotBalanceSnapshotInput is input for creating a snapshot record.
//
// OraclePrice/UsdValue mirror the nullable numeric(36,18) columns and use
// pointer-to-string semantics: nil means "do not insert this column" (the DB
// default of NULL applies). Snapshot syncers MUST populate both whenever the
// upstream exchange exposes a price for the asset; leaving them nil silently
// produces NULL rows that downstream dashboards then sum as $0, masking real
// holdings as fake reconciliation gaps.
type SpotBalanceSnapshotInput struct {
	ExchangeAccountID uuid.UUID `json:"exchange_account_id"`
	Asset             string    `json:"asset"`
	Balance           string    `json:"balance"`
	OraclePrice       *string   `json:"oracle_price,omitempty"`
	UsdValue          *string   `json:"usd_value,omitempty"`
	Timestamp         time.Time `json:"timestamp"`
}
