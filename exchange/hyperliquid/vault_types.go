package hyperliquid

// hyperliquidVaultSummary represents a vault from Hyperliquid's public vaults list.
// Returned by POST /info {"type": "vaults"}.
type hyperliquidVaultSummary struct {
	Name        string  `json:"name"`
	Vault       string  `json:"vault"`       // vault on-chain address (0x...)
	Leader      string  `json:"leader"`      // leader's address
	Description string  `json:"description"` // optional
	Apr         float64 `json:"apr"`         // annualised return %
	Tvl         float64 `json:"tvl"`         // total value locked (USD)
	IsClosed    bool    `json:"isClosed"`
}

// hyperliquidVaultDetailsResponse is the full detail response for a single vault.
// Returned by POST /info {"type": "vaultDetails", "vaultAddress": "0x..."}.
type hyperliquidVaultDetailsResponse struct {
	Name        string  `json:"name"`
	Vault       string  `json:"vault"`
	Leader      string  `json:"leader"`
	Description string  `json:"description"`
	Apr         float64 `json:"apr"`
	Tvl         float64 `json:"tvl"`
	IsClosed    bool    `json:"isClosed"`
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
