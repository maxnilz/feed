package main

import (
	"testing"
	"time"
)

func TestBuildNotificationSourcesHonorsConfiguredOrder(t *testing.T) {
	now := time.Date(2026, 4, 15, 10, 30, 0, 0, time.UTC)
	items := Items{}
	items.Append(
		&Item{Id: "b1", Email: "user@example.com", SourceName: "Source B", Title: "B", PublishedAt: now.Format(time.RFC3339)},
		&Item{Id: "a1", Email: "user@example.com", SourceName: "Source A", Title: "A", PublishedAt: now.Format(time.RFC3339)},
	)

	sources, flattened := buildNotificationSources(
		Email("user@example.com"),
		items,
		map[Email][]string{Email("user@example.com"): []string{"Source A", "Source B"}},
		now,
	)

	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}
	if sources[0].Name != "Source A" || sources[1].Name != "Source B" {
		t.Fatalf("unexpected source order: %q then %q", sources[0].Name, sources[1].Name)
	}
	if len(flattened) != 2 {
		t.Fatalf("expected 2 flattened items, got %d", len(flattened))
	}
	if got := sources[0].Items[0].DisplayPublishedAt; got != "10:30 UTC" {
		t.Fatalf("unexpected formatted time: %q", got)
	}
}

