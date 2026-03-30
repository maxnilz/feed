package main

import (
	"context"
	stderr "errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/maxnilz/feed/ai"
	"github.com/maxnilz/feed/errors"
	"github.com/maxnilz/feed/logging"
	"github.com/mmcdole/gofeed"
)

// SourceFetcher handles fetching items from a single RSS source
type SourceFetcher struct {
	// Subscriber info
	subscriber Subscriber

	// Source config
	source Source

	// AI filter (shared singleton)
	filter ai.Filter

	// Storage for persistence
	storage Storage

	// HTTP client for fetching feeds
	client *http.Client

	// Feed parser
	fp *gofeed.Parser

	// Logger
	logger logging.Logger
}

func NewSourceFetcher(subscriber Subscriber, source Source, storage Storage, filter ai.Filter, fetchTimeout time.Duration, logger logging.Logger) (*SourceFetcher, error) {
	if subscriber.Name == "" {
		return nil, errors.Newf(errors.InvalidArgument, nil, "subscriber name is required")
	}
	if subscriber.Email == "" {
		return nil, errors.Newf(errors.InvalidArgument, nil, "subscriber email is required")
	}

	// Validate source URLs
	for _, u := range source.AllURLs() {
		if _, err := url.Parse(u); err != nil {
			return nil, errors.Newf(errors.InvalidArgument, nil, "invalid source url in %s", subscriber.Name)
		}
	}

	if fetchTimeout == 0 {
		fetchTimeout = 30 * time.Second
	}

	return &SourceFetcher{
		subscriber: subscriber,
		source:     source,
		filter:     filter,
		storage:    storage,
		client:     &http.Client{Timeout: fetchTimeout},
		fp:         gofeed.NewParser(),
		logger:     logger,
	}, nil
}

// Fetch collects items from all endpoints in this source.
// Session must be set in context via WithSession.
func (sf *SourceFetcher) Fetch(ctx context.Context) ([]*Item, error) {
	var items []*Item

	for _, endpoint := range sf.source.AllURLs() {
		sf.logger.Info("Fetching", "source", sf.source.Name, "endpoint", endpoint)
		fetched, err := sf.fetchByURL(ctx, endpoint)
		if err != nil {
			// Ignore error
			sf.logger.Error(err, "fetch failed", "source", sf.source.Name, "endpoint", endpoint)
			continue
		}
		if len(fetched) == 0 {
			continue
		}
		items = append(items, fetched...)
	}
	return items, nil
}

func (sf *SourceFetcher) fetchByURL(ctx context.Context, endpoint string) ([]*Item, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.Newf(errors.Internal, err, "create get request to %v failed", endpoint)
	}
	resp, err := sf.client.Do(req)
	if err != nil {
		if stderr.Is(err, context.DeadlineExceeded) || stderr.Is(err, context.Canceled) {
			sf.logger.Info("Fetch timeout, skipping", "source", sf.source.Name, "endpoint", endpoint)
			return nil, nil
		}
		return nil, errors.Newf(errors.Internal, err, "request feeds to %v failed", endpoint)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.Newf(errors.Internal, nil, "invalid feed response: %v", resp.Status)
	}
	return sf.parseAndFilter(ctx, endpoint, resp.Body)
}

