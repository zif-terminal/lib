package hyperliquid

import "encoding/json"

// Hyperliquid API request/response types
// Base URL: https://api.hyperliquid.xyz/info (POST with JSON body)

// hlFill represents a single trade fill from Hyperliquid API
// Request: {"type": "userFillsByTime", "user": "0x...", "startTime": 1234567890000}
// Returns fills sorted by timestamp ascending (oldest first).
type hlFill struct {
	Time      int64  `json:"time"`      // Unix milliseconds
	Coin      string `json:"coin"`      // e.g., "ETH" for perp, "ETH-SPOT" for spot
	Side      string `json:"side"`      // "B" (buy) or "A" (sell/ask)
	Px        string `json:"px"`        // Price
	Sz        string `json:"sz"`        // Size/quantity
	Fee       string `json:"fee"`       // Fee amount (denominated in FeeToken)
	Tid       int64  `json:"tid"`       // Trade ID (unique)
	ClosedPnl string `json:"closedPnl"` // Realized PnL on this fill
	Hash      string `json:"hash"`      // Transaction hash
	StartPosition string `json:"startPosition"` // Position before fill
	Dir       string `json:"dir"`       // Direction description (e.g., "Open Long", "Close Short", "Sell", "Spot Dust Conversion")
	Oid       int64  `json:"oid"`       // Order ID
	FeeToken  string `json:"feeToken"`  // Fee denomination token
}

// hlFundingEntry represents a single funding payment from Hyperliquid API
// Request: {"type": "userFunding", "user": "0x...", "startTime": 1234567890000, "endTime": ...}
// Response wraps funding fields inside a "delta" object.
type hlFundingEntry struct {
	Time  int64          `json:"time"` // Unix milliseconds
	Hash  string         `json:"hash"` // Transaction hash
	Delta hlFundingDelta `json:"delta"`
}

// hlFundingDelta holds the funding payment details inside the delta wrapper
type hlFundingDelta struct {
	Coin        string `json:"coin"`        // e.g., "ETH"
	FundingRate string `json:"fundingRate"` // Funding rate
	NSamples    int    `json:"nSamples"`    // Number of samples (1 = hourly, 24 = daily)
	Usdc        string `json:"usdc"`        // USDC amount (signed)
	Szi         string `json:"szi"`         // Position size at time of funding
	Type        string `json:"type"`        // Always "funding"
}

// hlLedgerEntry represents a non-funding ledger update from Hyperliquid API
// Request: {"type": "userNonFundingLedgerUpdates", "user": "0x...", "startTime": ...}
type hlLedgerEntry struct {
	Time  int64         `json:"time"` // Unix milliseconds
	Hash  string        `json:"hash"` // Transaction hash
	Delta hlLedgerDelta `json:"delta"`
}

// hlLedgerDelta holds the details of a ledger update
type hlLedgerDelta struct {
	Type        string `json:"type"`        // "deposit", "withdraw", "internalTransfer", "spotTransfer", etc.
	Usdc        string `json:"usdc"`        // USDC amount (string)
	Amount      string `json:"amount"`      // Amount (sometimes used instead of usdc)
	Token       string `json:"token"`       // Token name (for non-USDC)
	Fee         string `json:"fee"`         // Fee (if applicable)
	Nonce       int64  `json:"nonce"`       // Nonce
	Destination string `json:"destination"` // Destination address (for withdrawals/spotTransfer)
	User        string `json:"user"`        // Source user address (for spotTransfer outbound)
	ToPerp          bool   `json:"toPerp"`          // Direction for accountClassTransfer (true = spot→perp)
	UsdcValue       string `json:"usdcValue"`       // USDC value of spot transfer (for price derivation)
	NetWithdrawnUsd string `json:"netWithdrawnUsd"` // Actual USDC returned to user on vaultWithdraw
	IsDeposit       bool   `json:"isDeposit"`       // Direction for cStakingTransfer (true = stake/leave balance)
	Vault           string `json:"vault"`           // Vault address (for vault-related entries)
}

// hlClearinghouseState represents the clearinghouse state for a user
// Request: {"type": "clearinghouseState", "user": "0x..."}
type hlClearinghouseState struct {
	AssetPositions []struct {
		Position struct {
			Coin          string `json:"coin"`
			Szi           string `json:"szi"`
			EntryPx       string `json:"entryPx"`
			PositionValue string `json:"positionValue"`
			UnrealizedPnl string `json:"unrealizedPnl"`
		} `json:"position"`
	} `json:"assetPositions"`
	MarginSummary struct {
		AccountValue string `json:"accountValue"`
		TotalRawUsd  string `json:"totalRawUsd"`
	} `json:"marginSummary"`
}

// hlSpotClearinghouseState represents the spot clearinghouse state for a user
// Request: {"type": "spotClearinghouseState", "user": "0x..."}
type hlSpotClearinghouseState struct {
	Balances []struct {
		Coin  string `json:"coin"`
		Total string `json:"total"`
		Hold  string `json:"hold"`
	} `json:"balances"`
}

// hlSubAccount represents a subaccount returned by the subAccounts API
// Request: {"type": "subAccounts", "user": "0x..."}
// Returns null (no subaccounts) or a list of hlSubAccount.
type hlSubAccount struct {
	SubAccountUser string `json:"subAccountUser"`
	Name           string `json:"name"`
	Master         string `json:"master"`
}

