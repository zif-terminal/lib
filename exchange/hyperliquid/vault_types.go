package hyperliquid

// hyperliquidVaultSummary represents a vault from Hyperliquid's vaultSummaries list.
// Returned by POST /info {"type": "vaultSummaries"}.
type hyperliquidVaultSummary struct {
	Name         string  `json:"name"`
	VaultAddress string  `json:"vaultAddress"` // on-chain address (0x...)
	Leader       string  `json:"leader"`
	Tvl          float64 `json:"tvl"`      // total value locked (USD), may be 0
	IsClosed     bool    `json:"isClosed"`
}

// hyperliquidVaultDetailsResponse is the full detail response for a single vault.
// Returned by POST /info {"type": "vaultDetails", "vaultAddress": "0x..."}.
type hyperliquidVaultDetailsResponse struct {
	Name             string  `json:"name"`
	VaultAddress     string  `json:"vaultAddress"`
	Leader           string  `json:"leader"`
	Description      string  `json:"description"`
	Apr              float64 `json:"apr"`
	MaxDistributable float64 `json:"maxDistributable"` // best TVL proxy available
	IsClosed         bool    `json:"isClosed"`
	AllowDeposits    bool    `json:"allowDeposits"`
}

// hyperliquidUserVaultEquity is one element in the array returned by
// POST /info {"type": "userVaultEquities", "user": "0x..."}.
type hyperliquidUserVaultEquity struct {
	VaultAddress string  `json:"vaultAddress"`
	Equity       float64 `json:"equity"` // user equity in USD
}

// VaultSummary is the public representation of a Hyperliquid vault returned by
// FetchVaults and FetchVaultDetails.
type VaultSummary struct {
	Address     string
	Name        string
	Leader      string
	Description string
	TVL         float64
	APR         float64
	IsClosed    bool
}

// UserVaultEquity represents a user's equity in one vault.
type UserVaultEquity struct {
	VaultAddress string
	Equity       float64
}
