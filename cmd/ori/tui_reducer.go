package main

import (
	"encoding/json"
	"fmt"
	"time"

	appcore "ori/internal/app"
	"ori/internal/llm"
	"ori/internal/runtime"
)

func (m *interactiveModel) nextBlockID(prefix string) string {
	m.nextTranscriptID++
	return fmt.Sprintf("%s-%d", prefix, m.nextTranscriptID)
}

func (m *interactiveModel) beginTranscriptPrompt(content string, ts time.Time) {
	m.transcript.appendUserBlock(m.nextBlockID("user"), content, ts)
	m.transcript.appendAssistantBlock(m.nextBlockID("assistant"), ts)
	m.active = true
	m.waiting = true
	m.responseReceived = false
	m.status = "waiting"
}

func (m *interactiveModel) appendCommandResult(input string, result *appcore.CommandResult) {
	if result == nil {
		return
	}
	if result.ResetSession || result.ClearViewport {
		m.transcript.clear()
		message := result.Text
		if message == "" {
			message = "Session reset."
		}
		m.transcript.appendSystemBlock(m.nextBlockID("system"), systemLevelInfo, message, time.Now())
		return
	}
	if result.Text == "" && result.Markdown == "" && result.Status == "" {
		return
	}
	m.transcript.appendCommandBlock(
		m.nextBlockID("command"),
		input,
		result.Text,
		result.Markdown,
		result.Status,
		time.Now(),
	)
}

func (m *interactiveModel) ensureTranscriptAssistant(ts time.Time) *assistantBlock {
	if asst := m.transcript.activeAssistant(); asst != nil {
		return asst
	}
	return m.transcript.appendAssistantBlock(m.nextBlockID("assistant"), ts)
}

func (m *interactiveModel) reduceRuntimeEvent(ev runtime.Event) bool {
	asst := m.ensureTranscriptAssistant(ev.Timestamp)
	if m.syncTerminalReducerStatus(asst) {
		return false
	}
	switch ev.Kind {
	case runtime.EventAgentStart, runtime.EventTurnStart:
		asst.setStatusIfNonTerminal(assistantStatusThinking)
		m.status = "thinking"
		return true

	case runtime.EventMessageUpdate:
		data, ok := ev.MessageUpdate()
		if !ok {
			return false
		}
		switch data.StreamEvent.Kind {
		case llm.StreamEventThinkingDelta:
			asst.appendReasoningDelta(data.StreamEvent.Delta, ev.Timestamp)
			m.status = "thinking"
			return data.StreamEvent.Delta != ""
		case llm.StreamEventTextDelta:
			asst.appendTextDelta(data.StreamEvent.Delta, ev.Timestamp)
			m.status = "responding"
			return data.StreamEvent.Delta != ""
		default:
			return false
		}

	case runtime.EventToolExecutionStart:
		data, ok := ev.ToolStart()
		if !ok {
			return false
		}
		asst.upsertToolStart(data.ToolCallID, data.ToolName, data.Args, ev.Timestamp)
		m.syncToolReducerStatus(asst)
		return true

	case runtime.EventToolExecUpdate:
		data, ok := ev.ToolUpdate()
		if !ok {
			return false
		}
		asst.updateTool(data.ToolCallID, data.ToolName, contentsToString(data.Partial), ev.Timestamp)
		m.syncToolReducerStatus(asst)
		return true

	case runtime.EventToolExecutionEnd:
		data, ok := ev.ToolEnd()
		if !ok {
			return false
		}
		asst.finishTool(data.ToolCallID, data.ToolName, contentsToString(data.Result), data.IsError, ev.Timestamp)
		m.syncToolReducerStatus(asst)
		return true

	case runtime.EventAgentEnd:
		data, ok := ev.AgentEnd()
		if !ok {
			return false
		}
		text, reasoning := appcore.ExtractFinalAssistant(data.Messages)
		return m.finalizeTranscriptAssistantAt(text, reasoning, finalSourceRuntime, ev.Timestamp)
	}
	return false
}

func (m *interactiveModel) finalizeTranscriptAssistant(content, reasoning string, source finalSource) bool {
	return m.finalizeTranscriptAssistantAt(content, reasoning, source, time.Now())
}

func (m *interactiveModel) finalizeTranscriptAssistantAt(content, reasoning string, source finalSource, ts time.Time) bool {
	asst := m.ensureTranscriptAssistant(ts)
	if asst.status == assistantStatusError || asst.status == assistantStatusCancelled {
		return false
	}
	if reasoning != "" && !assistantHasReasoningText(asst, reasoning) {
		asst.appendReasoningDelta(reasoning, ts)
	}
	asst.setFinalText(source, content, ts)
	if asst.status == assistantStatusDone {
		m.status = "done"
		m.active = false
		m.waiting = false
		m.responseReceived = true
	}
	return true
}

