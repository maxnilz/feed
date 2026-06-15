package main

import (
	"bytes"
	"fmt"
	"html/template"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/maxnilz/feed/errors"
	"github.com/maxnilz/feed/logging"
)

const emailBodyTemplate = `<body>
{{- range . -}}
<h1>New posts from {{.Name}}</h1>
<ol>
{{- range .Items -}}
<li><a href="{{.Link}}">{{.Title}}</a>
{{- if .Id}}&nbsp;<a href="{{.Id}}">[guid]</a>{{- end}}
&nbsp;[{{printf "%.2f" .Score}}]
&nbsp;{{compactTime .DisplayPublishedAt}}
{{- if .DisplayUpdatedAt}}&nbsp;{{compactTime .DisplayUpdatedAt}}{{- end}}</li>
{{- end -}}
</ol>
{{- end -}}
</body>`

func mustEmailTemplate() *template.Template {
	return template.Must(
		template.New("email").
			Funcs(template.FuncMap{"compactTime": compactTimeString}).
			Parse(emailBodyTemplate),
	)
}

type NotifyCallback func(items ...*Item) error

type Notifier interface {
	Notify(email Email, items Items, callback NotifyCallback) error
}

func NewNotifier(cfg Config, logger logging.Logger) (Notifier, error) {
	mailSender := cfg.MailSender
	host, _, err := net.SplitHostPort(mailSender.SmtpServer)
	if err != nil {
		return nil, errors.Newf(errors.InvalidArgument, err, "invalid host port")
	}
	senderAddr, password := mailSender.SenderAddr, mailSender.Password
	if senderAddr == "" || password == "" {
		return nil, errors.Newf(errors.InvalidArgument, nil, "invalid sender mail config")
	}
	auth := smtp.PlainAuth("", senderAddr, password, host)

	tmpl := mustEmailTemplate()

	sourceOrder := buildSourceOrder(cfg.Subscribers)

	// TODO: need to support smtp over socks or http proxy

	return &smtpNotifier{
		hostPort:    mailSender.SmtpServer,
		senderAddr:  senderAddr,
		password:    password,
		auth:        auth,
		Logger:      logger,
		tmpl:        tmpl,
		sourceOrder: sourceOrder,
		sendMail:    smtp.SendMail,
	}, nil
}

type sendMailFunc func(addr string, a smtp.Auth, from string, to []string, msg []byte) error

type smtpNotifier struct {
	hostPort    string
	password    string
	auth        smtp.Auth
	senderAddr  string
	Logger      logging.Logger
	tmpl        *template.Template
	sourceOrder map[Email][]string
	sendMail    sendMailFunc
}

func (s *smtpNotifier) Notify(email Email, items Items, callback NotifyCallback) error {
	sources, fs := buildNotificationSources(email, items, s.sourceOrder, time.Now())
	if len(sources) == 0 {
		return nil
	}

	buf := bytes.Buffer{}
	buf.WriteString(fmt.Sprintf("From: %s\r\n", s.senderAddr))
	buf.WriteString(fmt.Sprintf("To: %s\r\n", email.String()))
	buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	buf.WriteString("Subject: Semantic RSS reading list\r\n")
	buf.WriteString("\r\n") // Important: blank line between headers and body

	if err := s.tmpl.Execute(&buf, sources); err != nil { // Pass sources directly to template
		return errors.Newf(errors.Internal, err, "execute email template failed")
	}

	s.Logger.Info("Send RSS feeds notification", "email", email, "items", len(fs))
	if err := s.sendMail(s.hostPort, s.auth, s.senderAddr, []string{email.String()}, buf.Bytes()); err != nil {
		const shortErrMsg = "short response: "
		// Ignore the error if it's a short response error, refer to
		//  smpt.Client.Quit
		//    smpt.Client.cmd
		//      c.Text.ReadResponse(expectCode)
		//        net.textproto.Reader.ReadResponse
		//          net.textproto.Reader.readCodeLine
		//            net.textproto.Reader.parseCodeLine
		if !strings.HasPrefix(err.Error(), shortErrMsg) {
			return errors.Newf(errors.Internal, err, "send feeds failed")
		}
	}
	if callback != nil {
		if err := callback(fs...); err != nil {
			s.Logger.Error(err, "callback failed")
		}
	}
	return nil
}

func formatDisplayTime(s string, now time.Time) string {
	// Try parsing with common layouts
	layouts := []string{
		time.RFC3339,
		time.RFC1123,
		time.RFC1123Z,
		"2006-01-02 15:04:05",
	}

	var t time.Time
	var err error
	parsed := false

	for _, layout := range layouts {
		t, err = time.Parse(layout, s)
		if err == nil {
			parsed = true
			break
		}
	}

	if !parsed {
		return s
	}

	// Check if today (same year, month, day)
	if t.Year() == now.Year() && t.Month() == now.Month() && t.Day() == now.Day() {
		// Return time and timezone
		return t.Format("15:04 MST")
	}

	return s
}
