package solana_dex

// SPL token mint → asset symbol mapping.
//
// Only includes mints we expect to see in the wallets we sync. If Helius
// returns a token whose mint is not in this map, the client uses a stable
// short prefix of the mint as the asset string and logs a warning. Adding a
// new symbol is a one-line change here.
//
// Source: well-known public mint addresses on Solana mainnet-beta. Cross-check
// at https://solscan.io/token/<mint> when adding new entries.

const (
	mintWSOL  = "So11111111111111111111111111111111111111112"
	mintUSDC  = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	mintUSDT  = "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB"
	mintJLP   = "27G8MtK7VtTcCHkpASjSDdkWWYfoqT6ggEuKidVJidD4"
	mintJUP   = "JUPyiwrYJFskUPiHa7hkeR8VUtAeFoSYbKedZNsDvCN"
	mintJTO   = "jtojtomepa8beP8AuQc6eXt5FriJwfFMwQx2v2f9mCL"
	mintJITOSOL = "J1toso1uCk3RLmjorhTtrVwY9HJ7X8V9yYac6Y7kGCPn"
	mintMSOL  = "mSoLzYCxHdYgdzU16g5QSh3i5K3z3KZK7ytfqcJm7So"
	mintBSOL  = "bSo13r4TkiE4KumL71LsHTPpL2euBYLFx6h9HP3piy1"
	mintWETH  = "7vfCXTUXx5WJV5JADk17DUJ4ksgau7utNKj4b963voxs"
	mintWBTC  = "3NZ9JMVBmGAqocybic2c7LQCJScmgsAZ6vQqTDzcqmJh"
	mintDRIFT = "DriFtupJYLTosbwoN8koMbEYSx54aFAVLddWsbksjwg7"
	mintPYUSD = "2b1kV6DkPAnxd5ixfnxCpjxmKwqjjaYmCZfHsFu24GXo"
	mintBONK  = "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263"
	mintPYTH  = "HZ1JovNiVvGrGNiiYvEozEVgZ58xaU3RKwX8eACQBCt3"
	mintWIF   = "EKpQGSJtjMFqKZ9KQanSqYXRcF8fBopzLHYxdM65zcjm"
	mintTRUMP = "6p6xgHyF7AeE6TZkSmFsko444wqoP15icUSqi2jfGiPN"
	mintPENGU = "2zMMhcVQEXDtdE6vsFS7S7D5oUodfJHE8vd1gnBouauv"
	// Pump.fun mints (Fartcoin etc.) — non-stable meme tokens that show up in
	// the wallet swap history. We add them so the asset string is the human
	// symbol rather than an opaque MINT: prefix.
	mintFARTCOIN = "9BB6NFEcjBCtnNLFko2FqVQBq8HHM13kCyYcdQbgpump"
)

var mintToSymbol = map[string]string{
	mintWSOL:     "SOL",
	mintUSDC:     "USDC",
	mintUSDT:     "USDT",
	mintJLP:      "JLP",
	mintJUP:      "JUP",
	mintJTO:      "JTO",
	mintJITOSOL:  "JitoSOL",
	mintMSOL:     "mSOL",
	mintBSOL:     "bSOL",
	mintWETH:     "WETH",
	mintWBTC:     "WBTC",
	mintDRIFT:    "DRIFT",
	mintPYUSD:    "PYUSD",
	mintBONK:     "BONK",
	mintPYTH:     "PYTH",
	mintWIF:      "WIF",
	mintTRUMP:    "TRUMP",
	mintPENGU:    "PENGU",
	mintFARTCOIN: "FARTCOIN",
}

// resolveMint returns the symbol for a mint address. For unknown mints we
// fall back to the first 8 characters of the mint so the asset string is
// stable and human-recognisable, while still being unique enough to avoid
// collisions across the small set of mints any single wallet typically holds.
// Crash-loudly behaviour does NOT apply here — we'd rather record the activity
// against an opaque asset string than drop the row.
func resolveMint(mint string) string {
	if sym, ok := mintToSymbol[mint]; ok {
		return sym
	}
	if len(mint) > 8 {
		return "MINT:" + mint[:8]
	}
	return "MINT:" + mint
}
