package main

import (
	"bytes"
	"context"
	stderr "errors"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"os"
	"testing"

	"github.com/joho/godotenv"
	"github.com/maxnilz/feed/ai"
	"github.com/maxnilz/feed/errors"
	"github.com/mmcdole/gofeed"
)

func init() {
	// Load .env file for tests
	_ = godotenv.Load()
}

func TestErr(t *testing.T) {
	err := textproto.ProtocolError("short response: ")
	if !stderr.Is(err, textproto.ProtocolError("short response: ")) {
		t.Errorf("error not matched: %v", err)
	}
}

func TestParseFeed(t *testing.T) {
	url := "https://hnrss.org/newest"
	feedTxt, err := fetchFeed(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	feed, err := parseFeedSource(feedTxt)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(len(feed.Items))
}

func fetchFeed(ctx context.Context, url string) (string, error) {
	client := http.DefaultClient
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", errors.Newf(errors.Internal, err, "create get request to %v failed", url)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.Newf(errors.Internal, err, "request feeds to %v failed", url)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", errors.Newf(errors.Internal, nil, "invalid feed response: %v", resp.Status)
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", errors.Newf(errors.Internal, err, "read feed body from %v failed", url)
	}
	return string(bodyBytes), nil
}

func parseFeedSource(txt string) (*gofeed.Feed, error) {
	rd := bytes.NewReader([]byte(txt))
	fp := gofeed.NewParser()
	feed, err := fp.Parse(rd)
	if err != nil {
		return nil, errors.Newf(errors.Internal, err, "parse feed failed")
	}
	return feed, nil
}

func TestFetch(t *testing.T) {
	geminiAPIKey := os.Getenv("GEMINI_API_KEY")
	if geminiAPIKey == "" {
		t.Skip("GEMINI_API_KEY is not set, skipping TestFetch")
	}
	openaiAPIKey := os.Getenv("OPENAI_API_KEY")
	if openaiAPIKey == "" {
		t.Skip("OPENAI_API_KEY is not set, skipping TestFetch")
	}
	af := ai.FilterConfig{
		Type:         ai.FilterTypeOpenAI,
		GeminiAPIKey: geminiAPIKey,
		OpenAIAPIKey: openaiAPIKey,
	}
	filter, err := ai.NewFilter(context.Background(), af)
	if err != nil {
		t.Fatalf("NewFilter failed: %v", err)
	}
	defer filter.Close()

	storage, err := newSQLite(":memory:")
	if err != nil {
		t.Fatalf("newSQLite failed: %v", err)
	}
	defer storage.Close()

	source := Source{
		Name: "Hacker News",
		URL:  "https://hnrss.org/newest?count=100",
		SourceFilterConfig: ai.SourceFilterConfig{
			EmbeddedPromptFile:  "hn-prompt.md",
			EmbeddedFiltersFile: "hn-filters.txt",
			SimilarityThreshold: 0.8,
		},
	}
	subscriber := Subscriber{
		Name:  "test-user",
		Email: "a@example.com",
	}

	sf, err := NewSourceFetcher(subscriber, source, storage, filter, 0, VerboseLogger)
	if err != nil {
		t.Fatalf("NewSourceFetcher failed: %v", err)
	}

	// Set up session in context (mimics what Fetcher.Run does)
	ses, err := storage.NewSession(context.Background())
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	ses, err = ses.Begin()
	if err != nil {
		t.Fatalf("Begin failed: %v", err)
	}
	defer ses.Rollback()

	ctx := WithSession(context.Background(), ses)
	items, err := sf.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if err := ses.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	t.Logf("Fetched %d items", len(items))
	for _, item := range items {
		t.Logf("  - %s, %s, %s, %s", item.Title, item.Id, item.Link, item.SourceURL)
	}
}