func (sf *SourceFetcher) parseAndFilter(ctx context.Context, endpoint string, r io.Reader) ([]*Item, error) {
	ses, err := sf.storage.NewSession(ctx)
	if err != nil {
		return nil, errors.Newf(errors.Internal, err, "create session from %v failed", endpoint)
	}

	feed, err := sf.fp.Parse(r)
	if err != nil {
		return nil, errors.Newf(errors.Internal, err, "parse feeds at %v failed", endpoint)
	}
	if len(feed.Items) == 0 {
		sf.logger.Info("no new items fetched", "source", sf.source.Name, "endpoint", endpoint)
		return nil, nil
	}
	sort.Sort(feed)

	cursor, err := sf.storage.GetCursor(ses, sf.subscriber.Email, endpoint)
	if err != nil {
		return nil, err
	}

	// 1. Filter candidates by cursor
	var candidates []*gofeed.Item
	var filterItems []ai.FilterItem

	maxPublished := cursor

	for _, gofeedItem := range feed.Items {
		tm := gofeedItem.PublishedParsed
		if gofeedItem.UpdatedParsed != nil {
			tm = gofeedItem.UpdatedParsed
		}
		if tm == nil || !tm.After(cursor) {
			continue
		}

		candidates = append(candidates, gofeedItem)
		filterItems = append(filterItems, ai.FilterItem{
			Title:       gofeedItem.Title,
			Description: gofeedItem.Description,
			Content:     gofeedItem.Content,
			Link:        gofeedItem.Link,
		})

		// Update maxPublished considering ALL new items, even filtered ones
		if tm.After(maxPublished) {
			maxPublished = *tm
		}
	}

	if len(candidates) == 0 {
		sf.logger.Info("no new candidates", "source", sf.source.Name, "endpoint", endpoint)
		return nil, nil
	}

	// 2. Batch Semantic Filtering
	scores, err := sf.filter.Evaluate(ctx, filterItems, sf.source.SourceFilterConfig)
	if err != nil {
		return nil, errors.Newf(errors.Internal, err, "semantic evaluation failed")
	}

	var items []*Item
	for i, gofeedItem := range candidates {
		if scores[i] < sf.source.SimilarityThreshold {
			continue
		}

		authors := make([]string, 0, len(gofeedItem.Authors))
		for _, a := range gofeedItem.Authors {
			authors = append(authors, a.Name)
		}
		id := gofeedItem.GUID
		if id == "" {
			id = gofeedItem.Link
		}
		item := &Item{
			Id:          id,
			Email:       Email(sf.subscriber.Email),
			SourceURL:   endpoint,
			SourceName:  sf.source.Name,
			Title:       gofeedItem.Title,
			Description: gofeedItem.Description,
			Content:     gofeedItem.Content,
			Link:        gofeedItem.Link,
			UpdatedAt:   gofeedItem.Updated,
			PublishedAt: gofeedItem.Published,
			Author:      strings.Join(authors, ", "),
			Score:       scores[i],
			FetchAt:     time.Now(),
		}
		items = append(items, item)
	}

	if maxPublished.After(cursor) {
		if err := sf.storage.UpdateCursor(ses, sf.subscriber.Email, endpoint, maxPublished); err != nil {
			return nil, err
		}
	}

	sf.logger.Info("fetched items",
		"source", sf.source.Name,
		"endpoint", endpoint,
		"fetched", len(candidates),
		"filtered", len(items),
	)
	for i, item := range items {
		msg := fmt.Sprintf(" -> %02d", i+1)
		sf.logger.Info(msg, "id", item.Id, "title", item.Title, "score", item.Score)
	}

	return items, nil
}

// Fetcher coordinates fetching from multiple sources for a subscriber
type Fetcher struct {
	name    string
	storage Storage
	logger  logging.Logger

	sourceFetchers []*SourceFetcher
}

func NewFetcher(subscriber Subscriber, storage Storage, filter ai.Filter, fetchTimeout time.Duration, logger logging.Logger) (*Fetcher, error) {
	if subscriber.Name == "" {
		return nil, errors.Newf(errors.InvalidArgument, nil, "subscriber name is required")
	}
	if subscriber.Email == "" {
		return nil, errors.Newf(errors.InvalidArgument, nil, "subscriber email is required")
	}

	var sourceFetchers []*SourceFetcher
	for _, source := range subscriber.Sources {
		sf, err := NewSourceFetcher(subscriber, source, storage, filter, fetchTimeout, logger)
		if err != nil {
			return nil, err
		}
		sourceFetchers = append(sourceFetchers, sf)
	}

	return &Fetcher{
		name:           subscriber.Name,
		storage:        storage,
		logger:         logger,
		sourceFetchers: sourceFetchers,
	}, nil
}

func (f *Fetcher) Name() string {
	return fmt.Sprintf("fetcher-%s", f.name)
}

func (f *Fetcher) SourceFetchers() []*SourceFetcher {
	return f.sourceFetchers
}

func (f *Fetcher) Run(ctx context.Context) error {
	ses, err := f.storage.NewSession(ctx)
	if err != nil {
		return err
	}
	ses, err = ses.Begin()
	if err != nil {
		return err
	}
	defer ses.Rollback()

	// Store session in context for SourceFetchers to use
	ctx = WithSession(ctx, ses)

	var items Items
	for _, sf := range f.sourceFetchers {
		out, err := sf.Fetch(ctx)
		if err != nil {
			return err
		}
		items.Append(out...)
	}

	if err = f.storage.SaveItems(ses, items.List()...); err != nil {
		return err
	}
	if err = ses.Commit(); err != nil {
		return err
	}

	return nil
}
