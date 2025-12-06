package main

import (
	"context"
	"fmt"
	"time"

	"github.com/maxnilz/feed/errors"
)

type ArchiverJob struct {
	storage       Storage
	logger        Logger
	archivePeriod time.Duration // e.g., 7 * 24 * time.Hour (7 days)
}

func NewArchiverJob(storage Storage, logger Logger, archivePeriod time.Duration) *ArchiverJob {
	return &ArchiverJob{
		storage:       storage,
		logger:        logger,
		archivePeriod: archivePeriod,
	}
}

func (aj *ArchiverJob) Name() string {
	return "archiver-job"
}

func (aj *ArchiverJob) Run(ctx context.Context) error {
	aj.logger.Info("running archiver job")

	before := time.Now().Add(-aj.archivePeriod)

	ses, err := aj.storage.NewSession(ctx)
	if err != nil {
		return errors.Newf(errors.Internal, err, "new session failed")
	}
	defer ses.Rollback() // Ensure rollback

	archivedCount, err := aj.storage.ArchiveItems(ses, before)
	if err != nil {
		return errors.Newf(errors.Internal, err, "archive items failed")
	}

	if err := ses.Commit(); err != nil {
		return errors.Newf(errors.Internal, err, "commit archiver job failed")
	}

	aj.logger.Info(fmt.Sprintf("archived %d items", archivedCount))
	return nil
}
