package cron

import "time"

// CronSchedule defines job schedule
type CronSchedule struct {
    Kind    string `json:"kind"` // "at", "every", "cron"
    AtMs    int64  `json:"at_ms,omitempty"`
    EveryMs int64  `json:"every_ms,omitempty"`
    Expr    string `json:"expr,omitempty"`    // cron expression
    TZ      string `json:"tz,omitempty"`       // timezone
}

// CronPayload defines job payload
type CronPayload struct {
    Message string `json:"message"`
}

// CronJob represents a scheduled job
type CronJob struct {
    ID             string       `json:"id"`
    Name           string       `json:"name"`
    Enabled        bool         `json:"enabled"`
    Schedule       CronSchedule `json:"schedule"`
    Payload        CronPayload  `json:"payload"`
    State          string       `json:"state"` // "pending", "running", "done"
    DeleteAfterRun bool         `json:"delete_after_run"`
    CreatedAt      time.Time    `json:"created_at"`
    NextRunAt      *time.Time   `json:"next_run_at,omitempty"`
}
