package models

// Exchange represents a supported exchange in the database
// Matches the 'exchanges' table schema
type Exchange struct {
	ID             string `json:"id" db:"id"`
	Name           string `json:"name" db:"name"`
	DisplayName    string `json:"display_name" db:"display_name"`
	// RequiresAPIKey indicates that accessing this exchange's data requires an
	// API key from the user.  When true, the exchange's account data is only
	// visible to authenticated sessions (A1.6 compliance).
	RequiresAPIKey bool   `json:"requires_api_key" db:"requires_api_key"`
}

// ExchangeInput is used for GraphQL mutations
type ExchangeInput struct {
	Name           string `json:"name"`
	DisplayName    string `json:"display_name"`
	RequiresAPIKey bool   `json:"requires_api_key"`
}
