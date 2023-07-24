package main

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/maxnilz/feed/errors"
)

var _ Storage = (*sqllite)(nil)

func newSQLite(dbfile string) (*sqllite, error) {
	db, err := sql.Open("sqlite3", dbfile)
	if err != nil {
		return nil, errors.Newf(errors.Internal, err, "open sqlite db %v failed", dbfile)
	}
	s := &sqllite{db: db}
	if err = s.migrate(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

type sqllite struct {
	db *sql.DB
}

func (s *sqllite) NewAutoSession(ctx context.Context) (Session, error) {
	return &sqliteTxn{db: s.db, ctx: ctx, autoCommit: true}, nil

}

func (s *sqllite) NewSession(ctx context.Context) (Session, error) {
	return &sqliteTxn{db: s.db, ctx: ctx}, nil
}

func (s *sqllite) SaveFeeds(ses Session, feeds ...*Feed) error {
	q := `
INSERT INTO feed (id, email, site, title, description, content, link, updated_at, published_at, author, fetch_at) 
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
`
	for _, it := range feeds {
		args := []interface{}{
			it.Id, it.Email, it.SiteURL, it.Title, it.Description, it.Content, it.Link, it.UpdatedAt,
			it.PublishedAt, it.Author, it.FetchAt.Format("2006-01-02 15:01:05"),
		}
		if _, err := ses.Exec(q, args...); err != nil {
			return errors.Newf(errors.Internal, err, "save feeds failed")
		}
	}
	return nil
}

func (s *sqllite) AckFeeds(ses Session, at time.Time, feedIds ...string) error {
	q := `UPDATE feed SET ack = 1, ack_at = ? WHERE id = ?`
	for _, id := range feedIds {
		args := []interface{}{at.Format("2006-01-02 15:01:05"), id}
		if _, err := ses.Exec(q, args...); err != nil {
			return errors.Newf(errors.Internal, err, "ack feeds failed")
		}
	}
	return nil
}

func (s *sqllite) GetLatestFeedWaterMark(ses Session, email, site string) (time.Time, error) {
	q := `SELECT max(datetime(fetch_at)) FROM feed WHERE email = ? AND site = ? AND ack = 1`
	r := ses.QueryRow(q, email, site)
	var out *string
	if err := r.Scan(&out); err != nil {
		return time.Time{}, errors.Newf(errors.Internal, err, "get latest notification water mark failed")
	}
	if out == nil {
		return time.Time{}, nil
	}
	return time.Parse("2006-01-02 15:01:05", *out)
}

func (s *sqllite) migrate(ctx context.Context) error {
	q := `
CREATE TABLE IF NOT EXISTS feed (
    id TEXT NOT NULL,
    email TEXT NOT NULL,
    site TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    content TEXT NOT NULL,
    link TEXT NOT NULL,
    updated_at TEXT,
    published_at TEXT NOT NULL,
    author TEXT NOT NULL,
    fetch_at TEXT NOT NULL,
    ack INTEGER NOT NULL DEFAULT 0,
    ack_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_feed_id ON feed(id);
CREATE INDEX IF NOT EXISTS idx_feed_email_site ON feed(email, site);
`
	if _, err := s.db.ExecContext(ctx, q); err != nil {
		return errors.Newf(errors.Internal, err, "migrate sqlite schemas failed")
	}
	return nil
}

func (s *sqllite) Close() error {
	return s.db.Close()
}

var _ Session = (*sqliteTxn)(nil)

type sqliteTxn struct {
	db  *sql.DB
	ctx context.Context

	txn        *sql.Tx
	autoCommit bool
}

func (s *sqliteTxn) Begin() (Session, error) {
	if s.autoCommit {
		return s, nil
	}
	if s.txn != nil {
		return nil, errors.Newf(errors.Unimplemented, nil, "unsupported nest txn")
	}
	var err error
	s.txn, err = s.db.BeginTx(s.ctx, nil)
	if err != nil {
		return nil, errors.Newf(errors.Internal, err, "begin tx failed")
	}
	return s, nil
}

func (s *sqliteTxn) Rollback() error {
	if s.autoCommit {
		return nil
	}
	return s.txn.Rollback()
}

func (s *sqliteTxn) Commit() error {
	if s.autoCommit {
		return nil
	}
	return s.txn.Commit()
}

func (s *sqliteTxn) Exec(query string, args ...any) (sql.Result, error) {
	if s.autoCommit {
		return s.db.ExecContext(s.ctx, query, args...)
	}
	return s.txn.ExecContext(s.ctx, query, args...)
}

func (s *sqliteTxn) Query(query string, args ...any) (*sql.Rows, error) {
	if s.autoCommit {
		return s.db.QueryContext(s.ctx, query, args...)
	}
	return s.txn.QueryContext(s.ctx, query, args...)
}

func (s *sqliteTxn) QueryRow(query string, args ...any) *sql.Row {
	if s.autoCommit {
		return s.db.QueryRowContext(s.ctx, query, args...)
	}
	return s.txn.QueryRowContext(s.ctx, query, args...)
}
