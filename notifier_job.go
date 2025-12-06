package main

import (
	"context"
	"fmt"
	"time"

	"github.com/maxnilz/feed/errors"
)

type NotifierJob struct {
	subscriber Subscriber
	storage    Storage
	notifier   Notifier
	logger     Logger
}

func NewNotifierJob(subscriber Subscriber, storage Storage, notifier Notifier, logger Logger) *NotifierJob {
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

	if err := nj.notifier.Notify(itemsToSend, func(ackedItems ...*Item) error {
		// Acknowledge sent items in DB
		itemIDs := make([]string, 0, len(ackedItems))
		for _, item := range ackedItems {
			itemIDs = append(itemIDs, item.Id)
		}
		if len(itemIDs) > 0 {
			if err := nj.storage.AckItems(ses, time.Now(), itemIDs...); err != nil {
				return errors.Newf(errors.Internal, err, "ack items failed")
			}
		}
		return nil
	}); err != nil {
		return errors.Newf(errors.Internal, err, "notification failed")
	}

	return ses.Commit() // Commit transaction after successful notification and ack
}
