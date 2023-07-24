package main

import (
	"context"
	"database/sql"
	"net/url"
	"time"

	"github.com/maxnilz/feed/errors"
)

func NewStorage(cfg Config) (Storage, error) {
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

type Feeds struct {
	Emails []Email
	Sites  []Site
	List   []*Feed
	Map    map[Email]map[Site][]*Feed
}

func (fs *Feeds) Append(feeds ...*Feed) {
	for _, feed := range feeds {
		fs.List = append(fs.List, feed)
		if fs.Map == nil {
			fs.Map = make(map[Email]map[Site][]*Feed)
		}
		sitesFeeds, ok := fs.Map[feed.Email]
		if !ok {
			sitesFeeds = make(map[Site][]*Feed)
			fs.Map[feed.Email] = sitesFeeds
			fs.Emails = append(fs.Emails, feed.Email)
		}
		site := Site{Name: feed.SiteName, URL: feed.SiteURL}
		if _, ok = sitesFeeds[site]; !ok {
			fs.Sites = append(fs.Sites, site)
		}
		sitesFeeds[site] = append(sitesFeeds[site], feed)
	}
}

func (fs *Feeds) SiteFeeds(email Email, site Site) []*Feed {
	sitesFeeds, ok := fs.Map[email]
	if !ok {
		return nil
	}
	out, ok := sitesFeeds[site]
	if !ok {
		return nil
	}
	return out
}
