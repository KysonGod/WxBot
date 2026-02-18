package webfetch

import "context"

type Fetcher interface {
	Fetch(ctx context.Context, rawURL string) (string, error)
}

type NoopFetcher struct{}

func (NoopFetcher) Fetch(ctx context.Context, rawURL string) (string, error) {
	return "", ErrUnsupported
}
