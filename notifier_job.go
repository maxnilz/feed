package main

import (
	"context"
	"fmt"
	"time"

	"github.com/maxnilz/feed/errors"
	"github.com/maxnilz/feed/logging"
)

type NotifierJob struct {
	subscriber Subscriber
	storage    Storage
	notifier   Notifier
	logger     logging.Logger
}

func NewNotifierJob(subscriber Subscriber, storage Storage, notifier Notifier, logger logging.Logger) *NotifierJob {
	return &NotifierJob{
		subscriber: subscriber,
		storage:    storage,
		notifier:   notifier,
		logger:     logger,
	}
}

func (nj *NotifierJob) Name() string {
	return fmt.Sprintf("notifier-job-%s", nj.subscriber.Name)
}

func (nj *NotifierJob) Run(ctx context.Context) error {
	nj.logger.Info("running notifier job", "subscriber", nj.subscriber.Name)

	ses, err := nj.storage.NewSession(ctx)
	if err != nil {
		return errors.Newf(errors.Internal, err, "new session failed")
	}
	ses, err = ses.Begin()
	if err != nil {
		return errors.Newf(errors.Internal, err, "begin transaction failed")
	}
	defer ses.Rollback()

	items, err := nj.storage.GetUnackedItems(ses, nj.subscriber.Email)
	if err != nil {
		return errors.Newf(errors.Internal, err, "get unacked items failed")
	}

	if len(items) == 0 {
		nj.logger.Info("no new items to notify", "subscriber", nj.subscriber.Name)
		return nil
	}

	// Group items into the 'Items' struct for notifier
	itemsToSend := Items{}
	itemsToSend.Append(items...)

	if err := nj.notifier.Notify(Email(nj.subscriber.Email), itemsToSend, func(ackedItems ...*Item) error {
		// Acknowledge sent items in DB
		if len(ackedItems) > 0 {
			if err := nj.storage.AckItems(ses, time.Now(), ackedItems...); err != nil {
				return errors.Newf(errors.Internal, err, "ack items failed")
			}
		}
		return nil
	}); err != nil {
		return errors.Newf(errors.Internal, err, "notification failed")
	}
	if err := ses.Commit(); err != nil {
		return errors.Newf(errors.Internal, err, "commit notifier job failed")
	}
	return nil
}
