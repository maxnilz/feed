package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"strings"
	"sync"

	"github.com/maxnilz/feed/logging"
)

const defaultCachedFilterEntries = 256

var cacheLogger = log.Default()

type cachedFilter struct {
	logger logging.Logger

	inner      Filter
	maxEntries int

	mu    sync.Mutex
	cache map[string][]float32
	order []string
}

func newCachedFilter(inner Filter, logger logging.Logger, maxEntries int) *cachedFilter {
	if maxEntries <= 0 {
		maxEntries = defaultCachedFilterEntries
	}
	return &cachedFilter{
		logger:     logger,
		inner:      inner,
		maxEntries: maxEntries,
		cache:      make(map[string][]float32, maxEntries),
		order:      make([]string, 0, maxEntries),
	}
}

func (f *cachedFilter) Evaluate(ctx context.Context, items []FilterItem, cfg SourceFilterConfig) ([]float32, error) {
	key := cacheKeyForItems(items)

	f.mu.Lock()
	if scores, ok := f.cache[key]; ok {
		out := cloneScores(scores)
		f.mu.Unlock()
		k := key
		if len(key) > 8 {
			k = key[:8]
		}
		f.logger.Info("semantic filter cache hit", "key", k, "items", len(items))
		return out, nil
	}
	f.mu.Unlock()

	scores, err := f.inner.Evaluate(ctx, items, cfg)
	if err != nil {
		return nil, err
	}

	f.mu.Lock()
	if existing, ok := f.cache[key]; ok {
		out := cloneScores(existing)
		f.mu.Unlock()
		return out, nil
	}
	f.evictOldestIfNeeded()
	f.cache[key] = cloneScores(scores)
	f.order = append(f.order, key)
	out := cloneScores(f.cache[key])
	f.mu.Unlock()

	return out, nil
}

func (f *cachedFilter) Close() error {
	return f.inner.Close()
}

func (f *cachedFilter) evictOldestIfNeeded() {
	if len(f.cache) < f.maxEntries {
		return
	}
	oldest := f.order[0]
	f.order = f.order[1:]
	delete(f.cache, oldest)
}

func cacheKeyForItems(items []FilterItem) string {
	h := sha256.New()
	for _, item := range items {
		writeNormalized(h, item.Title)
		writeNormalized(h, item.Description)
		writeNormalized(h, item.Content)
		writeNormalized(h, item.Link)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeNormalized(h interface{ Write([]byte) (int, error) }, s string) {
	normalized := strings.ToLower(strings.Join(strings.Fields(s), " "))
	_, _ = h.Write([]byte(normalized))
	_, _ = h.Write([]byte{0})
}

func cloneScores(scores []float32) []float32 {
	out := make([]float32, len(scores))
	copy(out, scores)
	return out
}
