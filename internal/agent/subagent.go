package agent

import (
	"context"
	"sync"
)

type Subagent struct {
	ID      string
	Name    string
	Task    string
	Running bool
}

type SubagentManager struct {
	subagents map[string]*Subagent
	mu        sync.RWMutex
}

func NewSubagentManager() *SubagentManager {
	return &SubagentManager{
		subagents: make(map[string]*Subagent),
	}
}

func (sm *SubagentManager) Spawn(ctx context.Context, id, name, task string) (*Subagent, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sa := &Subagent{
		ID:      id,
		Name:    name,
		Task:    task,
		Running: true,
	}
	sm.subagents[id] = sa
	return sa, nil
}

func (sm *SubagentManager) Stop(id string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sa, ok := sm.subagents[id]; ok {
		sa.Running = false
	}
}

func (sm *SubagentManager) List() []*Subagent {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	var list []*Subagent
	for _, sa := range sm.subagents {
		list = append(list, sa)
	}
	return list
}
