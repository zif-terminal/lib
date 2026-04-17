package drift

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"
)

// HTTPStatusError is returned when the Drift API returns a non-200 status code.
// It carries the status code so callers can distinguish e.g. 400 from 500.
type HTTPStatusError struct {
	StatusCode int
	Status     string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("API returned status %d: %s", e.StatusCode, e.Status)
}

// pageResult holds items from a single page plus the next page token
type pageResult[T any] struct {
	items    []T
	nextPage string
}

// pageFetcher is a function that fetches a single page of data
type pageFetcher[T any] func(ctx context.Context, url string) (pageResult[T], error)

// timestampGetter extracts the timestamp from an item for filtering
type timestampGetter[T any] func(item T) time.Time

// fetchAllPages fetches all pages from a paginated endpoint
func fetchAllPages[T any](ctx context.Context, baseURL string, fetcher pageFetcher[T]) ([]T, error) {
	var all []T
	page := ""

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		url := baseURL
		if page != "" {
			url = fmt.Sprintf("%s?page=%s", baseURL, page)
		}

		result, err := fetcher(ctx, url)
		if err != nil {
			return nil, err
		}

		all = append(all, result.items...)

		if result.nextPage == "" {
			break
		}
		page = result.nextPage
	}

	return all, nil
}

// fetchHistorical fetches data from historical monthly archives
// It iterates through months from `since` to `recentWindowStart` and filters
// out records that would overlap with the recent API window.
func fetchHistorical[T any](
	ctx context.Context,
	baseURL string,
	accountID string,
	endpoint string,
	since time.Time,
	recentWindowStart time.Time,
	fetcher pageFetcher[T],
	getTimestamp timestampGetter[T],
) ([]T, error) {
	var all []T

	current := time.Date(since.Year(), since.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(recentWindowStart.Year(), recentWindowStart.Month(), 1, 0, 0, 0, 0, time.UTC)

	// Track whether we've received any data from historical months.
	// Sequential 400s before any data means the account didn't exist yet.
	// Once data arrives, a 400 is a real data gap.
	hasReceivedData := false

	for !current.After(end) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Build URL for this month: /user/{accountID}/{endpoint}/{year}/{month}
		monthURL := fmt.Sprintf("%s/user/%s/%s/%d/%d", baseURL, accountID, endpoint, current.Year(), int(current.Month()))

		monthItems, err := fetchAllPages(ctx, monthURL, fetcher)
		if err != nil {
			var httpErr *HTTPStatusError
			if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusBadRequest && !hasReceivedData {
				// Account likely didn't exist yet — skip this month
				log.Printf("drift/%s: skipping %d/%d (400, account likely didn't exist yet) | account=%s",
					endpoint, current.Year(), int(current.Month()), accountID)
				current = current.AddDate(0, 1, 0)
				continue
			}
			// Real error — either non-400, or 400 after we've already gotten data
			return nil, fmt.Errorf("drift/%s: historical fetch failed for %d/%d (account=%s): %w",
				endpoint, current.Year(), int(current.Month()), accountID, err)
		}

		// For the month that overlaps with the recent window, filter out records
		// that would be covered by the recent API (timestamp >= recentWindowStart)
		if current.Year() == recentWindowStart.Year() && current.Month() == recentWindowStart.Month() {
			filtered := make([]T, 0)
			for _, item := range monthItems {
				if getTimestamp(item).Before(recentWindowStart) {
					filtered = append(filtered, item)
				}
			}
			monthItems = filtered
		}

		if len(monthItems) > 0 {
			hasReceivedData = true
		}

		all = append(all, monthItems...)
		current = current.AddDate(0, 1, 0)
	}

	return all, nil
}

// fetchWithHistory fetches data from both historical and recent APIs
func fetchWithHistory[T any](
	ctx context.Context,
	baseURL string,
	accountID string,
	endpoint string,
	since time.Time,
	fetcher pageFetcher[T],
	getTimestamp timestampGetter[T],
) ([]T, error) {
	thirtyOneDaysAgo := time.Now().AddDate(0, 0, -31)

	// Determine effective since date (use Drift launch date if zero)
	effectiveSince := since
	if since.IsZero() {
		effectiveSince = time.Date(2021, 11, 1, 0, 0, 0, 0, time.UTC)
	}

	needsHistorical := effectiveSince.Before(thirtyOneDaysAgo)

	var all []T

	// Fetch historical data if needed
	if needsHistorical {
		historical, err := fetchHistorical(
			ctx,
			baseURL,
			accountID,
			endpoint,
			effectiveSince,
			thirtyOneDaysAgo,
			fetcher,
			getTimestamp,
		)
		if err != nil {
			return nil, err
		}
		all = append(all, historical...)
	}

	// Fetch recent data (last 31 days)
	recentURL := fmt.Sprintf("%s/user/%s/%s", baseURL, accountID, endpoint)
	recent, err := fetchAllPages(ctx, recentURL, fetcher)
	if err != nil {
		return nil, err
	}
	all = append(all, recent...)

	preFilterCount := len(all)

	// Filter by since timestamp
	if !since.IsZero() {
		filtered := make([]T, 0)
		for _, item := range all {
			if !getTimestamp(item).Before(since) {
				filtered = append(filtered, item)
			}
		}
		all = filtered
	}

	// Log diagnostic info when API returns 0 records or all records were filtered
	if preFilterCount == 0 {
		log.Printf("drift/%s: API returned 0 records | account=%s | url=%s", endpoint, accountID, recentURL)
	} else if len(all) == 0 && preFilterCount > 0 {
		log.Printf("drift/%s: all %d records filtered by since=%v | account=%s", endpoint, preFilterCount, since, accountID)
	}

	return all, nil
}
