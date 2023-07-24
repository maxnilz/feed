package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"gopkg.in/yaml.v3"
)

func main() {
	var configFile string
	var verbose bool
	flag.StringVar(&configFile, "config", "", "configuration file")
	flag.BoolVar(&verbose, "verbose", false, "verbose log")
	flag.Parse()
	if configFile == "" {
		log.Fatal("config file is missing")
	}
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

	logger := DefaultLogger
	if verbose {
		logger = VerboseLogger
	}

	// TODO: integrate with dependency injection, e.g. wire
	storage, err := NewStorage(config)
	if err != nil {
		log.Fatal(err)
	}
	mailbox, err := NewMailbox(config, logger)
	if err != nil {
		log.Fatal(err)
	}
	worker := NewWorker(config, storage, mailbox)

	scheduler := NewScheduler(logger)
	for _, it := range config.Subscribers {
		if err = scheduler.Schedule(it.Schedule, worker); err != nil {
			log.Fatal(err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		scheduler.Run(ctx)
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan bool, 1)
	go func() {
		<-sigs
		cancel()
		scheduler.Stop()
		storage.Close()
		done <- true
	}()

	<-done
}
