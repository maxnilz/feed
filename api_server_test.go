package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maxnilz/feed/logging"
)

type fakeSession struct{}

func (fakeSession) Begin() (Session, error)                 { return fakeSession{}, nil }
func (fakeSession) Rollback() error                         { return nil }
func (fakeSession) Commit() error                           { return nil }
func (fakeSession) Exec(string, ...any) (sql.Result, error) { return nil, nil }
func (fakeSession) Query(string, ...any) (*sql.Rows, error) { return nil, nil }
func (fakeSession) QueryRow(string, ...any) *sql.Row        { return nil }

type fakeStorage struct {
	items map[string][]*Item
}

func (s *fakeStorage) NewSession(context.Context) (Session, error) { return fakeSession{}, nil }
func (s *fakeStorage) SaveItems(Session, ...*Item) error           { return nil }
func (s *fakeStorage) AckItems(Session, time.Time, ...*Item) error { return nil }
func (s *fakeStorage) GetCursor(Session, string, string) (time.Time, error) {
	return time.Time{}, nil
}
func (s *fakeStorage) UpdateCursor(Session, string, string, time.Time) error { return nil }
func (s *fakeStorage) GetUnackedItems(_ Session, email string) ([]*Item, error) {
	return s.items[email], nil
}
func (s *fakeStorage) ArchiveItems(Session, time.Time) (int64, error) { return 0, nil }
func (s *fakeStorage) Close() error                                   { return nil }

func newTestServer(items map[string][]*Item, authToken string, defaultEmail Email) *apiServer {
	return &apiServer{
		storage:      &fakeStorage{items: items},
		logger:       logging.DiscardLogger,
		sourceOrder:  map[Email][]string{Email("foo@example.com"): {"Source A", "Source B"}},
		defaultEmail: defaultEmail,
		authToken:    authToken,
		htmlTmpl:     mustPendingNotificationsTemplate(),
	}
}

