package main

import (
	"context"
	stderr "errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joho/godotenv"
	"github.com/maxnilz/feed/ai"
	"github.com/maxnilz/feed/errors"
	"github.com/maxnilz/feed/logging"
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

func TestParseFeedTxt(t *testing.T) {
	wd, _ := os.Getwd()
	fname := filepath.Join(wd, "testdata", "364.xml")
	b, err := ioutil.ReadFile(fname)
	if err != nil {
		t.Errorf("error reading file: %v", err)
	}
	feedTxt := string(b)
	feed, err := parseFeedSource(feedTxt)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(len(feed.Items))
}

func TestParseFeed(t *testing.T) {
	url := "https://36kr.com/feed"
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
	fp := gofeed.NewParser()
	feed, _, err := parseFeedWithSanitization(fp, strings.NewReader(txt))
	if err != nil {
		return nil, errors.Newf(errors.Internal, err, "parse feed failed")
	}
	return feed, nil
}

func TestParseFeedSourceAllowsInvalidControlChars(t *testing.T) {
	raw := "<?xml version=\"1.0\" encoding=\"UTF-8\"?><rss version=\"2.0\"><channel><title>demo</title><item><title>Hello\x1eWorld</title><link>https://example.com/a</link><guid>a</guid></item></channel></rss>"
	feed, err := parseFeedSource(raw)
	if err != nil {
		t.Fatalf("parseFeedSource failed: %v", err)
	}
	if len(feed.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(feed.Items))
	}
	if feed.Items[0].Title != "HelloWorld" {
		t.Fatalf("unexpected sanitized title: %q", feed.Items[0].Title)
	}
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
		Type:         ai.FilterTypeGemini,
		GeminiAPIKey: geminiAPIKey,
		OpenAIAPIKey: openaiAPIKey,
	}
	filter, err := ai.NewFilter(context.Background(), logging.DefaultLogger, af)
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

	sf, err := NewSourceFetcher(subscriber, source, storage, filter, 0, logging.VerboseLogger)
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
