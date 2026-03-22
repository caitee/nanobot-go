package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"
)

type CronService struct {
	storePath string
	jobs      map[string]*CronJob
	timer     *time.Timer
	nextRun   time.Time
	onJob     func(job *CronJob) string
	mu        sync.RWMutex
	running   bool
	stopCh    chan struct{}
}

func NewCronService(storePath string, onJob func(job *CronJob) string) *CronService {
	return &CronService{
		storePath: storePath,
		jobs:      make(map[string]*CronJob),
		onJob:     onJob,
		stopCh:    make(chan struct{}),
	}
}

func (s *CronService) Start(ctx context.Context) error {
	s.mu.Lock()
	s.running = true
	s.loadJobs()
	s.scheduleNext()
	s.mu.Unlock()

	go s.run(ctx)
	return nil
}

func (s *CronService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.running = false
	close(s.stopCh)
	if s.timer != nil {
		s.timer.Stop()
	}
}

func (s *CronService) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-time.After(time.Second):
			s.mu.RLock()
			now := time.Now()
			if !now.Before(s.nextRun) {
				s.executeDueJobs()
				s.scheduleNext()
			}
			s.mu.RUnlock()
		}
	}
}

func (s *CronService) scheduleNext() {
	var earliest *time.Time
	for _, job := range s.jobs {
		if !job.Enabled {
			continue
		}
		next := s.nextRunTime(job)
		if next == nil {
			continue
		}
		if earliest == nil || next.Before(*earliest) {
			earliest = next
		}
	}
	if earliest != nil {
		s.nextRun = *earliest
		delay := time.Until(*earliest)
		if delay > 0 {
			if s.timer != nil {
				s.timer.Stop()
			}
			s.timer = time.NewTimer(delay)
		}
	}
}

func (s *CronService) nextRunTime(job *CronJob) *time.Time {
	now := time.Now()
	switch job.Schedule.Kind {
	case "at":
		t := time.UnixMilli(job.Schedule.AtMs)
		if t.After(now) {
			return &t
		}
		return nil
	case "every":
		return nil // Would need to track last run
	case "cron":
		// Simplified: use next minute
		t := now.Add(time.Minute)
		return &t
	default:
		return nil
	}
}

func (s *CronService) executeDueJobs() {
	for _, job := range s.jobs {
		if !job.Enabled {
			continue
		}
		if next := s.nextRunTime(job); next != nil && !time.Now().Before(*next) {
			go func(j *CronJob) {
				j.State = "running"
				if s.onJob != nil {
					s.onJob(j)
				}
				j.State = "done"
				if j.DeleteAfterRun {
					s.RemoveJob(j.ID)
				}
			}(job)
		}
	}
}

func (s *CronService) AddJob(name string, schedule CronSchedule, payload CronPayload) (*CronJob, error) {
	job := &CronJob{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Name:      name,
		Enabled:   true,
		Schedule:  schedule,
		Payload:   payload,
		State:     "pending",
		CreatedAt: time.Now(),
	}
	s.jobs[job.ID] = job
	s.saveJobs()
	return job, nil
}

func (s *CronService) RemoveJob(id string) bool {
	if _, ok := s.jobs[id]; ok {
		delete(s.jobs, id)
		s.saveJobs()
		return true
	}
	return false
}

func (s *CronService) ListJobs() []*CronJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var jobs []*CronJob
	for _, j := range s.jobs {
		jobs = append(jobs, j)
	}
	return jobs
}

func (s *CronService) loadJobs() {
	data, err := os.ReadFile(s.storePath)
	if err != nil {
		slog.Debug("no cron jobs file found", "path", s.storePath)
		return
	}
	json.Unmarshal(data, &s.jobs)
}

func (s *CronService) saveJobs() {
	data, _ := json.Marshal(s.jobs)
	os.WriteFile(s.storePath, data, 0644)
}
