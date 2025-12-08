package main

import (
	"bytes"
	"fmt"
	"html/template"
	"net"
	"net/smtp"
	"strings"

	"github.com/maxnilz/feed/errors"
)

const emailBodyTemplate = `<body>
{{- range . -}}
<h1>New posts from {{.Name}}</h1>
<ol>
{{- range .Items -}}
<li><a href="{{.Link}}">{{.Title}}</a>
{{- if .Id}}&nbsp;<a href="{{.Id}}">[guid]</a>{{- end}}
&nbsp;{{.PublishedAt}}
{{- if .UpdatedAt}}&nbsp;{{.UpdatedAt}}{{- end}}
</li>
{{- end -}}
</ol>
{{- end -}}
</body>`

type NotifyCallback func(items ...*Item) error

type Notifier interface {
	Notify(email Email, items Items, callback NotifyCallback) error
}

func NewNotifier(cfg Config, logger Logger) (Notifier, error) {
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

	tmpl := template.Must(template.New("email").Parse(emailBodyTemplate))

	// Build source order map from config
	sourceOrder := make(map[Email][]string)
	for _, subscriber := range cfg.Subscribers {
		var sourceNames []string
		for _, source := range subscriber.Sources {
			sourceNames = append(sourceNames, source.Name)
		}
		sourceOrder[Email(subscriber.Email)] = sourceNames
	}

	// TODO: need to support smtp over socks or http proxy

	return &smtpNotifier{
		hostPort:    mailSender.SmtpServer,
		senderAddr:  senderAddr,
		password:    password,
		auth:        auth,
		Logger:      logger,
		tmpl:        tmpl,
		sourceOrder: sourceOrder,
	}, nil
}

type smtpNotifier struct {
	hostPort    string
	password    string
	auth        smtp.Auth
	senderAddr  string
	Logger      Logger
	tmpl        *template.Template
	sourceOrder map[Email][]string
}

type sourceData struct {
	Name  string
	Items []*Item
}

func (s *smtpNotifier) Notify(email Email, items Items, callback NotifyCallback) error {
	userItems, ok := items.UserItems(email)
	if !ok {
		return nil
	}

	var sources []sourceData
	var fs []*Item

	// Use config order if available, otherwise fall back to insertion order
	sourceNames := s.sourceOrder[email]
	if len(sourceNames) == 0 {
		sourceNames = userItems.names
	}

	for _, source := range sourceNames {
		sourceItems, ok := userItems.get(source)
		if !ok || len(sourceItems) == 0 {
			continue
		}
		sources = append(sources, sourceData{
			Name:  source,
			Items: sourceItems,
		})
		fs = append(fs, sourceItems...)
	}

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
	if err := smtp.SendMail(s.hostPort, s.auth, s.senderAddr, []string{email.String()}, buf.Bytes()); err != nil {
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
