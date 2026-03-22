package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

const (
	maxRunHistory = 20
	storeVersion  = 1
)

// CronService manages scheduled jobs
type CronService struct {
	storePath string
	store     *CronStore
	lastMtime int64
	timer     *time.Timer
	timerTask *asyncTask
	running   bool
	onJob     func(job *CronJob) // callback for job execution
	mu        sync.RWMutex
	stopCh    chan struct{}
	cron      *cron.Cron
}

type asyncTask struct {
	task   chan struct{}
	cancel func()
}

// NewCronService creates a new CronService
func NewCronService(storePath string, onJob func(job *CronJob)) *CronService {
	return &CronService{
		storePath: storePath,
		onJob:     onJob,
		stopCh:    make(chan struct{}),
		cron:      cron.New(cron.WithSeconds()),
	}
}

func nowMs() int64 {
	return time.Now().UnixMilli()
}

// computeNextRun calculates the next run time for a schedule
func computeNextRun(schedule *CronSchedule, nowMs int64) *int64 {
	switch schedule.Kind {
	case "at":
		if schedule.AtMs > nowMs {
			return &schedule.AtMs
		}
		return nil

	case "every":
		if schedule.EveryMs <= 0 {
			return nil
		}
		next := nowMs + schedule.EveryMs
		return &next

	case "cron":
		if schedule.Expr == "" {
			return nil
		}
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		scheduleExpr := schedule.Expr
		// Convert 5-field to 6-field if needed (add seconds)
		if len(schedule.Expr) > 0 && len(schedule.Expr) < 6 {
			scheduleExpr = "0 " + schedule.Expr
		}
		entry, err := parser.Parse(scheduleExpr)
		if err != nil {
			slog.Debug("failed to parse cron expression", "expr", schedule.Expr, "error", err)
			return nil
		}
		now := time.Now()
		if schedule.TZ != "" {
			if loc, err := time.LoadLocation(schedule.TZ); err == nil {
				now = now.In(loc)
			}
		}
		next := entry.Next(now)
		nextMs := next.UnixMilli()
		return &nextMs

	default:
		return nil
	}
}

func validateScheduleForAdd(schedule *CronSchedule) error {
	if schedule.TZ != "" && schedule.Kind != "cron" {
		return fmt.Errorf("tz can only be used with cron schedules")
	}
	if schedule.Kind == "cron" && schedule.TZ != "" {
		if _, err := time.LoadLocation(schedule.TZ); err != nil {
			return fmt.Errorf("unknown timezone '%s'", schedule.TZ)
		}
	}
	return nil
}

// Start starts the cron service
func (s *CronService) Start(ctx context.Context) error {
	s.mu.Lock()
	s.running = true
	s.loadStore()
	s.recomputeNextRuns()
	s.saveStore()
	s.armTimer()
	s.mu.Unlock()

	go s.run(ctx)
	slog.Info("Cron service started", "jobs", len(s.store.Jobs))
	return nil
}

func (s *CronService) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		}
	}
}

// Stop stops the cron service
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
	if s.timerTask != nil {
		s.timerTask.cancel()
	}
	s.cron.Stop()
}

func (s *CronService) recomputeNextRuns() {
	if s.store == nil {
		return
	}
	now := nowMs()
	for _, job := range s.store.Jobs {
		if job.Enabled {
			job.State.NextRunAtMs = computeNextRun(&job.Schedule, now)
		}
	}
}

func (s *CronService) getNextWakeMs() *int64 {
	if s.store == nil {
		return nil
	}
	var earliest *int64
	for _, job := range s.store.Jobs {
		if !job.Enabled {
			continue
		}
		if job.State.NextRunAtMs == nil {
			continue
		}
		if earliest == nil || *job.State.NextRunAtMs < *earliest {
			earliest = job.State.NextRunAtMs
		}
	}
	return earliest
}

func (s *CronService) armTimer() {
	if s.timerTask != nil {
		s.timerTask.cancel()
		s.timerTask = nil
	}

	nextWake := s.getNextWakeMs()
	if nextWake == nil || !s.running {
		return
	}

	delay := time.Duration(*nextWake-time.Now().UnixMilli()) * time.Millisecond
	if delay < 0 {
		delay = 0
	}

	s.timer = time.NewTimer(delay)
	task := &asyncTask{
		task: make(chan struct{}),
	}
	s.timerTask = task

	go func() {
		select {
		case <-s.timer.C:
			s.onTimer()
		case <-task.task:
			return
		}
	}()
}

