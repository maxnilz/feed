package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/maxnilz/feed/errors"
)

func NewStorage(cfg Config) (Storage, error) {
	if cfg.DSN == "" {
		return nil, errors.Newf(errors.InvalidArgument, nil, "missing dsn")
	}
	u, err := url.Parse(cfg.DSN)
	if err != nil {
		return nil, errors.Newf(errors.InvalidArgument, err, "invalid dsn: %s", cfg.DSN)
	}
	switch u.Scheme {
	case "sqlite", "sqlite3":
		return newSQLite(u.Path)
	default:
		return nil, errors.Newf(errors.Unimplemented, nil, "unsupported db: %s", u.Scheme)
	}
}

type Storage interface {
	NewSession(ctx context.Context) (Session, error)
	NewAutoSession(ctx context.Context) (Session, error)
	SaveFeeds(ses Session, feeds ...*Feed) error
	AckFeeds(ses Session, at time.Time, feedIds ...string) error
	GetLatestFeedWaterMark(ses Session, email, site string) (time.Time, error)
	Close() error
}

type Session interface {
	// Begin starts a transactional session.
	//
	// It's the user's responsibility to manage the session,
	// Either Rollback or Commit MUST be called to pair with Begin to avoid transaction leak.
	Begin() (Session, error)
	// Rollback aborts the changes made by the transactional session.
	Rollback() error
	// Commit commits the changes made by the transactional session.
	Commit() error
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

type Feed struct {
	Id          string
	Email       Email
	SiteURL     string
	SiteName    string
	Title       string
	Description string
	Content     string
	Link        string
	UpdatedAt   string
	PublishedAt string
	Author      string
	FetchAt     time.Time
}

type Email string

func (e Email) String() string {
	return string(e)
}

type SitesFeeds struct {
	// map of site name to feeds
	m     map[string][]*Feed
	names []string
}

func (sf *SitesFeeds) add(name string, feed *Feed) {
	if sf.m == nil {
		sf.m = make(map[string][]*Feed)
	}
	if _, ok := sf.m[name]; !ok {
		sf.names = append(sf.names, name)
	}
	sf.m[name] = append(sf.m[name], feed)
}

func (sf *SitesFeeds) get(name string) ([]*Feed, bool) {
	feeds, ok := sf.m[name]
	return feeds, ok
}

type Feeds struct {
	Emails []Email
	List   []*Feed

	// feeds by email
	m map[Email]*SitesFeeds
}

func (fs *Feeds) Append(feeds ...*Feed) {
	if fs.m == nil {
		fs.m = make(map[Email]*SitesFeeds)
	}
	for _, feed := range feeds {
		fs.List = append(fs.List, feed)
		sitesFeeds, ok := fs.m[feed.Email]
		if !ok {
			sitesFeeds = &SitesFeeds{}
			fs.m[feed.Email] = sitesFeeds
			fs.Emails = append(fs.Emails, feed.Email)
		}
		site := feed.SiteName
		sitesFeeds.add(site, feed)
	}
}

func (fs *Feeds) SitesFeeds(email Email) (*SitesFeeds, bool) {
	sitesFeeds, ok := fs.m[email]
	return sitesFeeds, ok
}

func (fs *Feeds) String() string {
	sb := strings.Builder{}
	// total count
	sb.WriteString(fmt.Sprintf("Total Feeds: %d\n", len(fs.List)))
	// header
	sb.WriteString("Id | Email | SiteURL | Title\n")
	sb.WriteString(strings.Repeat("-", 60))
	sb.WriteString("\n")
	// items
	for _, feed := range fs.List {
		sb.WriteString(feed.Id)
		sb.WriteString(" | ")
		sb.WriteString(feed.Email.String())
		sb.WriteString(" | ")
		sb.WriteString(feed.SiteURL)
		sb.WriteString(" | ")
		sb.WriteString(feed.Title)
		sb.WriteString("\n")
	}
	return sb.String()
}
