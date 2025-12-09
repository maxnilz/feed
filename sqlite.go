package main

import (
	"context"
	"database/sql"
	stderr "errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/maxnilz/feed/errors"
)

const sqliteDateTimeFormat = "2006-01-02 15:04:05"

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

func (s *sqllite) NewSession(ctx context.Context) (Session, error) {
	if ses := sessionFromContext(ctx); ses != nil {
		return ses, nil
	}
	return &sqliteTxn{db: s.db, ctx: ctx}, nil
}

func (s *sqllite) SaveItems(ses Session, items ...*Item) error {
	if len(items) == 0 {
		return nil
	}

	const batchSize = 50
	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}
		batch := items[i:end]

		var placeholders []string
		var args []interface{}
		for _, it := range batch {
			placeholders = append(placeholders, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
			args = append(args,
				it.Id, it.Email, it.SourceURL, it.SourceName, it.Title, it.Description, it.Content, it.Link, it.UpdatedAt,
				it.PublishedAt, it.Author, it.Score, it.FetchAt.Format(sqliteDateTimeFormat),
			)
		}

		q := fmt.Sprintf(`
INSERT OR IGNORE INTO item (id, email, source, source_name, title, description, content, link, updated_at, published_at, author, score, fetch_at)
VALUES %s;`, strings.Join(placeholders, ","))

		if _, err := ses.Exec(q, args...); err != nil {
			return errors.Newf(errors.Internal, err, "save items failed")
		}
	}
	return nil
}

func (s *sqllite) AckItems(ses Session, at time.Time, items ...*Item) error {
	if len(items) == 0 {
		return nil
	}

	const batchSize = 50
	ackTime := at.Format(sqliteDateTimeFormat)

	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}
		batch := items[i:end]

		placeholders := make([]string, len(batch))
		args := make([]interface{}, 0, len(batch)*3+1)
		args = append(args, ackTime)
		for j, item := range batch {
			placeholders[j] = "(?, ?, ?)"
			args = append(args, item.Id, item.Email, item.SourceName)
		}

		q := fmt.Sprintf(`UPDATE item SET ack = 1, ack_at = ? WHERE (id, email, source_name) IN (%s)`, strings.Join(placeholders, ","))
		if _, err := ses.Exec(q, args...); err != nil {
			return errors.Newf(errors.Internal, err, "ack items failed")
		}
	}
	return nil
}

func (s *sqllite) GetCursor(ses Session, email, source string) (time.Time, error) {
	q := `SELECT last_published_at FROM subscription_cursor WHERE email = ? AND source = ?`
	var out string
	if err := ses.QueryRow(q, email, source).Scan(&out); err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return time.Time{}, nil
		}
		return time.Time{}, errors.Newf(errors.Internal, err, "get cursor failed")
	}
	return time.Parse(sqliteDateTimeFormat, out)
}

func (s *sqllite) UpdateCursor(ses Session, email, source string, latestPublishedAt time.Time) error {
	q := `INSERT INTO subscription_cursor (email, source, last_published_at) VALUES (?, ?, ?)
          ON CONFLICT(email, source) DO UPDATE SET last_published_at = excluded.last_published_at`
	tStr := latestPublishedAt.Format(sqliteDateTimeFormat)
	if _, err := ses.Exec(q, email, source, tStr); err != nil {
		return errors.Newf(errors.Internal, err, "update cursor failed")
	}
	return nil
}

func (s *sqllite) ArchiveItems(ses Session, before time.Time) (int64, error) {
	cutoff := before.Format(sqliteDateTimeFormat)

	// Copy to history
	qCopy := `INSERT OR IGNORE INTO history_item SELECT * FROM item WHERE ack = 1 AND fetch_at < ?`
	res, err := ses.Exec(qCopy, cutoff)
	if err != nil {
		return 0, errors.Newf(errors.Internal, err, "copy items to history failed")
	}
	count, err := res.RowsAffected()
	if err != nil {
		return 0, errors.Newf(errors.Internal, err, "get rows affected failed")
	}

	// Delete from item
	qDel := `DELETE FROM item WHERE ack = 1 AND fetch_at < ?`
	if _, err := ses.Exec(qDel, cutoff); err != nil {
		return 0, errors.Newf(errors.Internal, err, "delete archived items failed")
	}

	return count, nil
}

