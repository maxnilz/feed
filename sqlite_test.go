package main

import (
	"context"
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

func TestSqllite(t *testing.T) {
	dbfile := "/tmp/feed.db"
	s, err := newSQLite(dbfile)
	if err != nil {
		t.Fatalf("open storage with %s failed: %v", dbfile, err)
	}
	feeds := []*Feed{
		{
			"1",
			"a@example.com",
			"https://foo.com/index.rss",
			"hello foo",
			"a hello message",
			"hello, my dear friend",
			"https://foo.com/1",
			"2023-07-22 07:00:00",
			"",
			"2023-07-22 07:00:00",
			"foo",
			mustParseTime("2023-07-22 07:00:00"),
		},
		{
			"2",
			"b@example.com",
			"https://foo.com/index.rss",
			"hello foo",
			"a hello message",
			"hello, my dear friend",
			"https://foo.com/1",
			"2023-07-22 08:00:00",
			"",
			"2023-07-22 08:00:00",
			"foo",
			mustParseTime("2023-07-22 08:00:00"),
		},
		{
			"nack",
			"b@example.com",
			"https://foo.com/index.rss",
			"hello foo",
			"a hello message",
			"hello, my dear friend",
			"https://foo.com/1",
			"2023-07-22 09:00:00",
			"",
			"2023-07-22 09:00:00",
			"foo",
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

	if err := s.SaveFeeds(ses, feeds...); err != nil {
		t.Fatal(err)
	}

	ackAt := mustParseTime("2023-07-22 09:00:00")
	for _, it := range feeds {
		if it.Id == "nack" {
			continue
		}
		if err := s.AckFeeds(ses, ackAt, it.Id); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		email             string
		site              string
		expectedWaterMark time.Time
	}{
		{"a@example.com", "https://foo.com/index.rss", mustParseTime("2023-07-22 07:00:00")},
		{"b@example.com", "https://foo.com/index.rss", mustParseTime("2023-07-22 08:00:00")},
		{"c@example.com", "https://foo.com/index.rss", time.Time{}},
	}
	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			got, err := s.GetLatestFeedWaterMark(ses, c.email, c.site)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.expectedWaterMark {
				t.Fatalf("expected %v, got %v", c.expectedWaterMark, got)
			}
		})
	}
}
