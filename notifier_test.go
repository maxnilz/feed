package main

import (
	"fmt"
	"html/template"
	"net/smtp"
	"strings"
	"testing"
	"time"
)

type mockLogger struct{}

func (m *mockLogger) Info(msg string, args ...any) {}
func (m *mockLogger) Error(err error, msg string, args ...any) {}

func TestSmtpNotifier(t *testing.T) {
	// Setup
	tmpl := template.Must(template.New("email").Parse(emailBodyTemplate))
	
	sent := false
	var sentMsg []byte

	notifier := &smtpNotifier{
		hostPort:   "localhost:25",
		senderAddr: "sender@example.com",
		Logger:     &mockLogger{},
		tmpl:       tmpl,
		sourceOrder: map[Email][]string{}, // No explicit order
		sendMail: func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
			sent = true
			sentMsg = msg
			if addr != "localhost:25" {
				t.Errorf("expected addr localhost:25, got %s", addr)
			}
			if from != "sender@example.com" {
				t.Errorf("expected from sender@example.com, got %s", from)
			}
			if len(to) != 1 || to[0] != "user@example.com" {
				t.Errorf("expected to [user@example.com], got %v", to)
			}
			return nil
		},
	}

	// Create items
	now := time.Now()
	// An item published today
	itemToday := &Item{
		Id:          "1",
		Email:       "user@example.com",
		SourceName:  "Source A",
		Title:       "Title Today",
		Link:        "http://example.com/1",
		PublishedAt: now.Format(time.RFC3339),
		UpdatedAt:   now.Add(1 * time.Hour).Format(time.RFC3339),
		Score:       0.95,
	}
	// An item published yesterday
	yesterday := now.Add(-24 * time.Hour)
	itemYesterday := &Item{
		Id:          "2",
		Email:       "user@example.com",
		SourceName:  "Source A",
		Title:       "Title Yesterday",
		Link:        "http://example.com/2",
		PublishedAt: yesterday.Format(time.RFC3339),
		UpdatedAt:   yesterday.Add(1 * time.Hour).Format(time.RFC3339),
		Score:       0.55,
	}

	items := Items{}
	items.Append(itemToday, itemYesterday)

	// Execute
	err := notifier.Notify(Email("user@example.com"), items, func(items ...*Item) error {
		return nil
	})
	if err != nil {
		t.Fatalf("Notify failed: %v", err)
	}

	if !sent {
		t.Fatal("sendMail was not called")
	}

	body := string(sentMsg)

	// Verify Content
	if !strings.Contains(body, "Title Today") {
		t.Error("body missing Title Today")
	}
	if !strings.Contains(body, "Title Yesterday") {
		t.Error("body missing Title Yesterday")
	}

	// Verify Score Rendering
	if !strings.Contains(body, fmt.Sprintf("%.2f", 0.95)) {
		t.Errorf("body missing score 0.95")
	}
	if !strings.Contains(body, fmt.Sprintf("%.2f", 0.55)) {
		t.Errorf("body missing score 0.55")
	}

	// Verify Date Rendering
	// Today should be HH:MM MST
	expectedTime := now.Format("15:04 MST")
	if !strings.Contains(body, expectedTime) {
		t.Errorf("body missing formatted time for today: %s", expectedTime)
	}
	// Today's UpdatedAt
	expectedUpdatedTime := now.Add(1 * time.Hour).Format("15:04 MST")
	if !strings.Contains(body, expectedUpdatedTime) {
		t.Errorf("body missing formatted updated time for today: %s", expectedUpdatedTime)
	}

	// Yesterday should include date (RFC3339 or whatever formatPublishedAt returns for non-today)
	// formatPublishedAt returns original string if not today.
	// Note: html/template escapes '+', so we check for that.
	expectedYesterday := strings.ReplaceAll(yesterday.Format(time.RFC3339), "+", "&#43;")
	if !strings.Contains(body, expectedYesterday) {
		t.Logf("Body content:\n%s", body)
		t.Errorf("body missing full date for yesterday: %s", expectedYesterday)
	}
	// Yesterday's UpdatedAt
	expectedUpdatedYesterday := strings.ReplaceAll(yesterday.Add(1*time.Hour).Format(time.RFC3339), "+", "&#43;")
	if !strings.Contains(body, expectedUpdatedYesterday) {
		t.Errorf("body missing full updated date for yesterday: %s", expectedUpdatedYesterday)
	}
}