func (s *sqllite) GetUnackedItems(ses Session, email string) ([]*Item, error) {
	q := `SELECT id, email, source, source_name, title, description, content, link, updated_at, published_at, author, score, fetch_at FROM item WHERE email = ? AND ack = 0 ORDER BY score DESC, published_at DESC`
	rows, err := ses.Query(q, email)
	if err != nil {
		return nil, errors.Newf(errors.Internal, err, "query unacked items failed")
	}
	defer rows.Close()

	var items []*Item
	for rows.Next() {
		item := &Item{}
		var fetchAtStr string // For scanning time string
		if err := rows.Scan(
			&item.Id, &item.Email, &item.SourceURL, &item.SourceName, &item.Title, &item.Description, &item.Content,
			&item.Link, &item.UpdatedAt, &item.PublishedAt, &item.Author, &item.Score, &fetchAtStr,
		); err != nil {
			return nil, errors.Newf(errors.Internal, err, "scan unacked item failed")
		}
		item.FetchAt, err = time.Parse(sqliteDateTimeFormat, fetchAtStr)
		if err != nil {
			return nil, errors.Newf(errors.Internal, err, "parse FetchAt failed")
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *sqllite) Close() error {
	return s.db.Close()
}

func (s *sqllite) migrate(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS item (
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
);`,
		`CREATE INDEX IF NOT EXISTS idx_item_email_source_ack ON item(email, source, ack);`,
		`CREATE TABLE IF NOT EXISTS history_item (
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
);`,
		`CREATE INDEX IF NOT EXISTS idx_history_item_fetch_at ON history_item(fetch_at);`,
		`CREATE TABLE IF NOT EXISTS subscription_cursor (
    email TEXT NOT NULL,
    source TEXT NOT NULL,
    last_published_at TEXT NOT NULL,
    PRIMARY KEY (email, source)
);`,
	}

	for _, q := range queries {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return errors.Newf(errors.Internal, err, "exec schema query failed")
		}
	}

	if err := s.addColumnIfNotExists(ctx, "item", "score", "REAL NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfNotExists(ctx, "history_item", "score", "REAL NOT NULL DEFAULT 0"); err != nil {
		return err
	}

	return nil
}

func (s *sqllite) addColumnIfNotExists(ctx context.Context, table, column, colDef string) error {
	// Check if column exists
	q := fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name='%s'", table, column)
	var count int
	if err := s.db.QueryRowContext(ctx, q).Scan(&count); err != nil {
		return errors.Newf(errors.Internal, err, "check column %s.%s failed", table, column)
	}
	if count > 0 {
		return nil
	}

	// Add column
	alter := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, colDef)
	if _, err := s.db.ExecContext(ctx, alter); err != nil {
		return errors.Newf(errors.Internal, err, "add column %s.%s failed", table, column)
	}
	return nil
}

var _ Session = (*sqliteTxn)(nil)

type sqliteTxn struct {
	db  *sql.DB
	ctx context.Context
	txn *sql.Tx

	parent *sqliteTxn
}

func (s *sqliteTxn) Begin() (Session, error) {
	if s.txn != nil {
		// Nested transaction, return a new sqliteTxn
		// with the same underlying sql.Tx, where the
		// commit/rollback is noop for nested txns.
		txn := &sqliteTxn{
			db:     s.db,
			ctx:    s.ctx,
			txn:    s.txn,
			parent: s,
		}
		return txn, nil
	}
	var err error
	s.txn, err = s.db.BeginTx(s.ctx, nil)
	if err != nil {
		return nil, errors.Newf(errors.Internal, err, "begin tx failed")
	}
	return s, nil
}

func (s *sqliteTxn) Rollback() error {
	if s.parent != nil {
		return nil // only the top-level txn should rollback
	}
	return s.txn.Rollback()
}

func (s *sqliteTxn) Commit() error {
	if s.parent != nil {
		return nil // only the top-level txn should commit
	}
	return s.txn.Commit()
}

func (s *sqliteTxn) Exec(query string, args ...any) (sql.Result, error) {
	if s.txn == nil {
		// Non-transactional session
		return s.db.ExecContext(s.ctx, query, args...)
	}
	return s.txn.ExecContext(s.ctx, query, args...)
}

func (s *sqliteTxn) Query(query string, args ...any) (*sql.Rows, error) {
	if s.txn == nil {
		return s.db.QueryContext(s.ctx, query, args...)
	}
	return s.txn.QueryContext(s.ctx, query, args...)
}

func (s *sqliteTxn) QueryRow(query string, args ...any) *sql.Row {
	if s.txn == nil {
		return s.db.QueryRowContext(s.ctx, query, args...)
	}
	return s.txn.QueryRowContext(s.ctx, query, args...)
}
