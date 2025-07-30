package main

import (
	"context"
	stderr "errors"
	"log"
	"net/textproto"
	"os"
	"path/filepath"
	"testing"

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
