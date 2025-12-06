# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test Commands

```bash
# Build (requires CGO for sqlite3)
CGO_ENABLED=1 go build -o feed

# Run tests
go test ./...

# Run a single test
go test -run TestFunctionName ./...

# Run with verbose output
go test -v ./...

# Run the application
./feed -config config.yaml -verbose
```

## Architecture Overview

This is an RSS feed aggregator that fetches RSS feeds on a schedule, optionally filters items using AI (Gemini embeddings or LLM), and sends email notifications to subscribers.

### Core Components

**Scheduler (`schedule.go`)** - Custom cron-based job scheduler using `robfig/cron/v3`. Jobs implement the `Job` interface with `Name()` and `Run(ctx)` methods.

**Fetcher (`fetcher.go`)** - Scheduled job that fetches RSS feeds for a subscriber. Uses `mmcdole/gofeed` for parsing. Maintains a cursor (last published timestamp) per email+source to avoid re-processing items.

**NotifierJob (`notifier_job.go`)** - Scheduled job that sends email notifications for unacknowledged items. Each subscriber has their own schedule.

**ArchiverJob (`archiver_job.go`)** - Daily job that moves acknowledged items older than 7 days to history table.

**Storage (`storage.go`, `sqlite.go`)** - SQLite-based persistence with session/transaction support. Tables: `item`, `history_item`, `subscription_cursor`.

**AI Filtering (`ai/`)** - Optional semantic filtering of feed items:
- `FilterTypeEmbedding` (default): Uses Gemini `text-embedding-004` to compute cosine similarity between items and filter criteria
- `FilterTypeLLM`: Uses Gemini generative model for relevance scoring
- Items below `similarityThreshold` are filtered out

### Data Flow

1. `Fetcher.Run()` fetches RSS feeds → filters by cursor → applies AI filter → saves new items
2. `NotifierJob.Run()` queries unacked items → sends email via SMTP → marks items as acknowledged
3. `ArchiverJob.Run()` moves old acknowledged items to history table

### Configuration

See `config.example.yaml`. Key fields:
- `dsn`: SQLite connection string (`sqlite3:///path/to/feed.db`)
- `geminiAPIKey`: For AI filtering (optional)
- `fetchInterval`: How often to fetch feeds (default 10m)
- `subscribers[].schedule`: Cron expression for notification emails
- `subscribers[].sources[].filterType`: `embedding` or `llm`
- `subscribers[].sources[].semanticFilters`: List of topic strings for AI filtering
- `subscribers[].sources[].similarityThreshold`: Minimum similarity score (0.0-1.0)

### Error Handling

Uses custom error package (`errors/`) with error codes (InvalidArgument, Internal, Unimplemented) and wrapped errors via `golang.org/x/xerrors`.
