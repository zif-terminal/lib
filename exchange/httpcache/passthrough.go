package httpcache

import "context"

// passthrough is the no-op transport: it always delegates to live and never
// touches the cache. Used when caching is disabled (the default).
type passthrough struct{}

func (passthrough) Get(ctx context.Context, _, _, _ string, live Fetcher) ([]byte, error) {
	return live(ctx)
}

func (passthrough) Post(ctx context.Context, _, _, _ string, _ []byte, live Fetcher) ([]byte, error) {
	return live(ctx)
}

func (passthrough) IsEnabled() bool { return false }

func (passthrough) Mode() Mode { return ModeDisabled }