// hlBorrowLendInterest represents a single borrow/lend interest entry from Hyperliquid API
// Request: {"type": "userBorrowLendInterest", "user": "0x...", "startTime": 0}
// Returns hourly interest entries per token, sorted ascending by time.
type hlBorrowLendInterest struct {
	Time   int64  `json:"time"`   // Unix milliseconds
	Token  string `json:"token"`  // e.g., "USDC", "HYPE"
	Borrow string `json:"borrow"` // Interest paid (cost)
	Supply string `json:"supply"` // Interest earned (yield)
}

// hlVaultEquity represents a vault the user participates in
// Request: {"type": "userVaultEquities", "user": "0x..."}
type hlVaultEquity struct {
	VaultAddress string `json:"vaultAddress"`
	Equity       string `json:"equity"`
}

// hlVaultDetails represents vault details from the API
// Request: {"type": "vaultDetails", "vaultAddress": "0x..."}
type hlVaultDetails struct {
	Name       string `json:"name"`
	VaultAddress string `json:"vaultAddress"`
	Leader     string `json:"leader"`
}

// hlDelegatorHistoryEntry models entries from the HL delegatorHistory endpoint.
// These cover HYPE consensus staking events (cDeposit, delegate, undelegate,
// withdrawal lifecycle). cDeposit and cWithdraw are the only event types here
// that move HYPE in or out of the trading balance — the rest happen entirely
// inside consensus staking and don't affect the trading wallet.
//
// Request: {"type": "delegatorHistory", "user": "0x...", "startTime": ...}
// Sample response (real call):
//
//	[{"time":1747323884335,"hash":"0xd6e8514d...","delta":{"cDeposit":{"amount":"1000.0"}}}]
type hlDelegatorHistoryEntry struct {
	Time     int64                   `json:"time"`  // Unix milliseconds
	Hash     string                  `json:"hash"`  // Transaction hash
	Delta    hlDelegatorHistoryDelta `json:"delta"` // Encode/decode driven by struct tag for tests; decode is overridden by UnmarshalJSON below to also capture RawDelta.
	RawDelta json.RawMessage         `json:"-"`     // Captured by UnmarshalJSON; preserves raw bytes so unknown variants surface in errors.
}

// UnmarshalJSON captures both the typed delta variants AND the raw delta JSON
// so that unhandled variants can crash-loud with the original payload visible.
// Without RawDelta, the silent-skip on `default:` would discard the only signal
// of which new HL event type appeared.
func (e *hlDelegatorHistoryEntry) UnmarshalJSON(data []byte) error {
	var raw struct {
		Time  int64           `json:"time"`
		Hash  string          `json:"hash"`
		Delta json.RawMessage `json:"delta"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	e.Time = raw.Time
	e.Hash = raw.Hash
	e.RawDelta = raw.Delta
	if len(raw.Delta) > 0 {
		if err := json.Unmarshal(raw.Delta, &e.Delta); err != nil {
			return err
		}
	}
	return nil
}

// hlDelegatorHistoryDelta wraps the various delta payloads. Exactly one field
// is set per entry. Every known HL delegatorHistory variant is modelled
// explicitly so transformDelegatorHistoryEntry can give each a named case
// (no-silent-unknowns policy). Only cDeposit/cWithdraw move the trading
// balance; the staking-internal lifecycle variants (delegate/undelegate,
// the unstaking-queue withdrawal phases) are modelled solely so they can be
// explicitly no-op'd rather than crash-loud or silently dropped.
type hlDelegatorHistoryDelta struct {
	// CDeposit: HYPE leaving trading balance into consensus staking.
	// Some wallets see this event ONLY in delegatorHistory; others see it
	// in BOTH delegatorHistory AND userNonFundingLedgerUpdates (as
	// cStakingTransfer{isDeposit:true}). FetchDeposits dedupes by hash so
	// we never emit two transfer rows for one on-chain event regardless
	// of which endpoint(s) reported it.
	CDeposit *hlCDepositDelta `json:"cDeposit,omitempty"`

	// CWithdraw: HYPE returning from consensus staking. Same dual-endpoint
	// situation as cDeposit — the primary path is cStakingTransfer
	// {isDeposit:false} in non-funding ledger, but some wallets also see
	// it here. Dedup happens in FetchDeposits.
	CWithdraw *hlCWithdrawDelta `json:"cWithdraw,omitempty"`

	// Delegate: HYPE delegated to / undelegated from a validator
	// (isUndelegate distinguishes the direction). This moves HYPE between
	// the staking account and a validator — entirely INSIDE the staking
	// account. It never touches the spot/trading balance, so it is
	// explicitly no-op'd in transformDelegatorHistoryEntry.
	Delegate *hlDelegateDelta `json:"delegate,omitempty"`

	// Withdrawal: a marker for the 7-day unstaking queue lifecycle.
	// phase=="initiated" when a staking->spot withdrawal enters the queue,
	// phase=="finalized" 7 days later when it clears. Neither phase moves
	// the trading balance HERE — the actual spot credit arrives via the
	// separate cWithdraw / cStakingTransfer{isDeposit:false} path, which
	// is the only thing we may emit a transfer for. Counting withdrawal
	// here too would double-count. Explicitly no-op'd (both phases).
	Withdrawal *hlWithdrawalDelta `json:"withdrawal,omitempty"`
}

type hlCDepositDelta struct {
	Amount string `json:"amount"`
}

type hlCWithdrawDelta struct {
	Amount string `json:"amount"`
}

type hlDelegateDelta struct {
	Validator    string `json:"validator"`
	Amount       string `json:"amount"`
	IsUndelegate bool   `json:"isUndelegate"`
}

type hlWithdrawalDelta struct {
	Amount string `json:"amount"`
	Phase  string `json:"phase"` // "initiated" | "finalized"
}
