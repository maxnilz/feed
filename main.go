package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/maxnilz/feed/ai"
	"github.com/maxnilz/feed/logging"
	"gopkg.in/yaml.v3"
)

func main() {
	// Load .env file if it exists (ignore error if not found)
	_ = godotenv.Load()

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

	// Override config with environment variables if set
	config.ApplyEnvOverrides()

	logger := logging.DefaultLogger
	if verbose {
		logger = logging.VerboseLogger
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create singleton filter
	filter, err := ai.NewFilter(ctx, logger, config.Filter)
	if err != nil {
		log.Fatalf("failed to create filter: %v", err)
	}
	defer filter.Close()

	// TODO: integrate with dependency injection, e.g. wire
	storage, err := NewStorage(config)
	if err != nil {
		log.Fatal(err)
	}
	defer storage.Close()

	notifier, err := NewNotifier(config, logger)
	if err != nil {
		log.Fatal(err)
	}

	scheduler := NewScheduler(logger)

	// Register Fetchers
	for _, subscriber := range config.Subscribers {
		fetcher, err := NewFetcher(subscriber, storage, filter, config.FetchTimeout, logger)
		if err != nil {
			log.Fatal(err)
		}

		// Fetcher runs on a global interval (or per source if config had it there)
		// For now, let's make it a fixed interval from config.
		// If fetchInterval is not set, use a default (e.g., 10 minutes)
		interval := config.FetchInterval
		if interval == 0 {
			interval = 10 * time.Minute
		}
		// Create a cron spec for the interval (e.g. "@every 10m")
		cronSpec := fmt.Sprintf("@every %s", interval.String())

		if err = scheduler.Schedule(cronSpec, fetcher); err != nil {
			log.Fatal(err)
		}
	}

	// Register NotifierJobs for each subscriber
	for _, subscriber := range config.Subscribers {
		notifierJob := NewNotifierJob(subscriber, storage, notifier, logger)
		if err = scheduler.Schedule(subscriber.Schedule, notifierJob); err != nil {
			log.Fatal(err)
		}
	}

	// Register ArchiverJob
	archiverJob := NewArchiverJob(storage, logger, 7*24*time.Hour)   // Archive items older than 7 days
	if err = scheduler.Schedule("@daily", archiverJob); err != nil { // Run once a day
		log.Fatal(err)
	}

	scheduler.Start(ctx)
	defer scheduler.Stop()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan bool, 1)
	go func() {
		<-sigs
		done <- true
	}()

	<-done

	cancel()
}
