package models

// Exchange represents a supported exchange in the database
// Matches the 'exchanges' table schema
type Exchange struct {
	ID             string `json:"id" db:"id"`
	Name           string `json:"name" db:"name"`
	DisplayName    string `json:"display_name" db:"display_name"`
	SettlesOnClose bool   `json:"settles_on_close" db:"settles_on_close"`
}

// ExchangeInput is used for GraphQL mutations
type ExchangeInput struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}