func (s *CronService) onTimer() {
	s.mu.Lock()
	s.loadStore()
	if s.store == nil {
		s.mu.Unlock()
		return
	}

	now := nowMs()
	var dueJobs []*CronJob
	for _, job := range s.store.Jobs {
		if !job.Enabled {
			continue
		}
		if job.State.NextRunAtMs != nil && now >= *job.State.NextRunAtMs {
			dueJobs = append(dueJobs, job)
		}
	}
	s.mu.Unlock()

	for _, job := range dueJobs {
		s.executeJob(job)
	}

	s.mu.Lock()
	s.saveStore()
	s.armTimer()
	s.mu.Unlock()
}

func (s *CronService) executeJob(job *CronJob) {
	startMs := nowMs()
	slog.Info("Cron: executing job '%s' (%s)", job.Name, job.ID)

	var lastStatus string
	var lastError string

	if s.onJob != nil {
		s.onJob(job)
		lastStatus = "ok"
	} else {
		lastStatus = "ok"
	}

	endMs := nowMs()
	job.State.LastRunAtMs = &startMs
	job.UpdatedAtMs = endMs

	record := CronRunRecord{
		RunAtMs:    startMs,
		Status:     lastStatus,
		DurationMs: endMs - startMs,
		Error:      lastError,
	}
	job.State.RunHistory = append(job.State.RunHistory, record)
	if len(job.State.RunHistory) > maxRunHistory {
		job.State.RunHistory = job.State.RunHistory[len(job.State.RunHistory)-maxRunHistory:]
	}

	// Handle one-shot jobs
	if job.Schedule.Kind == "at" {
		if job.DeleteAfterRun {
			s.removeJobInternal(job.ID)
			return
		}
		job.Enabled = false
		job.State.NextRunAtMs = nil
	} else {
		// Compute next run
		job.State.NextRunAtMs = computeNextRun(&job.Schedule, nowMs())
	}

	slog.Info("Cron: job completed", "name", job.Name)
}

func (s *CronService) removeJobInternal(jobID string) {
	if s.store == nil {
		return
	}
	jobs := make([]*CronJob, 0, len(s.store.Jobs))
	for _, j := range s.store.Jobs {
		if j.ID != jobID {
			jobs = append(jobs, j)
		}
	}
	s.store.Jobs = jobs
}

// ========== Public API ==========

// ListJobs returns all jobs, optionally including disabled ones
func (s *CronService) ListJobs(includeDisabled bool) []*CronJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	store := s.loadStoreInternal()
	if store == nil {
		return nil
	}

	var jobs []*CronJob
	for _, j := range store.Jobs {
		if includeDisabled || j.Enabled {
			jobs = append(jobs, j)
		}
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].State.NextRunAtMs == nil {
			return false
		}
		if jobs[j].State.NextRunAtMs == nil {
			return true
		}
		return *jobs[i].State.NextRunAtMs < *jobs[j].State.NextRunAtMs
	})
	return jobs
}

// AddJob adds a new job
func (s *CronService) AddJob(
	name string,
	schedule CronSchedule,
	message string,
	deliver bool,
	channel string,
	to string,
	deleteAfterRun bool,
) (*CronJob, error) {
	if err := validateScheduleForAdd(&schedule); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	store := s.loadStoreInternal()
	if store == nil {
		store = &CronStore{Version: storeVersion}
	}
	now := nowMs()

	job := &CronJob{
		ID:             fmt.Sprintf("%d", now),
		Name:           name,
		Enabled:        true,
		Schedule:       schedule,
		Payload: CronPayload{
			Kind:    "agent_turn",
			Message: message,
			Deliver: deliver,
			Channel: channel,
			To:      to,
		},
		State: CronJobState{
			NextRunAtMs: computeNextRun(&schedule, now),
		},
		CreatedAtMs:    now,
		UpdatedAtMs:    now,
		DeleteAfterRun: deleteAfterRun,
	}

	store.Jobs = append(store.Jobs, job)
	s.store = store
	s.saveStoreInternal(store)
	s.armTimer()

	slog.Info("Cron: added job '%s' (%s)", name, job.ID)
	return job, nil
}

