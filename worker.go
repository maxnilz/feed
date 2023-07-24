package main

import (
	"context"
	"io"
	"net/http"
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

	Sites         []Site
	Subscriptions map[Site][]Subscriber

	fp *gofeed.Parser
}

func NewWorker(cfg Config, storage Storage, mailbox Mailbox) *Worker {
	var sites []Site
	subscriptions := map[Site][]Subscriber{}
	for _, it := range cfg.Subscribers {
		for _, site := range it.Sites {
			if _, ok := subscriptions[site]; !ok {
				sites = append(sites, site)
				subscriptions[site] = make([]Subscriber, 0)
			}
			subscriptions[site] = append(subscriptions[site], it)
		}
	}
	return &Worker{
		storage:       storage,
		mailbox:       mailbox,
		Sites:         sites,
		Subscriptions: subscriptions,
		fp:            gofeed.NewParser(),
	}
}

func (w *Worker) Name() string {
	return "feeds worker"
}

func (w *Worker) Run(ctx context.Context) error {
	var feeds Feeds
	for _, site := range w.Sites {
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
	subscribers := w.Subscriptions[site]
	if len(subscribers) == 0 {
		return nil, nil
	}
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
	for _, it := range subscribers {
		var cursor time.Time
		cursor, err = w.storage.GetLatestFeedWaterMark(ses, it.Email, site.URL)
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
				Email:       Email(it.Email),
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