func (m *interactiveModel) finalizeTranscriptFromOutbound(content, reasoning string, fromAgentEventFinal bool) bool {
	asst := m.transcript.activeAssistant()
	if asst != nil {
		if asst.status == assistantStatusError || asst.status == assistantStatusCancelled {
			return false
		}
		if asst.status == assistantStatusDone && asst.finalSource == finalSourceRuntime {
			return false
		}
	}
	return m.finalizeTranscriptAssistant(content, reasoning, finalSourceFallback)
}

func (m *interactiveModel) cancelActiveAssistant() {
	if asst := m.transcript.activeAssistant(); asst != nil {
		asst.status = assistantStatusCancelled
		asst.completedAt = time.Now()
	}
	m.active = false
	m.waiting = false
	m.status = "cancelled"
}

func (m *interactiveModel) syncToolReducerStatus(asst *assistantBlock) {
	if m.syncTerminalReducerStatus(asst) {
		return
	}
	if asst.hasRunningTool() {
		asst.setStatusIfNonTerminal(assistantStatusRunningTools)
		m.status = "running tools"
		return
	}
	asst.setStatusIfNonTerminal(assistantStatusThinking)
	m.status = "thinking"
}

func (m *interactiveModel) syncTerminalReducerStatus(asst *assistantBlock) bool {
	if !isTerminalAssistantStatus(asst.status) {
		return false
	}
	m.status = transcriptStatusString(asst.status)
	return true
}

func assistantHasReasoningText(asst *assistantBlock, reasoning string) bool {
	for i := range asst.segments {
		segment := asst.segments[i]
		if segment.kind == segmentKindReasoning && segment.reasoning != nil && segment.reasoning.text == reasoning {
			return true
		}
	}
	return false
}

func transcriptStatusString(status assistantStatus) string {
	switch status {
	case assistantStatusWaiting:
		return "waiting"
	case assistantStatusThinking:
		return "thinking"
	case assistantStatusResponding:
		return "responding"
	case assistantStatusRunningTools:
		return "running tools"
	case assistantStatusDone:
		return "done"
	case assistantStatusError:
		return "error"
	case assistantStatusCancelled:
		return "cancelled"
	default:
		return ""
	}
}

func transcriptFromSessionMessages(messages []appcore.SessionMessageView, ts time.Time) transcript {
	var tr transcript
	nextID := 0
	newID := func(prefix string) string {
		nextID++
		return fmt.Sprintf("%s-%d", prefix, nextID)
	}
	var currentAssistant *assistantBlock
	toolCalls := map[string]*assistantBlock{}
	ensureAssistant := func() *assistantBlock {
		if currentAssistant == nil {
			currentAssistant = tr.appendAssistantBlock(newID("assistant"), ts)
		}
		return currentAssistant
	}

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			tr.appendUserBlock(newID("user"), msg.Content, ts)
			currentAssistant = nil
		case "assistant":
			currentAssistant = ensureAssistant()
			if msg.Reasoning != "" {
				currentAssistant.appendReasoningDelta(msg.Reasoning, ts)
			}
			if msg.Content != "" {
				currentAssistant.appendTextDelta(msg.Content, ts)
			}
			for _, call := range msg.ToolCalls {
				currentAssistant.upsertToolStart(call.ID, call.Name, sessionToolCallArgs(call), ts)
				if call.ID != "" {
					toolCalls[call.ID] = currentAssistant
				}
			}
			markSessionAssistantDone(currentAssistant, ts)
		case "tool", "tool_result", "toolResult":
			target := toolCalls[msg.ToolCallID]
			if target == nil {
				target = currentAssistant
			}
			if target == nil {
				target = ensureAssistant()
			}
			target.finishTool(msg.ToolCallID, msg.Name, msg.Content, false, ts)
			markSessionAssistantDone(target, ts)
		default:
			message := msg.Content
			if msg.Role != "" {
				message = msg.Role + ": " + message
			}
			tr.appendSystemBlock(newID("system"), systemLevelInfo, message, ts)
			currentAssistant = nil
		}
	}
	tr.activeAssistantID = ""
	return tr
}

func sessionToolCallArgs(call appcore.SessionToolCallView) map[string]any {
	if len(call.ArgumentsMap) > 0 {
		return call.ArgumentsMap
	}
	if call.Arguments == "" {
		return nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		return map[string]any{"arguments": call.Arguments}
	}
	return args
}

func markSessionAssistantDone(asst *assistantBlock, ts time.Time) {
	if asst == nil {
		return
	}
	asst.status = assistantStatusDone
	asst.completedAt = ts
}
