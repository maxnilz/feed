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
	flag.StringVar(&configFile, "config", "config.yaml", "configuration file")
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

	scheduler := NewScheduler(logger)
	for _, subscriber := range config.Subscribers {
		worker, err := NewWorker(subscriber, storage, mailbox)
		if err != nil {
			log.Fatal(err)
		}
		if err = scheduler.Schedule(subscriber.Schedule, worker); err != nil {
			log.Fatal(err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	scheduler.Start(ctx)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan bool, 1)
	go func() {
		<-sigs
		done <- true
	}()

	<-done

	cancel()
	scheduler.Stop()
	storage.Close()
}
