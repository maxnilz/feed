package main

import (
	"os"
	"time"

	"github.com/maxnilz/feed/ai"
)

type Config struct {
	DSN           string          `yaml:"dsn"`
	Filter        ai.FilterConfig `yaml:"filter"`
	FetchInterval time.Duration   `yaml:"fetchInterval"`
	FetchTimeout  time.Duration   `yaml:"fetchTimeout"`
	Subscribers   []Subscriber    `yaml:"subscribers"`
	MailSender    MailSender      `yaml:"mailSender"`
}

type Subscriber struct {
	Name     string   `yaml:"name"`
	Email    string   `yaml:"email"`
	Sources  []Source `yaml:"sources"`
	Schedule string   `yaml:"schedule"`
}

type Source struct {
	Name string   `yaml:"name"`
	URL  string   `yaml:"url"`
	URLs []string `yaml:"urls"` // Multiple URLs for a single source

	// Embed SourceFilterConfig for per-source filter settings
	ai.SourceFilterConfig `yaml:",inline"`
}

// AllURLs returns all configured URLs for this source, combining URL and URLs fields.
func (s Source) AllURLs() []string {
	var urls []string
	if s.URL != "" {
		urls = append(urls, s.URL)
	}
	urls = append(urls, s.URLs...)
	return urls
}

type MailSender struct {
	SmtpServer string `yaml:"smtpServer"`
	SenderAddr string `yaml:"senderAddr"`
	Password   string `yaml:"password"`
}

// ApplyEnvOverrides applies environment variable overrides to the config.
// Environment variables take precedence over YAML config values.
func (c *Config) ApplyEnvOverrides() {
	if v := os.Getenv("GEMINI_API_KEY"); v != "" {
		c.Filter.GeminiAPIKey = v
	}
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		c.Filter.OpenAIAPIKey = v
	}
	if v := os.Getenv("SMTP_PASSWORD"); v != "" {
		c.MailSender.Password = v
	}
	if v := os.Getenv("DSN"); v != "" {
		c.DSN = v
	}
}
