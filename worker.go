package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/maxnilz/feed/errors"
	"github.com/mmcdole/gofeed"
)

type Worker struct {
	storage Storage
	mailbox Mailbox

	subscriber Subscriber

	fp *gofeed.Parser
}

func NewWorker(subscriber Subscriber, storage Storage, mailbox Mailbox) (*Worker, error) {
	if subscriber.Name == "" {
		return nil, errors.Newf(errors.InvalidArgument, nil, "subscriber name is required")
	}
	if subscriber.Email == "" {
		return nil, errors.Newf(errors.InvalidArgument, nil, "subscriber email is required")
	}
	for _, site := range subscriber.Sites {
		if _, err := url.Parse(site.URL); err != nil {
			return nil, errors.Newf(errors.InvalidArgument, nil, "found invalid site url in %s", subscriber.Name)
		}
	}
	return &Worker{
		storage:    storage,
		mailbox:    mailbox,
		subscriber: subscriber,
		fp:         gofeed.NewParser(),
	}, nil
}

func (w *Worker) Name() string {
	return fmt.Sprintf(w.subscriber.Name)
}

func (w *Worker) Run(ctx context.Context) error {
	var feeds Feeds
	for _, site := range w.subscriber.Sites {
		out, err := w.collectFeedsFromSite(ctx, site)
		if err != nil {
			return err
		}
		feeds.Append(out...)
	}
	// log feeds first,
	// then send feeds & ack later
	ses, err := w.storage.NewSession(ctx)
	if err != nil {
		return err
	}
	ses, err = ses.Begin()
	if err != nil {
		return err
	}
	if err = w.storage.SaveFeeds(ses, feeds.List...); err != nil {
		_ = ses.Rollback()
		return err
	}
	if err = ses.Commit(); err != nil {
		return err
	}
	// Send feeds
	if err = w.mailbox.SendFeeds(feeds, w.ackFeeds); err != nil {
		return err
	}

	return nil
}

func (w *Worker) collectFeedsFromSite(ctx context.Context, site Site) ([]*Feed, error) {
	client := http.DefaultClient
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, site.URL, nil)
	if err != nil {
		return nil, errors.Newf(errors.Internal, err, "create get request to %v failed", site.URL)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.Newf(errors.Internal, err, "request feeds to %v failed", site.URL)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.Newf(errors.Internal, nil, "invalid feed response: %v", resp.Status)
	}
	return w.collectFeeds(ctx, site, resp.Body)
}

func (w *Worker) collectFeeds(ctx context.Context, site Site, r io.Reader) ([]*Feed, error) {
	feed, err := w.fp.Parse(r)
	if err != nil {
		return nil, errors.Newf(errors.Internal, err, "parse feeds at %v failed", site.URL)
	}
	if len(feed.Items) == 0 {
		return nil, nil
	}
	sort.Sort(feed)

	ses, err := w.storage.NewAutoSession(ctx)
	if err != nil {
		return nil, err
	}

	var feeds []*Feed
	var cursor time.Time
	cursor, err = w.storage.GetLatestFeedWaterMark(ses, w.subscriber.Email, site.URL)
	if err != nil {
		return nil, err
	}
	for i := 0; i < len(feed.Items); i++ {
		f := feed.Items[i]
		tm := f.PublishedParsed
		if f.UpdatedParsed != nil {
			tm = f.UpdatedParsed
		}
		if !tm.After(cursor) {
			continue
		}
		authors := make([]string, 0, len(f.Authors))
		for _, a := range f.Authors {
			authors = append(authors, a.Name)
		}
		ent := &Feed{
			Id:          uuid.NewString(),
			Email:       Email(w.subscriber.Email),
			SiteURL:     site.URL,
			SiteName:    site.Name,
			Title:       f.Title,
			Description: f.Description,
			Content:     f.Content,
			Link:        f.Link,
			UpdatedAt:   f.Updated,
			PublishedAt: f.Published,
			Author:      strings.Join(authors, ", "),
			FetchAt:     time.Now(),
		}
		feeds = append(feeds, ent)
	}
	return feeds, nil
}

func (w *Worker) ackFeeds(feeds ...*Feed) error {
	ses, err := w.storage.NewSession(context.Background())
	if err != nil {
		return err
	}
	ses, err = ses.Begin()
	if err != nil {
		return err
	}
	defer ses.Rollback()

	ids := make([]string, 0, len(feeds))
	for _, f := range feeds {
		ids = append(ids, f.Id)
	}
	if err = w.storage.AckFeeds(ses, time.Now(), ids...); err != nil {
		return err
	}
	if err = ses.Commit(); err != nil {
		return err
	}
	return nil
}
