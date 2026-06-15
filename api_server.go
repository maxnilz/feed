package main

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/maxnilz/feed/logging"
)

const (
	pendingPathPrefix  = "/api/v1/subscribers/"
	pendingPathDefault = "/api/v1/notifications/pending"
)

// APIServer is the interface for the optional REST API lifecycle.
type APIServer interface {
	Start(ctx context.Context)
	Stop()
}

// NewAPIServer returns a real HTTP API server when cfg.API.Enabled is true,
// or a no-op implementation otherwise, so callers need no conditional logic.
func NewAPIServer(cfg Config, storage Storage, logger logging.Logger) APIServer {
	if !cfg.API.Enabled {
		return noopAPIServer{}
	}
	return newAPIServer(cfg, storage, logger)
}

// noopAPIServer satisfies APIServer when the API is disabled.
type noopAPIServer struct{}

func (noopAPIServer) Start(context.Context) {}
func (noopAPIServer) Stop()                 {}

const pendingNotificationsHTMLTemplate = `<!DOCTYPE html>
<html>
  <head>
    <meta charset="UTF-8" />
    <title>Pending Notifications – {{.Email}}</title>
    <style>
      body {
        font-family: sans-serif;
        max-width: 900px;
        margin: 2rem auto;
        padding: 0 1rem;
      }
      h1 {
        border-bottom: 1px solid #ccc;
        padding-bottom: 0.4rem;
      }
      ol {
        padding-left: 1.5rem;
      }
      li {
        margin: 0.4rem 0;
		line-height: 1.6;
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto auto;
		align-items: baseline;
		column-gap: 0.35rem;
	  }
	  .item-title {
		flex: 1 1 auto;
		min-width: 0;
		white-space: normal;
		overflow-wrap: anywhere;
	  }
	  .item-guid {
		white-space: nowrap;
      }
      .meta {
		display: inline-block;
		white-space: nowrap;
        color: #666;
	  }
	  .item-score {
		display: inline-block;
		width: 6ch;
		white-space: nowrap;
	  }
	  .item-time {
		display: inline-block;
		width: 11ch;
		white-space: nowrap;
      }
    </style>
  </head>
  <body>
    <p class="meta">
      Showing pending items for <strong>{{.Email}}</strong> ({{.TotalItems}} total)
    </p>
    {{- range .Sources}}
    <h1>New posts from {{.Name}}</h1>
    <ol>
      {{- range .Items}}
      <li>
		<a class="item-title" href="{{.Link}}">{{.Title}}</a>
		{{- if .GUID}}<a class="item-guid" href="{{.GUID}}">[guid]</a>{{end}}
		<span class="meta">
		  <span class="item-score">[{{printf "%.2f" .Score}}]</span>
		  <span class="item-time">{{compactTime .DisplayPublishedAt}}</span>
		  {{- if .DisplayUpdatedAt}} <span class="item-time">{{compactTime .DisplayUpdatedAt}}</span>{{end}}
        </span>
      </li>
      {{- end}}
    </ol>
    {{- end}}
  </body>
</html>`

func mustPendingNotificationsTemplate() *template.Template {
	return template.Must(
		template.New("pending").
			Funcs(template.FuncMap{"compactTime": compactTimeString}).
			Parse(pendingNotificationsHTMLTemplate),
	)
}

type apiServer struct {
	storage      Storage
	logger       logging.Logger
	sourceOrder  map[Email][]string
	defaultEmail Email
	authToken    string
	httpServer   *http.Server
	htmlTmpl     *template.Template
	waiter       sync.WaitGroup
}

type pendingNotificationsResponse struct {
	Email      string                  `json:"email"`
	TotalItems int                     `json:"totalItems"`
	Sources    []pendingSourceResponse `json:"sources"`
}

type pendingSourceResponse struct {
	Name  string                `json:"name"`
	Items []pendingItemResponse `json:"items"`
}

type pendingItemResponse struct {
	ID                 string  `json:"id"`
	Title              string  `json:"title"`
	Link               string  `json:"link"`
	GUID               string  `json:"guid,omitempty"`
	Score              float32 `json:"score"`
	PublishedAt        string  `json:"publishedAt"`
	UpdatedAt          string  `json:"updatedAt,omitempty"`
	DisplayPublishedAt string  `json:"displayPublishedAt"`
	DisplayUpdatedAt   string  `json:"displayUpdatedAt,omitempty"`
}

func newAPIServer(cfg Config, storage Storage, logger logging.Logger) *apiServer {
	listenAddr := cfg.API.ListenAddr
	if listenAddr == "" {
		listenAddr = ":8080"
	}

	var defaultEmail Email
	if len(cfg.Subscribers) > 0 {
		defaultEmail = Email(cfg.Subscribers[0].Email)
	}

	s := &apiServer{
		storage:      storage,
		logger:       logger,
		sourceOrder:  buildSourceOrder(cfg.Subscribers),
		defaultEmail: defaultEmail,
		authToken:    cfg.API.AuthToken,
		htmlTmpl:     mustPendingNotificationsTemplate(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc(pendingPathDefault, s.handlePendingNoEmail)
	mux.HandleFunc(pendingPathPrefix, s.handlePendingWithEmail)
	mux.HandleFunc("/healthz", s.handleHealthz)

	s.httpServer = &http.Server{
		Addr:    listenAddr,
		Handler: mux,
	}

	return s
}

// Start launches the HTTP server and a watcher goroutine that performs a
// graceful shutdown when ctx is cancelled, mirroring the Scheduler pattern.
func (s *apiServer) Start(ctx context.Context) {
	s.waiter.Add(1)
	go func() {
		defer s.waiter.Done()
		s.logger.Info("starting api server", "addr", s.httpServer.Addr)
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error(err, "api server stopped unexpectedly")
		}
	}()

	// Watch for context cancellation and trigger graceful shutdown.
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			s.logger.Error(err, "api server shutdown failed")
		}
	}()
}