func TestAPIPendingNotificationsJSONParam(t *testing.T) {
	now := time.Now().UTC()
	s := newTestServer(map[string][]*Item{
		"foo@example.com": {
			{Id: "2", Email: "foo@example.com", SourceName: "Source B", Title: "B", Link: "https://example.com/b", PublishedAt: now.Format(time.RFC3339), Score: 0.6},
			{Id: "1", Email: "foo@example.com", SourceName: "Source A", Title: "A", Link: "https://example.com/a", PublishedAt: now.Format(time.RFC3339), Score: 0.9},
		},
	}, "", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscribers/foo%40example.com/notifications/pending?json", nil)
	rec := httptest.NewRecorder()
	s.handlePendingWithEmail(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}

	var resp pendingNotificationsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if resp.Email != "foo@example.com" {
		t.Fatalf("unexpected email: %s", resp.Email)
	}
	if resp.TotalItems != 2 {
		t.Fatalf("unexpected total items: %d", resp.TotalItems)
	}
	if len(resp.Sources) != 2 {
		t.Fatalf("unexpected source count: %d", len(resp.Sources))
	}
	if resp.Sources[0].Name != "Source A" || resp.Sources[1].Name != "Source B" {
		t.Fatalf("unexpected source order: %s then %s", resp.Sources[0].Name, resp.Sources[1].Name)
	}
}

func TestAPIPendingNotificationsHTMLDefault(t *testing.T) {
	now := time.Now().UTC()
	s := newTestServer(map[string][]*Item{
		"foo@example.com": {
			{Id: "1", Email: "foo@example.com", SourceName: "Source A", Title: "Hello World", Link: "https://example.com/1", PublishedAt: now.Format(time.RFC3339), Score: 0.9},
		},
	}, "", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscribers/foo%40example.com/notifications/pending", nil)
	rec := httptest.NewRecorder()
	s.handlePendingWithEmail(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("expected text/html content-type, got %s", ct)
	}
	if !strings.Contains(rec.Body.String(), "Hello World") {
		t.Fatal("expected item title in HTML output")
	}
}

func TestAPIPendingNotificationsNoEmailDefault(t *testing.T) {
	now := time.Now().UTC()
	s := newTestServer(map[string][]*Item{
		"foo@example.com": {
			{Id: "1", Email: "foo@example.com", SourceName: "Source A", Title: "Default Route", Link: "https://example.com/1", PublishedAt: now.Format(time.RFC3339), Score: 0.8},
		},
	}, "", Email("foo@example.com"))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/pending?json", nil)
	rec := httptest.NewRecorder()
	s.handlePendingNoEmail(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp pendingNotificationsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if resp.Email != "foo@example.com" {
		t.Fatalf("expected foo@example.com, got %s", resp.Email)
	}
	if resp.TotalItems != 1 {
		t.Fatalf("expected 1 item, got %d", resp.TotalItems)
	}
}

func TestAPIPendingNotificationsAuth(t *testing.T) {
	s := newTestServer(map[string][]*Item{}, "secret", Email("foo@example.com"))

	unauthorizedReq := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/pending", nil)
	unauthorizedRec := httptest.NewRecorder()
	s.handlePendingNoEmail(unauthorizedRec, unauthorizedReq)
	if unauthorizedRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", unauthorizedRec.Code)
	}

	authorizedReq := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/pending", nil)
	authorizedReq.Header.Set("Authorization", "Bearer secret")
	authorizedRec := httptest.NewRecorder()
	s.handlePendingNoEmail(authorizedRec, authorizedReq)
	if authorizedRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", authorizedRec.Code)
	}
}

func TestCompactTimeString(t *testing.T) {
	t.Run("datetime keeps date", func(t *testing.T) {
		got := compactTimeString("Tue, 14 Apr 2026 23:43:45 +0000")
		if got != "04-14 23:43" {
			t.Fatalf("expected compact datetime with date, got %q", got)
		}
	})

	t.Run("time only keeps timezone", func(t *testing.T) {
		got := compactTimeString("00:41 +0000")
		if got != "00:41" {
			t.Fatalf("expected compact time-only value without timezone, got %q", got)
		}
	})
}

func TestManualRenderPendingNotificationsHTMLTemplate(t *testing.T) {
	resp := pendingNotificationsResponse{
		Email:      "maxnilz.io@gmail.com",
		TotalItems: 12,
		Sources: []pendingSourceResponse{
			{
				Name: "Semantic HN",
				Items: []pendingItemResponse{
					{
						ID:                 "47773288",
						Title:              "Authorization for LLM Tool Schemas: Formal Model with Noninterference Guarantees [pdf]",
						Link:               "https://raw.githubusercontent.com/AndyGauge/andygauge.github.io/master/publication/noninterference%20guarantee.pdf",
						GUID:               "https://news.ycombinator.com/item?id=47773288",
						Score:              0.95,
						PublishedAt:        "00:41 +0000",
						DisplayPublishedAt: "00:41 +0000",
					},
					{
						ID:                 "47773110",
						Title:              "1B payments per day ft TigerBeetle, Postgres",
						Link:               "https://backend.how/posts/1b-payments-per-day/",
						GUID:               "https://news.ycombinator.com/item?id=47773110",
						Score:              0.95,
						PublishedAt:        "00:10 +0000",
						DisplayPublishedAt: "00:10 +0000",
					},
					{
						ID:                 "47772916",
						Title:              "Deep Dive into Efficient LLM Inference with Nano-vLLM",
						Link:               "https://cefboud.com/posts/inside-llm-inference-engine-nano-vllm-explanation/",
						GUID:               "https://news.ycombinator.com/item?id=47772916",
						Score:              0.95,
						PublishedAt:        "Tue, 14 Apr 2026 23:43:45 +0000",
						DisplayPublishedAt: "Tue, 14 Apr 2026 23:43:45 +0000",
					},
					{
						ID:                 "47771697",
						Title:              "Are ClickHouse JOINs Slow? A 2026 PR-by-PR Analysis",
						Link:               "https://dataanalyticsguide.substack.com/p/clickhouse-join-performance-2026",
						GUID:               "https://news.ycombinator.com/item?id=47771697",
						Score:              0.95,
						PublishedAt:        "Tue, 14 Apr 2026 21:21:49 +0000",
						DisplayPublishedAt: "Tue, 14 Apr 2026 21:21:49 +0000",
					},
					{
						ID:                 "47772834",
						Title:              "Pointer-Stable Dynamic Arrays",
						Link:               "https://vectrx.substack.com/p/pointer-stable-dynamic-arrays",
						GUID:               "https://news.ycombinator.com/item?id=47772834",
						Score:              0.90,
						PublishedAt:        "Tue, 14 Apr 2026 23:32:06 +0000",
						DisplayPublishedAt: "Tue, 14 Apr 2026 23:32:06 +0000",
					},
					{
						ID:                 "47773047",
						Title:              "Operating Systems: Three Easy Pieces",
						Link:               "https://pages.cs.wisc.edu/~remzi/OSTEP/",
						GUID:               "https://news.ycombinator.com/item?id=47773047",
						Score:              0.85,
						PublishedAt:        "00:02 +0000",
						DisplayPublishedAt: "00:02 +0000",
					},
					{
						ID:                 "47772266",
						Title:              "GitHub webhook secrets leaked in headers",
						Link:               "https://gist.github.com/ltrgoddard/7abfc8e4123e403505dfbe767a2487ab",
						GUID:               "https://news.ycombinator.com/item?id=47772266",
						Score:              0.85,
						PublishedAt:        "Tue, 14 Apr 2026 22:25:32 +0000",
						DisplayPublishedAt: "Tue, 14 Apr 2026 22:25:32 +0000",
					},
					{
						ID:                 "47771990",
						Title:              "One Layer, +12%: What 667 Configs Reveal About Small LLM Anatomy",
						Link:               "https://austinsnerdythings.com/2026/04/14/rys-layer-duplication-qwen3-4b/",
						GUID:               "https://news.ycombinator.com/item?id=47771990",
						Score:              0.85,
						PublishedAt:        "Tue, 14 Apr 2026 21:54:05 +0000",
						DisplayPublishedAt: "Tue, 14 Apr 2026 21:54:05 +0000",
					},
					{
						ID:                 "47771735",
						Title:              "How to diagnose RAG failures from traces",
						Link:               "https://www.siquick.com/blog/diagnose-rag-failures-from-traces",
						GUID:               "https://news.ycombinator.com/item?id=47771735",
						Score:              0.85,
						PublishedAt:        "Tue, 14 Apr 2026 21:27:02 +0000",
						DisplayPublishedAt: "Tue, 14 Apr 2026 21:27:02 +0000",
					},
					{
						ID:                 "47773067",
						Title:              "S.A.F.E.: RFC-style intent checks for privileged AI automation",
						Link:               "https://zenodo.org/records/19161806",
						GUID:               "https://news.ycombinator.com/item?id=47773067",
						Score:              0.80,
						PublishedAt:        "00:04 +0000",
						DisplayPublishedAt: "00:04 +0000",
					},
					{
						ID:                 "47772806",
						Title:              "Show HN: Spectre: A systems design-by-contract language, self hosted compiler",
						Link:               "https://spectrelang.org",
						GUID:               "https://news.ycombinator.com/item?id=47772806",
						Score:              0.80,
						PublishedAt:        "Tue, 14 Apr 2026 23:29:37 +0000",
						DisplayPublishedAt: "Tue, 14 Apr 2026 23:29:37 +0000",
					},
				},
			},
		},
	}

	tmpl := mustPendingNotificationsTemplate()

	outputPath := filepath.Join("tmp", "pending-notifications-preview.html")
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		t.Fatalf("create output dir failed: %v", err)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		t.Fatalf("create output file failed: %v", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, resp); err != nil {
		t.Fatalf("render template failed: %v", err)
	}

	t.Logf("generated preview: %s", outputPath)
}
