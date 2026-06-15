package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func mustParseTime(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestMigration(t *testing.T) {
	dbFile := "/tmp/feed_migration_test.db"
	os.Remove(dbFile) // Start fresh

	// 1. Setup old schema (manually)
	db, err := sql.Open("sqlite3", dbFile)
	if err != nil {
		t.Fatal(err)
	}
	oldSchema := `
    CREATE TABLE item (
        id TEXT NOT NULL,
        email TEXT NOT NULL,
        source TEXT NOT NULL,
        source_name TEXT NOT NULL,
        title TEXT NOT NULL,
        description TEXT NOT NULL,
        content TEXT NOT NULL,
        link TEXT NOT NULL,
        updated_at TEXT,
        published_at TEXT NOT NULL,
        author TEXT NOT NULL,
        fetch_at TEXT NOT NULL,
        ack INTEGER NOT NULL DEFAULT 0,
        ack_at TEXT,
        PRIMARY KEY (id, email, source_name)
    );
    `
	if _, err := db.Exec(oldSchema); err != nil {
		db.Close()
		t.Fatalf("setup old schema failed: %v", err)
	}
	db.Close()

	// 2. Run Migration via newSQLite
	s, err := newSQLite(dbFile)
	if err != nil {
		t.Fatalf("newSQLite failed: %v", err)
	}
	defer s.Close()

	// 3. Verify Migration by saving and retrieving an item with score
	ctx := context.Background()
	ses, err := s.NewSession(ctx)
	if err != nil {
		t.Fatal(err)
	}

	item := &Item{
		Id:          "test-migration",
		Email:       "test@example.com",
		SourceName:  "test-source",
		Title:       "Test Title",
		PublishedAt: "2023-01-01 12:00:00",
		FetchAt:     time.Now(),
		Score:       0.75,
	}

	if err := s.SaveItems(ses, item); err != nil {
		t.Fatalf("SaveItems failed (migration issue?): %v", err)
	}

	// Verify
	items, err := s.GetUnackedItems(ses, "test@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Score != 0.75 {
		t.Errorf("expected score 0.75, got %f", items[0].Score)
	}
}

func TestSqllite(t *testing.T) {
	dbfile := "/tmp/feed.db"
	s, err := newSQLite(dbfile)
	if err != nil {
		t.Fatalf("open storage with %s failed: %v", dbfile, err)
	}
	items := []*Item{
		{
			"1",
			"a@example.com",
			"https://foo.com/index.rss",
			"Foo Feed",
			"hello foo",
			"a hello message",
			"hello, my dear friend",
			"https://foo.com/1",
			"2023-07-22 07:00:00",
			"2023-07-22 07:00:00",
			"foo",
			0.5,
			mustParseTime("2023-07-22 07:00:00"),
		},
		{
			"2",
			"b@example.com",
			"https://foo.com/index.rss",
			"Foo Feed",
			"hello foo",
			"a hello message",
			"hello, my dear friend",
			"https://foo.com/1",
			"2023-07-22 08:00:00",
			"2023-07-22 08:00:00",
			"foo",
			0.8,
			mustParseTime("2023-07-22 08:00:00"),
		},
		{
			"nack",
			"b@example.com",
			"https://foo.com/index.rss",
			"Foo Feed",
			"hello foo",
			"a hello message",
			"hello, my dear friend",
			"https://foo.com/1",
			"2023-07-22 09:00:00",
			"2023-07-22 09:00:00",
			"foo",
			0.9,
			mustParseTime("2023-07-22 09:00:00"),
		},
	}
	ctx := context.Background()
	ses, err := s.NewSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ses, err = ses.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer ses.Rollback()

	if err := s.SaveItems(ses, items...); err != nil {
		t.Fatal(err)
	}

	ackAt := mustParseTime("2023-07-22 09:00:00")
	for _, it := range items {
		if it.Id == "nack" {
			continue
		}
		if err := s.AckItems(ses, ackAt, it); err != nil {
			t.Fatal(err)
		}
	}

	// Test UpdateCursor and GetCursor
	if err := s.UpdateCursor(ses, "a@example.com", "https://foo.com/index.rss", mustParseTime("2023-07-22 07:00:00")); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateCursor(ses, "b@example.com", "https://foo.com/index.rss", mustParseTime("2023-07-22 08:00:00")); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		email          string
		site           string
		expectedCursor time.Time
	}{
		{"a@example.com", "https://foo.com/index.rss", mustParseTime("2023-07-22 07:00:00")},
		{"b@example.com", "https://foo.com/index.rss", mustParseTime("2023-07-22 08:00:00")},
		{"c@example.com", "https://foo.com/index.rss", time.Time{}}, // no cursor set
	}
	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			got, err := s.GetCursor(ses, c.email, c.site)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.expectedCursor {
				t.Fatalf("expected %v, got %v", c.expectedCursor, got)
			}
		})
	}
}

