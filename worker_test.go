package main

import (
	"context"
	stderr "errors"
	"fmt"
	"log"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxnilz/feed/errors"
	"github.com/mmcdole/gofeed"
	"gopkg.in/yaml.v3"
)

func TestWorkerRun(t *testing.T) {
	if os.Getenv("LOCAL_DEV") != "true" {
		t.Skip("Skipping test in non-local environment")
	}
	wd, _ := os.Getwd()
	configFile := filepath.Join(wd, "config-prod.yaml")

	f, err := os.Open(configFile)
	if err != nil {
		log.Fatalf("open %s failed", configFile)
	}
	defer f.Close()

	var config Config
	dec := yaml.NewDecoder(f)
	if err = dec.Decode(&config); err != nil {
		log.Fatalf("invalid config file: %v", err)
	}

	logger := VerboseLogger

	storage, err := NewStorage(config)
	if err != nil {
		log.Fatal(err)
	}
	mailbox, err := NewMailbox(config, logger)
	if err != nil {
		log.Fatal(err)
	}

	subscriber := config.Subscribers[0]
	w, err := NewWorker(subscriber, storage, mailbox)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = w.Run(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestErr(t *testing.T) {
	err := textproto.ProtocolError("short response: ")
	if !stderr.Is(err, textproto.ProtocolError("short response: ")) {
		t.Errorf("error not matched: %v", err)
	}
}

func TestParseFeed(t *testing.T) {
	url := "https://hnrss.org/newest?q=prompt+engineering"
	feed, err := parseFeedSource(url)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(len(feed.Items))
}

func parseFeedSource(url string) (*gofeed.Feed, error) {
	ctx := context.Background()
	client := http.DefaultClient
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.Newf(errors.Internal, err, "create get request to %v failed", url)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.Newf(errors.Internal, err, "request feeds to %v failed", url)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.Newf(errors.Internal, nil, "invalid feed response: %v", resp.Status)
	}

	fp := gofeed.NewParser()
	feed, err := fp.Parse(resp.Body)
	if err != nil {
		return nil, errors.Newf(errors.Internal, err, "parse feed from %v failed", url)
	}
	return feed, nil
}
