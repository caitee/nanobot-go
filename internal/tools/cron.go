package tools

import "context"

type CronTool struct{}

func NewCronTool() *CronTool { return &CronTool{} }
func (t *CronTool) Name() string   { return "cron" }
func (t *CronTool) Description() string { return "Schedule cron jobs" }
func (t *CronTool) Parameters() map[string]any { return map[string]any{} }
func (t *CronTool) Execute(ctx context.Context, params map[string]any) (any, error) {
	return "cron not implemented", nil
}