func TestSQLiteArchiveItems(t *testing.T) {
	dbfile := filepath.Join(t.TempDir(), "feed_archive.db")
	s, err := newSQLite(dbfile)
	if err != nil {
		t.Fatalf("open storage with %s failed: %v", dbfile, err)
	}
	defer s.Close()

	ctx := context.Background()
	ses, err := s.NewSession(ctx)
	if err != nil {
		t.Fatal(err)
	}

	items := []*Item{
		{
			Id:          "old-ack-1",
			Email:       "archive@example.com",
			SourceURL:   "https://foo.com/index.rss",
			SourceName:  "Foo Feed",
			Title:       "old acknowledged 1",
			Description: "desc",
			Content:     "content",
			Link:        "https://foo.com/1",
			UpdatedAt:   "2023-07-20 07:00:00",
			PublishedAt: "2023-07-20 07:00:00",
			Author:      "foo",
			Score:       0.6,
			FetchAt:     mustParseTime("2023-07-20 07:00:00"),
		},
		{
			Id:          "old-ack-2",
			Email:       "archive@example.com",
			SourceURL:   "https://foo.com/index.rss",
			SourceName:  "Foo Feed",
			Title:       "old acknowledged 2",
			Description: "desc",
			Content:     "content",
			Link:        "https://foo.com/2",
			UpdatedAt:   "2023-07-21 07:00:00",
			PublishedAt: "2023-07-21 07:00:00",
			Author:      "foo",
			Score:       0.7,
			FetchAt:     mustParseTime("2023-07-21 07:00:00"),
		},
		{
			Id:          "recent-ack",
			Email:       "archive@example.com",
			SourceURL:   "https://foo.com/index.rss",
			SourceName:  "Foo Feed",
			Title:       "recent acknowledged",
			Description: "desc",
			Content:     "content",
			Link:        "https://foo.com/3",
			UpdatedAt:   "2023-07-23 07:00:00",
			PublishedAt: "2023-07-23 07:00:00",
			Author:      "foo",
			Score:       0.8,
			FetchAt:     mustParseTime("2023-07-23 07:00:00"),
		},
		{
			Id:          "old-unacked",
			Email:       "archive@example.com",
			SourceURL:   "https://foo.com/index.rss",
			SourceName:  "Foo Feed",
			Title:       "old unacknowledged",
			Description: "desc",
			Content:     "content",
			Link:        "https://foo.com/4",
			UpdatedAt:   "2023-07-20 08:00:00",
			PublishedAt: "2023-07-20 08:00:00",
			Author:      "foo",
			Score:       0.9,
			FetchAt:     mustParseTime("2023-07-20 08:00:00"),
		},
	}

	if err := s.SaveItems(ses, items...); err != nil {
		t.Fatal(err)
	}

	ackAt := mustParseTime("2023-07-24 07:00:00")
	if err := s.AckItems(ses, ackAt, items[0], items[1], items[2]); err != nil {
		t.Fatal(err)
	}

	archived, err := s.ArchiveItems(ses, mustParseTime("2023-07-22 00:00:00"))
	if err != nil {
		t.Fatal(err)
	}
	if archived != 2 {
		t.Fatalf("expected archived count 2, got %d", archived)
	}

	var itemCount int
	if err := ses.QueryRow(`SELECT COUNT(*) FROM item`).Scan(&itemCount); err != nil {
		t.Fatal(err)
	}
	if itemCount != 2 {
		t.Fatalf("expected 2 rows remaining in item table, got %d", itemCount)
	}

	var historyCount int
	if err := ses.QueryRow(`SELECT COUNT(*) FROM history_item`).Scan(&historyCount); err != nil {
		t.Fatal(err)
	}
	if historyCount != 2 {
		t.Fatalf("expected 2 rows in history_item table, got %d", historyCount)
	}

	var oldUnackedInHistory int
	if err := ses.QueryRow(`SELECT COUNT(*) FROM history_item WHERE id = 'old-unacked'`).Scan(&oldUnackedInHistory); err != nil {
		t.Fatal(err)
	}
	if oldUnackedInHistory != 0 {
		t.Fatalf("expected old-unacked to stay out of history table, got count %d", oldUnackedInHistory)
	}
}