// Stop waits for the HTTP server goroutine to exit.
func (s *apiServer) Stop() {
	s.waiter.Wait()
}

func (s *apiServer) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// handlePendingNoEmail serves /api/v1/notifications/pending using the first configured subscriber.
func (s *apiServer) handlePendingNoEmail(w http.ResponseWriter, r *http.Request) {
	if s.defaultEmail == "" {
		http.Error(w, "no subscribers configured", http.StatusNotFound)
		return
	}
	s.serveNotifications(w, r, s.defaultEmail)
}

// handlePendingWithEmail serves /api/v1/subscribers/{email}/notifications/pending.
func (s *apiServer) handlePendingWithEmail(w http.ResponseWriter, r *http.Request) {
	emailStr, ok := extractEmailFromPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.serveNotifications(w, r, Email(emailStr))
}

// serveNotifications is the shared logic: queries pending items and renders HTML (default) or JSON (?json).
func (s *apiServer) serveNotifications(w http.ResponseWriter, r *http.Request, email Email) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ses, err := s.storage.NewSession(r.Context())
	if err != nil {
		http.Error(w, "failed to open storage session", http.StatusInternalServerError)
		return
	}

	rawItems, err := s.storage.GetUnackedItems(ses, email.String())
	if err != nil {
		http.Error(w, "failed to query pending notifications", http.StatusInternalServerError)
		return
	}

	itemSet := Items{}
	itemSet.Append(rawItems...)
	sources, flattened := buildNotificationSources(email, itemSet, s.sourceOrder, time.Now())

	resp := pendingNotificationsResponse{
		Email:      email.String(),
		TotalItems: len(flattened),
		Sources:    make([]pendingSourceResponse, 0, len(sources)),
	}
	for _, src := range sources {
		sr := pendingSourceResponse{Name: src.Name, Items: make([]pendingItemResponse, 0, len(src.Items))}
		for _, item := range src.Items {
			sr.Items = append(sr.Items, pendingItemResponse{
				ID:                 item.Id,
				Title:              item.Title,
				Link:               item.Link,
				GUID:               item.Id,
				Score:              item.Score,
				PublishedAt:        item.PublishedAt,
				UpdatedAt:          item.UpdatedAt,
				DisplayPublishedAt: item.DisplayPublishedAt,
				DisplayUpdatedAt:   item.DisplayUpdatedAt,
			})
		}
		resp.Sources = append(resp.Sources, sr)
	}

	// Respond as JSON when ?json query param is present, otherwise render HTML.
	if r.URL.Query().Has("json") {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			s.logger.Error(err, "encode api response failed")
		}
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	if err := s.htmlTmpl.Execute(w, resp); err != nil {
		s.logger.Error(err, "render html response failed")
	}
}

func (s *apiServer) authorized(r *http.Request) bool {
	if s.authToken == "" {
		return true
	}
	const bearer = "Bearer "
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, bearer) {
		return false
	}
	return strings.TrimPrefix(authHeader, bearer) == s.authToken
}

func extractEmailFromPath(path string) (string, bool) {
	if !strings.HasPrefix(path, pendingPathPrefix) {
		return "", false
	}
	suffix := strings.TrimPrefix(path, pendingPathPrefix)
	parts := strings.Split(suffix, "/")
	if len(parts) != 3 || parts[1] != "notifications" || parts[2] != "pending" {
		return "", false
	}
	decoded, err := url.PathUnescape(parts[0])
	if err != nil || decoded == "" {
		return "", false
	}
	return decoded, true
}

func compactTimeString(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}

	// Normalize repeated spaces often seen in some source date strings.
	s = strings.Join(strings.Fields(s), " ")

	type timeLayout struct {
		layout  string
		hasDate bool
	}

	layouts := []timeLayout{
		{layout: time.RFC3339, hasDate: true},
		{layout: time.RFC1123, hasDate: true},
		{layout: time.RFC1123Z, hasDate: true},
		{layout: "2006-01-02 15:04:05 -0700", hasDate: true},
		{layout: "2006-01-02 15:04:05", hasDate: true},
		{layout: "15:04 -0700", hasDate: false},
		{layout: "15:04 MST", hasDate: false},
	}

	for _, layout := range layouts {
		t, err := time.Parse(layout.layout, s)
		if err != nil {
			continue
		}
		if layout.hasDate {
			// Keep date for datetime values while staying compact and fixed-width.
			return t.Format("01-02 15:04")
		}
		return t.Format("15:04")
	}

	// Fallback: keep a short fixed-width preview.
	if len(s) > 11 {
		return s[:11]
	}
	return s
}
