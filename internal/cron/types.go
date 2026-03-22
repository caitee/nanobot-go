package cron

// CronSchedule defines job schedule
type CronSchedule struct {
	Kind    string `json:"kind"` // "at", "every", "cron"
	AtMs    int64  `json:"atMs,omitempty"`
	EveryMs int64  `json:"everyMs,omitempty"`
	Expr    string `json:"expr,omitempty"` // cron expression
	TZ      string `json:"tz,omitempty"`   // timezone
}

// CronPayload defines job payload
type CronPayload struct {
	Kind    string `json:"kind,omitempty"`    // "agent_turn"
	Message string `json:"message,omitempty"`
	Deliver bool   `json:"deliver,omitempty"`
	Channel string `json:"channel,omitempty"`
	To      string `json:"to,omitempty"`
}

// CronRunRecord represents a single job execution
type CronRunRecord struct {
	RunAtMs    int64  `json:"runAtMs"`
	Status     string `json:"status"`
	DurationMs int64  `json:"durationMs,omitempty"`
	Error      string `json:"error,omitempty"`
}

// CronJobState represents the current state of a job
type CronJobState struct {
	NextRunAtMs   *int64         `json:"nextRunAtMs,omitempty"`
	LastRunAtMs   *int64         `json:"lastRunAtMs,omitempty"`
	LastStatus    string         `json:"lastStatus,omitempty"`
	LastError     string         `json:"lastError,omitempty"`
	RunHistory    []CronRunRecord `json:"runHistory,omitempty"`
}

// CronJob represents a scheduled job
type CronJob struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Enabled        bool          `json:"enabled"`
	Schedule       CronSchedule  `json:"schedule"`
	Payload        CronPayload   `json:"payload"`
	State          CronJobState  `json:"state,omitempty"`
	CreatedAtMs    int64         `json:"createdAtMs"`
	UpdatedAtMs    int64         `json:"updatedAtMs"`
	DeleteAfterRun bool          `json:"deleteAfterRun"`
}

// CronStore represents the persisted store of all cron jobs
type CronStore struct {
	Version int        `json:"version"`
	Jobs    []*CronJob `json:"jobs"`
}