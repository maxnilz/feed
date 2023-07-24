package main

import (
	"bytes"
	"fmt"
	"net"
	"net/smtp"

	"github.com/maxnilz/feed/errors"
)

type SendCallback func(feeds ...*Feed) error

type Mailbox interface {
	SendFeeds(feeds Feeds, callback SendCallback) error
}

func NewMailbox(cfg Config, logger Logger) (Mailbox, error) {
	mailSender := cfg.MailSender
	host, port, err := net.SplitHostPort(mailSender.SmtpServer)
	if err != nil {
		return nil, errors.Newf(errors.InvalidArgument, err, "invalid hostport")
	}
	senderAddr, password := mailSender.SenderAddr, mailSender.Password
	if senderAddr == "" || password == "" {
		return nil, errors.Newf(errors.InvalidArgument, nil, "invalid sender mail config")
	}
	auth := smtp.PlainAuth("", senderAddr, password, host)

	// TODO: need to support smtp over socks or http proxy

	return &smtpImpl{
		hostPort:   mailSender.SmtpServer,
		host:       host,
		port:       port,
		senderAddr: senderAddr,
		password:   password,
		auth:       auth,
		Logger:     logger,
	}, nil
}

type smtpImpl struct {
	hostPort, host, port string
	username, password   string
	auth                 smtp.Auth
	senderAddr           string
	Logger               Logger
}

func (s *smtpImpl) SendFeeds(feeds Feeds, callback SendCallback) error {
	var fs []*Feed
	for _, email := range feeds.Emails {
		buf := bytes.Buffer{}
		buf.WriteString("From: ")
		buf.WriteString(s.senderAddr)
		buf.WriteString("\n")
		buf.WriteString("To: ")
		buf.WriteString(email.String())
		buf.WriteString("\n")
		buf.WriteString("Content-Type: text/html; charset=UTF-8\n")
		buf.WriteString("Subject: RSS feeds notification\n\n")
		buf.WriteString("<body>")
		for _, site := range feeds.Sites {
			siteFeeds := feeds.SiteFeeds(email, site)
			if len(siteFeeds) == 0 {
				continue
			}
			fs = append(fs, siteFeeds...)
			buf.WriteString(fmt.Sprintf("<h1>New posts from <a href=\"%s\">%s</a></h1>", site.URL, site.Name))
			buf.WriteString("<ol>")
			for _, feed := range siteFeeds {
				buf.WriteString("<li>")
				buf.WriteString(fmt.Sprintf("<a href=\"%s\">%s</a>", feed.Link, feed.Title))
				buf.WriteString(fmt.Sprintf("&nbsp;%s", feed.PublishedAt))
				if feed.UpdatedAt != "" {
					buf.WriteString(fmt.Sprintf("&nbsp;%s", feed.UpdatedAt))
				}
				buf.WriteString("</li>")
			}
			buf.WriteString("</ol>")
		}
		buf.WriteString("</body>")
		s.Logger.Info("Send RSS feeds notification", "email", email)
		if err := smtp.SendMail(s.hostPort, s.auth, s.senderAddr, []string{email.String()}, buf.Bytes()); err != nil {
			return errors.Newf(errors.Internal, err, "send feeds failed")
		}
		if callback != nil {
			_ = callback(fs...)
		}
	}
	return nil
}
