package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"nanobot-go/internal/bus"
	"nanobot-go/internal/cron"
)

type CronTool struct {
	cronService *cron.CronService
	messageBus  bus.MessageBus
	channel     string
	chatID      string
}

func NewCronTool(cronService *cron.CronService, messageBus bus.MessageBus) *CronTool {
	return &CronTool{
		cronService: cronService,
		messageBus:  messageBus,
	}
}

func (t *CronTool) Name() string    { return "cron" }
func (t *CronTool) Description() string {
	return "Schedule reminders and recurring tasks. Actions: add, list, remove."
}
func (t *CronTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []any{"add", "list", "remove"},
				"description": "Action to perform",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Reminder message (for add)",
			},
			"every_seconds": map[string]any{
				"type":        "integer",
				"description": "Interval in seconds (for recurring tasks)",
			},
			"cron_expr": map[string]any{
				"type":        "string",
				"description": "Cron expression like '0 9 * * *' (for scheduled tasks)",
			},
			"tz": map[string]any{
				"type":        "string",
				"description": "IANA timezone for cron expressions (e.g. 'America/Vancouver')",
			},
			"at": map[string]any{
				"type":        "string",
				"description": "ISO datetime for one-time execution (e.g. '2026-02-12T10:30:00')",
			},
			"job_id": map[string]any{
				"type":        "string",
				"description": "Job ID (for remove)",
			},
		},
		"required": []any{"action"},
	}
}

func (t *CronTool) Execute(ctx context.Context, params map[string]any) (any, error) {
	action, _ := params["action"].(string)

	switch action {
	case "add":
		return t.addJob(params)
	case "list":
		return t.listJobs(), nil
	case "remove":
		return t.removeJob(params)
	}
	return nil, fmt.Errorf("unknown action: %s", action)
}

func (t *CronTool) addJob(params map[string]any) (any, error) {
	message, _ := params["message"].(string)
	if message == "" {
		return "Error: message is required for add", nil
	}
	if t.channel == "" || t.chatID == "" {
		return "Error: no session context (channel/chat_id)", nil
	}

	tz, _ := params["tz"].(string)
	cronExpr, _ := params["cron_expr"].(string)
	if tz != "" && cronExpr == "" {
		return "Error: tz can only be used with cron_expr", nil
	}

	var schedule cron.CronSchedule
	deleteAfterRun := false

	if everySeconds, ok := params["every_seconds"].(float64); ok && everySeconds > 0 {
		schedule = cron.CronSchedule{
			Kind:    "every",
			EveryMs: int64(everySeconds * 1000),
		}
	} else if cronExpr != "" {
		schedule = cron.CronSchedule{
			Kind: "cron",
			Expr: cronExpr,
			TZ:   tz,
		}
	} else if at, ok := params["at"].(string); ok && at != "" {
		dt, err := time.Parse(time.RFC3339, at)
		if err != nil {
			return fmt.Sprintf("Error: invalid ISO datetime format '%s'. Expected format: YYYY-MM-DDTHH:MM:SS", at), nil
		}
		schedule = cron.CronSchedule{
			Kind:  "at",
			AtMs:  dt.UnixMilli(),
		}
		deleteAfterRun = true
	} else {
		return "Error: either every_seconds, cron_expr, or at is required", nil
	}

	name := message
	if len(name) > 30 {
		name = name[:30]
	}

	job, err := t.cronService.AddJob(
		name,
		schedule,
		message,
		true,  // deliver
		t.channel,
		t.chatID,
		deleteAfterRun,
	)
	if err != nil {
		return fmt.Sprintf("Error: %v", err), nil
	}
	return fmt.Sprintf("Created job '%s' (id: %s)", job.Name, job.ID), nil
}

func (t *CronTool) listJobs() string {
	jobs := t.cronService.ListJobs(false)
	if len(jobs) == 0 {
		return "No scheduled jobs."
	}

	var lines []string
	for _, j := range jobs {
		timing := formatTiming(&j.Schedule)
		parts := []string{fmt.Sprintf("- %s (id: %s, %s)", j.Name, j.ID, timing)}
		if j.State.LastRunAtMs != nil {
			lastDt := time.UnixMilli(*j.State.LastRunAtMs).UTC()
			info := fmt.Sprintf("  Last run: %s — %s", lastDt.Format(time.RFC3339), j.State.LastStatus)
			if j.State.LastError != "" {
				info += fmt.Sprintf(" (%s)", j.State.LastError)
			}
			parts = append(parts, info)
		}
		if j.State.NextRunAtMs != nil {
			nextDt := time.UnixMilli(*j.State.NextRunAtMs).UTC()
			parts = append(parts, fmt.Sprintf("  Next run: %s", nextDt.Format(time.RFC3339)))
		}
		lines = append(lines, strings.Join(parts, "\n"))
	}
	return "Scheduled jobs:\n" + strings.Join(lines, "\n")
}

func (t *CronTool) removeJob(params map[string]any) (any, error) {
	jobID, _ := params["job_id"].(string)
	if jobID == "" {
		return "Error: job_id is required for remove", nil
	}
	if t.cronService.RemoveJob(jobID) {
		return fmt.Sprintf("Removed job %s", jobID), nil
	}
	return fmt.Sprintf("Job %s not found", jobID), nil
}

func formatTiming(schedule *cron.CronSchedule) string {
	switch schedule.Kind {
	case "cron":
		if schedule.TZ != "" {
			return fmt.Sprintf("cron: %s (%s)", schedule.Expr, schedule.TZ)
		}
		return fmt.Sprintf("cron: %s", schedule.Expr)
	case "every":
		ms := schedule.EveryMs
		if ms%3600000 == 0 {
			return fmt.Sprintf("every %dh", ms/3600000)
		}
		if ms%60000 == 0 {
			return fmt.Sprintf("every %dm", ms/60000)
		}
		if ms%1000 == 0 {
			return fmt.Sprintf("every %ds", ms/1000)
		}
		return fmt.Sprintf("every %dms", ms)
	case "at":
		if schedule.AtMs > 0 {
			dt := time.UnixMilli(schedule.AtMs).UTC()
			return fmt.Sprintf("at %s", dt.Format(time.RFC3339))
		}
	}
	return schedule.Kind
}

// SetContext sets the current session context for delivery
func (t *CronTool) SetContext(channel, chatID string) {
	t.channel = channel
	t.chatID = chatID
}

// Helper to convert any numeric type
func toInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case string:
		if i, err := strconv.ParseInt(n, 10, 64); err == nil {
			return i
		}
	}
	return 0
}
