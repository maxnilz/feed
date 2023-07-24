package main

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/maxnilz/feed/errors"
	"github.com/robfig/cron/v3"
)

type Job interface {
	Name() string
	Run(ctx context.Context) error
}

func NewScheduler(logger Logger) *Scheduler {
	return &Scheduler{
		Mutex:     sync.Mutex{},
		jobWaiter: sync.WaitGroup{},
		logger:    logger,
	}
}

type Scheduler struct {
	jobs []*CronJob

	sync.Mutex
	running   bool
	jobWaiter sync.WaitGroup

	logger Logger
}

func (s *Scheduler) Schedule(spec string, job Job) error {
	schedule, err := cron.ParseStandard(spec)
	if err != nil {
		return errors.Newf(errors.InvalidArgument, err, "invalid cron spec")
	}
	s.jobs = append(s.jobs, &CronJob{Job: job, Schedule: schedule})
	return nil
}

func (s *Scheduler) Run(ctx context.Context) {
	s.Lock()
	if s.running {
		s.Unlock()
		return
	}
	s.running = true
	s.Unlock()
	s.run(ctx)
}

func (s *Scheduler) run(ctx context.Context) {
	now := time.Now()
	for _, it := range s.jobs {
		it.Next = it.Schedule.Next(now)
		s.logger.Info("schedule", "now", now, "job", it.Job.Name(), "next", it.Next)
	}
	for {
		sort.Sort(byTime(s.jobs))
		var timer *time.Timer
		if len(s.jobs) == 0 || s.jobs[0].Next.IsZero() {
			// If there are no entries yet, just sleep - it still handles new entries
			// and stop requests.
			timer = time.NewTimer(100000 * time.Hour)
		} else {
			timer = time.NewTimer(s.jobs[0].Next.Sub(now))
		}

		for {
			select {
			case now = <-timer.C:
				s.logger.Info("wake", "now", now)
				// Run every job whose next time was less than now
				for _, it := range s.jobs {
					if it.Next.After(now) || it.Next.IsZero() {
						break
					}
					s.startJob(ctx, it.Job)
					it.Prev = it.Next
					it.Next = it.Schedule.Next(now)
					s.logger.Info("schedule job", "now", now, "job", it.Job.Name(), "next", it.Next)
				}
			case <-ctx.Done():
				s.logger.Info("stop")
				timer.Stop()
				return
			}
			break
		}
	}
}

func (s *Scheduler) startJob(ctx context.Context, job Job) {
	s.jobWaiter.Add(1)
	go func() {
		defer s.jobWaiter.Done()
		if err := job.Run(ctx); err != nil {
			s.logger.Error(err, "job failed", "job", job.Name())
		}
	}()
}

func (s *Scheduler) Stop() {
	s.Lock()
	defer s.Unlock()
	if s.running {
		s.running = false
	}
	s.jobWaiter.Wait()
}

type Schedule cron.Schedule

type CronJob struct {
	Job      Job
	Next     time.Time
	Prev     time.Time
	Schedule Schedule
}

// byTime is a wrapper for sorting the job array by time
// (with zero time at the end).
type byTime []*CronJob

func (s byTime) Len() int      { return len(s) }
func (s byTime) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s byTime) Less(i, j int) bool {
	// Two zero times should return false.
	// Otherwise, zero is "greater" than any other time.
	// (To sort it at the end of the list.)
	if s[i].Next.IsZero() {
		return false
	}
	if s[j].Next.IsZero() {
		return true
	}
	return s[i].Next.Before(s[j].Next)
}
