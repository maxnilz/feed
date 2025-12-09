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

// sessionKey is the context key for storing Session
type sessionKey struct{}

// WithSession returns a new context with the session stored in it
func WithSession(ctx context.Context, ses Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, ses)
}

// sessionFromContext retrieves the session from context
// Returns nil if no session is stored in the context
func sessionFromContext(ctx context.Context) Session {
	ses, _ := ctx.Value(sessionKey{}).(Session)
	return ses
}

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
	SaveItems(ses Session, items ...*Item) error
	AckItems(ses Session, at time.Time, items ...*Item) error
	GetCursor(ses Session, email, source string) (time.Time, error)
	UpdateCursor(ses Session, email, source string, latestPublishedAt time.Time) error
	GetUnackedItems(ses Session, email string) ([]*Item, error)
	ArchiveItems(ses Session, before time.Time) (int64, error)
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

type Item struct {
	Id          string
	Email       Email
	SourceURL   string
	SourceName  string
	Title       string
	Description string
	Content     string
	Link        string
	UpdatedAt   string
	PublishedAt string
	Author      string
	Score       float32
	FetchAt     time.Time
}

type Email string

func (e Email) String() string {
	return string(e)
}

type UserItems struct {
	// map of source name to items
	m     map[string][]*Item
	names []string
}

func (sf *UserItems) add(name string, item *Item) {
	if sf.m == nil {
		sf.m = make(map[string][]*Item)
	}
	if _, ok := sf.m[name]; !ok {
		sf.names = append(sf.names, name)
	}
	sf.m[name] = append(sf.m[name], item)
}

func (sf *UserItems) get(name string) ([]*Item, bool) {
	items, ok := sf.m[name]
	return items, ok
}

type Items struct {
	// items by email
	m map[Email]*UserItems
}

func (fs *Items) Append(items ...*Item) {
	if fs.m == nil {
		fs.m = make(map[Email]*UserItems)
	}
	for _, item := range items {
		userItems, ok := fs.m[item.Email]
		if !ok {
			userItems = &UserItems{}
			fs.m[item.Email] = userItems
		}
		source := item.SourceName
		userItems.add(source, item)
	}
}

func (fs *Items) Emails() []Email {
	emails := make([]Email, 0, len(fs.m))
	for email := range fs.m {
		emails = append(emails, email)
	}
	return emails
}

func (fs *Items) List() []*Item {
	var allItems []*Item
	for _, userItems := range fs.m {
		for _, sourceName := range userItems.names {
			allItems = append(allItems, userItems.m[sourceName]...)
		}
	}
	return allItems
}

func (fs *Items) UserItems(email Email) (*UserItems, bool) {
	userItems, ok := fs.m[email]
	return userItems, ok
}

func (fs *Items) String() string {
	sb := strings.Builder{}
	allItems := fs.List()
	// total count
	sb.WriteString(fmt.Sprintf("Total Items: %d\n", len(allItems)))
	// header
	sb.WriteString("Id | Email | SourceURL | Title\n")
	sb.WriteString(strings.Repeat("-", 60))
	sb.WriteString("\n")
	// items
	for _, item := range allItems {
		sb.WriteString(item.Id)
		sb.WriteString(" | ")
		sb.WriteString(item.Email.String())
		sb.WriteString(" | ")
		sb.WriteString(item.SourceURL)
		sb.WriteString(" | ")
		sb.WriteString(item.Title)
		sb.WriteString("\n")
	}
	return sb.String()
}
