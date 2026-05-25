package solana_dex

// Helius enhanced-transactions response shapes.
//
// Endpoint: GET https://api.helius.xyz/v0/addresses/<address>/transactions
//   ?api-key=<key>&limit=<n>&before=<signature>
//
// The endpoint returns a JSON array of transaction objects (no envelope).
// Pagination is signature-based: pass `before=<oldest signature in last page>`
// to fetch the next, older page. End of stream is signaled by an empty array.
//
// Docs: https://docs.helius.dev/api-reference/enhanced-transactions-api

// heliusTransaction is a single parsed transaction from the enhanced API.
// We decode only the fields we use; Helius surfaces many more (fee/feePayer/
// description/events/lighthouseData/...) which we ignore.
type heliusTransaction struct {
	Signature   string                 `json:"signature"`
	Timestamp   int64                  `json:"timestamp"` // Unix seconds
	Slot        int64                  `json:"slot"`
	Type        string                 `json:"type"`   // SWAP, TRANSFER, UNKNOWN, ...
	Source      string                 `json:"source"` // JUPITER, DRIFT, SYSTEM_PROGRAM, ...
	Description string                 `json:"description"`
	Fee         int64                  `json:"fee"`     // lamports
	FeePayer    string                 `json:"feePayer"`
	TxError     interface{}            `json:"transactionError"` // null on success

	NativeTransfers []heliusNativeTransfer `json:"nativeTransfers"`
	TokenTransfers  []heliusTokenTransfer  `json:"tokenTransfers"`
	AccountData     []heliusAccountData    `json:"accountData"`
	Instructions    []heliusInstruction    `json:"instructions"`
	Events          map[string]interface{} `json:"events"`
}

// heliusNativeTransfer is a SOL transfer parsed by Helius.
// Amount is in lamports (1 SOL = 1e9 lamports).
type heliusNativeTransfer struct {
	FromUserAccount string `json:"fromUserAccount"`
	ToUserAccount   string `json:"toUserAccount"`
	Amount          int64  `json:"amount"`
}

// heliusTokenTransfer is an SPL token transfer parsed by Helius.
// TokenAmount is the human-readable amount (already adjusted for decimals).
// Mint is the SPL token mint address.
type heliusTokenTransfer struct {
	FromTokenAccount string  `json:"fromTokenAccount"`
	ToTokenAccount   string  `json:"toTokenAccount"`
	FromUserAccount  string  `json:"fromUserAccount"`
	ToUserAccount    string  `json:"toUserAccount"`
	TokenAmount      float64 `json:"tokenAmount"`
	Mint             string  `json:"mint"`
	TokenStandard    string  `json:"tokenStandard"`
}

// heliusAccountData carries per-account balance deltas for the tx.
// We use NativeBalanceChange to recover precise lamport-level SOL deltas,
// and TokenBalanceChanges (rawTokenAmount) to recover precise SPL deltas
// without relying on the lossy float64 in tokenTransfers.
type heliusAccountData struct {
	Account             string                     `json:"account"`
	NativeBalanceChange int64                      `json:"nativeBalanceChange"`
	TokenBalanceChanges []heliusTokenBalanceChange `json:"tokenBalanceChanges"`
}

// heliusTokenBalanceChange is a per-account SPL balance delta carried in
// accountData. RawTokenAmount.TokenAmount is the integer base-units delta
// (signed, decoded as a string) and Decimals tells us where to put the dot.
type heliusTokenBalanceChange struct {
	UserAccount    string                  `json:"userAccount"`
	TokenAccount   string                  `json:"tokenAccount"`
	Mint           string                  `json:"mint"`
	RawTokenAmount heliusRawTokenAmount    `json:"rawTokenAmount"`
}

type heliusRawTokenAmount struct {
	TokenAmount string `json:"tokenAmount"` // signed integer as string
	Decimals    int    `json:"decimals"`
}

// heliusInstruction is one top-level instruction. We use ProgramID to detect
// Drift program interactions for the dedup rule (skip the entire Helius tx
// when any top-level instruction is a Drift program ID — Drift's own sync
// already records those events).
type heliusInstruction struct {
	Accounts          []string            `json:"accounts"`
	Data              string              `json:"data"`
	ProgramID         string              `json:"programId"`
	InnerInstructions []heliusInstruction `json:"innerInstructions"`
}

// heliusBalancesResp is the response from GET /v0/addresses/<addr>/balances.
// nativeBalance is in lamports.
type heliusBalancesResp struct {
	Tokens        []heliusTokenBalance `json:"tokens"`
	NativeBalance int64                `json:"nativeBalance"`
}

type heliusTokenBalance struct {
	TokenAccount string `json:"tokenAccount"`
	Mint         string `json:"mint"`
	Amount       int64  `json:"amount"`   // raw, integer base units
	Decimals     int    `json:"decimals"`
}

// jupiterPriceEntry is one entry in Jupiter's `lite-api.jup.ag/price/v3`
// response. The endpoint returns a JSON object keyed by mint address; we
// only need usdPrice. Other fields surfaced by Jupiter (decimals,
// blockId, priceChange24h, ...) are intentionally ignored.
//
// Endpoint: GET https://lite-api.jup.ag/price/v3?ids=<mint1>,<mint2>,...
// Response shape:
//
//	{
//	  "<mint>": { "usdPrice": <number>, "decimals": <int>, ... },
//	  ...
//	}
//
// Mints with no price (illiquid / delisted) are simply absent from the
// response — callers must treat "missing" as "skip pricing for that asset",
// not as zero.
type jupiterPriceEntry struct {
	UsdPrice float64 `json:"usdPrice"`
}