// RemoveJob removes a job by ID
func (s *CronService) RemoveJob(jobID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	store := s.loadStoreInternal()
	if store == nil {
		return false
	}
	before := len(store.Jobs)
	s.removeJobInternal(jobID)
	removed := len(store.Jobs) < before

	if removed {
		s.store = store
		s.saveStoreInternal(store)
		s.armTimer()
		slog.Info("Cron: removed job", "job_id", jobID)
	}
	return removed
}

// EnableJob enables or disables a job
func (s *CronService) EnableJob(jobID string, enabled bool) *CronJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	store := s.loadStoreInternal()
	if store == nil {
		return nil
	}
	for _, job := range store.Jobs {
		if job.ID == jobID {
			job.Enabled = enabled
			job.UpdatedAtMs = nowMs()
			if enabled {
				job.State.NextRunAtMs = computeNextRun(&job.Schedule, nowMs())
			} else {
				job.State.NextRunAtMs = nil
			}
			s.store = store
			s.saveStoreInternal(store)
			s.armTimer()
			return job
		}
	}
	return nil
}

// RunJob manually runs a job
func (s *CronService) RunJob(ctx context.Context, jobID string, force bool) bool {
	s.mu.Lock()
	store := s.loadStoreInternal()
	if store == nil {
		s.mu.Unlock()
		return false
	}
	for _, job := range store.Jobs {
		if job.ID == jobID {
			if !force && !job.Enabled {
				s.mu.Unlock()
				return false
			}
			s.mu.Unlock()
			s.executeJob(job)
			s.mu.Lock()
			s.saveStoreInternal(store)
			s.armTimer()
			s.mu.Unlock()
			return true
		}
	}
	s.mu.Unlock()
	return false
}

// GetJob returns a job by ID
func (s *CronService) GetJob(jobID string) *CronJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	store := s.loadStoreInternal()
	if store == nil {
		return nil
	}
	for _, j := range store.Jobs {
		if j.ID == jobID {
			return j
		}
	}
	return nil
}

// Status returns service status
func (s *CronService) Status() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	store := s.loadStoreInternal()
	jobs := 0
	if store != nil {
		jobs = len(store.Jobs)
	}
	return map[string]any{
		"enabled":        s.running,
		"jobs":           jobs,
		"next_wake_at_ms": s.getNextWakeMs(),
	}
}

// ========== Persistence ==========

func (s *CronService) loadStore() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store = s.loadStoreInternal()
}

func (s *CronService) loadStoreInternal() *CronStore {
	if s.store != nil && s.storePath != "" {
		if info, err := os.Stat(s.storePath); err == nil {
			mtime := info.ModTime().Unix()
			if mtime != s.lastMtime {
				slog.Info("Cron: jobs.json modified externally, reloading")
				s.store = nil
			}
		}
	}
	if s.store != nil {
		return s.store
	}

	if s.storePath == "" {
		return &CronStore{Version: storeVersion}
	}

	data, err := os.ReadFile(s.storePath)
	if err != nil {
		slog.Debug("no cron jobs file found", "path", s.storePath)
		return &CronStore{Version: storeVersion}
	}

	var store CronStore
	if err := json.Unmarshal(data, &store); err != nil {
		slog.Warn("Failed to load cron store", "error", err)
		return &CronStore{Version: storeVersion}
	}
	s.lastMtime = 0
	if info, err := os.Stat(s.storePath); err == nil {
		s.lastMtime = info.ModTime().Unix()
	}
	return &store
}

func (s *CronService) saveStore() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store == nil {
		return
	}
	s.saveStoreInternal(s.store)
}

func (s *CronService) saveStoreInternal(store *CronStore) {
	if s.storePath == "" {
		return
	}
	dir := filepath.Dir(s.storePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		slog.Error("failed to create cron store directory", "error", err)
		return
	}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		slog.Error("failed to marshal cron store", "error", err)
		return
	}
	if err := os.WriteFile(s.storePath, data, 0644); err != nil {
		slog.Error("failed to save cron store", "error", err)
		return
	}
	if info, err := os.Stat(s.storePath); err == nil {
		s.lastMtime = info.ModTime().Unix()
	}
}
