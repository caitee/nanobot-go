package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"ori/internal/llm"
	"ori/internal/tool"
)

// loopConfig is the internal, fully-populated configuration used by the
// low-level runAgentLoop / runAgentLoopContinue functions. It is assembled by
// the Agent wrapper before each run.
type loopConfig struct {
	model            llm.Model
	thinkingLevel    string
	temperature      float64
	maxTokens        int
	sessionID        string
	tools            []tool.AgentTool
	toolIndex        map[string]tool.AgentTool
	toolExecution    tool.ExecutionMode
	streamFn         llm.StreamFn
	convertToLLM     ConvertToLLM
	transformContext TransformContext
	getAPIKey        GetAPIKey
	shouldStopAfter  ShouldStopAfter
	beforeToolCall   BeforeToolCall
	afterToolCall    AfterToolCall

	getSteeringMessages func() []AgentMessage
	getFollowUpMessages func() []AgentMessage

	sink EventSink
}

// runAgentLoop drives a fresh prompt: appends `prompts` to the transcript,
// then iterates turn / tool_batch / steering until the model stops and no
// follow-ups remain. Returns the newly appended messages.
func runAgentLoop(
	ctx context.Context,
	prompts []AgentMessage,
	initialMessages []AgentMessage,
	cfg loopConfig,
) ([]AgentMessage, error) {
	cfg.emit(Event{Kind: EventAgentStart, Timestamp: time.Now()})

	transcript := append([]AgentMessage{}, initialMessages...)
	newMessages := make([]AgentMessage, 0, len(prompts)+4)

	for _, p := range prompts {
		transcript = append(transcript, p)
		newMessages = append(newMessages, p)
		cfg.emit(Event{Kind: EventMessageStart, Timestamp: time.Now(), Data: MessageStartData{Message: p}})
		cfg.emit(Event{Kind: EventMessageEnd, Timestamp: time.Now(), Data: MessageEndData{Message: p}})
	}

	return runTurnLoop(ctx, transcript, newMessages, cfg)
}

// runAgentLoopContinue resumes from an existing transcript. The final message
// is expected to be a user or tool-result message.
func runAgentLoopContinue(
	ctx context.Context,
	initialMessages []AgentMessage,
	cfg loopConfig,
) ([]AgentMessage, error) {
	cfg.emit(Event{Kind: EventAgentStart, Timestamp: time.Now()})
	return runTurnLoop(ctx, append([]AgentMessage{}, initialMessages...), nil, cfg)
}

// runTurnLoop is the shared core driving turns, tool batches, steering, and
// follow-ups. It is the Go translation of pi-mono's agent-loop.ts:155-246.
func runTurnLoop(
	ctx context.Context,
	transcript []AgentMessage,
	newMessages []AgentMessage,
	cfg loopConfig,
) ([]AgentMessage, error) {
	for {
		// Inner loop: turns + steering drain between turns.
		for {
			if err := ctx.Err(); err != nil {
				cfg.emitAgentEnd(newMessages)
				return newMessages, err
			}

			cfg.emit(Event{Kind: EventTurnStart, Timestamp: time.Now()})

			assistant, toolResults, appended, err := runSingleTurn(ctx, transcript, cfg)
			if err != nil {
				// runSingleTurn may have returned a synthetic aborted
				// assistant in `appended` even though err is non-nil. Fold
				// it into the run's newMessages so the persister / caller
				// can still see what was streamed up to the cancel point.
				if len(appended) > 0 {
					transcript = append(transcript, appended...)
					newMessages = append(newMessages, appended...)
					cfg.emit(Event{
						Kind:      EventTurnEnd,
						Timestamp: time.Now(),
						Data: TurnEndData{
							Assistant:   assistant,
							ToolResults: toolResults,
						},
					})
				}
				// The provider / tool subsystem couldn't even encode the failure
				// as a stream; bubble up so the Agent wrapper can synthesize a
				// terminal assistant message.
				cfg.emitAgentEnd(newMessages)
				return newMessages, err
			}

			transcript = append(transcript, appended...)
			newMessages = append(newMessages, appended...)

			cfg.emit(Event{
				Kind:      EventTurnEnd,
				Timestamp: time.Now(),
				Data: TurnEndData{
					Assistant:   assistant,
					ToolResults: toolResults,
				},
			})

			// Terminal reasons: the model is done or errored out. No more LLM calls.
			if assistant.StopReason != llm.StopReasonToolUse {
				goto afterInner
			}

			// Otherwise the model wanted tools and we've already executed them;
			// decide whether to keep going.
			if cfg.shouldStopAfter != nil {
				stop, err := cfg.shouldStopAfter(ctx, ShouldStopContext{
					Assistant:   assistant,
					ToolResults: toolResults,
					Transcript:  transcript,
					NewMessages: newMessages,
				})
				if err != nil {
					cfg.emitAgentEnd(newMessages)
					return newMessages, mapRuntimeError(err, "shouldStopAfter")
				}
				if stop {
					goto afterInner
				}
			}

			// Drain steering messages (mid-run injections).
			if cfg.getSteeringMessages != nil {
				steering := cfg.getSteeringMessages()
				for _, m := range steering {
					transcript = append(transcript, m)
					newMessages = append(newMessages, m)
					cfg.emit(Event{Kind: EventMessageStart, Timestamp: time.Now(), Data: MessageStartData{Message: m}})
					cfg.emit(Event{Kind: EventMessageEnd, Timestamp: time.Now(), Data: MessageEndData{Message: m}})
				}
			}

			// If a tool batch signalled early termination, stop regardless.
			if earlyTerminate(toolResults) {
				goto afterInner
			}
		}

	afterInner:
		// Outer loop polls follow-up messages. If there are any, append them
		// and start a fresh inner loop; otherwise we're done.
		if cfg.getFollowUpMessages == nil {
			break
		}
		followUps := cfg.getFollowUpMessages()
		if len(followUps) == 0 {
			break
		}
		for _, m := range followUps {
			transcript = append(transcript, m)
			newMessages = append(newMessages, m)
			cfg.emit(Event{Kind: EventMessageStart, Timestamp: time.Now(), Data: MessageStartData{Message: m}})
			cfg.emit(Event{Kind: EventMessageEnd, Timestamp: time.Now(), Data: MessageEndData{Message: m}})
		}
	}

	cfg.emitAgentEnd(newMessages)
	return newMessages, nil
}

// runSingleTurn handles one LLM call + one tool batch. It returns the final
// assistant message, the tool results it produced, and the list of messages
// to append to the transcript (assistant first, then tool-result messages in
// source order).
func runSingleTurn(
	ctx context.Context,
	transcript []AgentMessage,
	cfg loopConfig,
) (llm.AssistantMessage, []llm.ToolResultMessage, []AgentMessage, error) {
	// 1. transformContext → convertToLlm
	prepared := transcript
	if cfg.transformContext != nil {
		out, err := cfg.transformContext(ctx, cloneMessages(prepared))
		if err != nil {
			return llm.AssistantMessage{}, nil, nil, mapRuntimeError(err, "transformContext")
		}
		prepared = out
	}

	llmMessages, err := cfg.convertToLLM(prepared)
	if err != nil {
		return llm.AssistantMessage{}, nil, nil, mapRuntimeError(err, "convertToLLM")
	}

	// 2. Resolve API key if needed.
	apiKey := ""
	if cfg.getAPIKey != nil {
		apiKey, err = cfg.getAPIKey(ctx, cfg.model.Provider)
		if err != nil {
			return llm.AssistantMessage{}, nil, nil, mapGetAPIKeyError(err, cfg.model.Provider)
		}
	}

	// 3. Run the stream and consume events. The stream function itself must
	//    not throw; failures come back as terminal StreamEventError.
	streamOpts := llm.StreamOptions{
		Reasoning:   cfg.thinkingLevel,
		SessionID:   cfg.sessionID,
		APIKey:      apiKey,
		Temperature: cfg.temperature,
		MaxTokens:   cfg.maxTokens,
	}
	llmCtx := llm.Context{
		SystemPrompt: "",
		Messages:     llmMessages,
		Tools:        toolDefs(cfg.tools),
	}
	// Extract the system prompt from the wrapped context if the caller chose
	// to express it as a system message. We accept it as the convention that
	// convertToLLM may emit a system message or leave SystemPrompt empty.
	stream := cfg.streamFn(ctx, cfg.model, llmCtx, streamOpts)

	assistant, err := consumeStream(ctx, stream, cfg)
	if err != nil {
		// consumeStream marks an aborted partial with StopReasonAborted and
		// returns whatever was streamed up to the cancel point. Emit it as a
		// real assistant message so downstream persistence / UI sees the
		// truncated turn rather than silently losing the work.
		if assistant.StopReason == llm.StopReasonAborted {
			assistantMsg := WrapLLM(assistant)
			cfg.emit(Event{Kind: EventMessageStart, Timestamp: time.Now(), Data: MessageStartData{Message: assistantMsg}})
			cfg.emit(Event{Kind: EventMessageEnd, Timestamp: time.Now(), Data: MessageEndData{Message: assistantMsg}})
			return assistant, nil, []AgentMessage{assistantMsg}, err
		}
		return llm.AssistantMessage{}, nil, nil, err
	}

	// 4. Record the assistant message.
	assistantMsg := WrapLLM(assistant)
	appended := []AgentMessage{assistantMsg}
	cfg.emit(Event{Kind: EventMessageStart, Timestamp: time.Now(), Data: MessageStartData{Message: assistantMsg}})
	cfg.emit(Event{Kind: EventMessageEnd, Timestamp: time.Now(), Data: MessageEndData{Message: assistantMsg}})

	// 5. If the model requested tools, execute the batch.
	if assistant.StopReason != llm.StopReasonToolUse {
		return assistant, nil, appended, nil
	}

	toolResults, err := executeToolBatch(ctx, assistant, cfg)
	if err != nil {
		return assistant, nil, appended, err
	}

	for _, tr := range toolResults {
		m := WrapLLM(tr)
		appended = append(appended, m)
		cfg.emit(Event{Kind: EventMessageStart, Timestamp: time.Now(), Data: MessageStartData{Message: m}})
		cfg.emit(Event{Kind: EventMessageEnd, Timestamp: time.Now(), Data: MessageEndData{Message: m}})
	}

	return assistant, toolResults, appended, nil
}

// consumeStream accumulates StreamEvent values into an AssistantMessage while
// emitting message_update events for UI consumers.
func consumeStream(
	ctx context.Context,
	stream llm.EventStream,
	cfg loopConfig,
) (llm.AssistantMessage, error) {
	var partial llm.AssistantMessage
	partial.Provider = cfg.model.Provider
	partial.API = cfg.model.API
	partial.Model = cfg.model.ID
	partial.Timestamp = time.Now()

	for {
		select {
		case <-ctx.Done():
			// Mark the partial as aborted so callers that want to persist
			// whatever was streamed so far (text/thinking blocks, in-progress
			// tool calls) can distinguish "user hit ctrl+c" from a provider
			// error. pi-mono does the same in agent.ts handleRunFailure.
			err := ctx.Err()
			partial.StopReason = llm.StopReasonAborted
			if err != nil {
				partial.ErrorMessage = err.Error()
			}
			return partial, err
		case ev, ok := <-stream:
			if !ok {
				// Channel closed without a terminal event — treat as error.
				partial.StopReason = llm.StopReasonError
				partial.ErrorMessage = "stream closed without terminal event"
				return partial, nil
			}

			if ev.Partial != nil {
				partial = *ev.Partial
			}

			cfg.emit(Event{
				Kind:      EventMessageUpdate,
				Timestamp: time.Now(),
				Data: MessageUpdateData{
					Partial:     WrapLLM(partial),
					StreamEvent: ev,
				},
			})

			switch ev.Kind {
			case llm.StreamEventDone:
				if ev.Message != nil {
					return *ev.Message, nil
				}
				partial.StopReason = ev.StopReason
				return partial, nil
			case llm.StreamEventError:
				if ev.Message != nil {
					return *ev.Message, nil
				}
				partial.StopReason = llm.StopReasonError
				partial.ErrorMessage = ev.ErrorMessage
				// Map provider errors to structured errors
				return partial, mapProviderError(nil, ev.ErrorMessage)
			}
		}
	}
}

// executeToolBatch runs the tool calls referenced by the assistant message
// according to the configured execution mode. It always returns tool-result
// messages in the same order as the tool calls on the assistant message.
func executeToolBatch(
	ctx context.Context,
	assistant llm.AssistantMessage,
	cfg loopConfig,
) ([]llm.ToolResultMessage, error) {
	calls := collectToolCalls(assistant)
	if len(calls) == 0 {
		return nil, nil
	}

	// Prepare per-call info (looked-up tool + prepared args + optional block).
	type prep struct {
		call    llm.ToolCallContent
		tool    tool.AgentTool
		args    map[string]any
		isError bool
		content []llm.Content
	}
	preps := make([]prep, len(calls))

	for i, c := range calls {
		tl := lookupTool(cfg, c.Name)
		if tl == nil {
			preps[i] = prep{
				call:    c,
				isError: true,
				content: []llm.Content{llm.TextContent{Text: fmt.Sprintf("tool not found: %s", c.Name)}},
			}
			continue
		}

		args := c.Arguments
		prepared, err := tl.PrepareArguments(args)
		if err != nil {
			preps[i] = prep{
				call:    c,
				tool:    tl,
				isError: true,
				content: []llm.Content{llm.TextContent{Text: fmt.Sprintf("argument preparation failed: %v", err)}},
			}
			continue
		}

		// beforeToolCall hook.
		if cfg.beforeToolCall != nil {
			res, err := cfg.beforeToolCall(ctx, BeforeToolCallContext{
				Assistant: assistant,
				ToolCall:  c,
				Args:      prepared,
			})
			if err != nil {
				preps[i] = prep{
					call:    c,
					tool:    tl,
					args:    prepared,
					isError: true,
					content: []llm.Content{llm.TextContent{Text: fmt.Sprintf("beforeToolCall error: %v", err)}},
				}
				continue
			}
			if res != nil && res.Block {
				reason := res.Reason
				if reason == "" {
					reason = "tool call blocked"
				}
				preps[i] = prep{
					call:    c,
					tool:    tl,
					args:    prepared,
					isError: true,
					content: []llm.Content{llm.TextContent{Text: reason}},
				}
				continue
			}
		}

		preps[i] = prep{call: c, tool: tl, args: prepared}
	}

	// Execute.
	results := make([]llm.ToolResultMessage, len(preps))

	execOne := func(i int) {
		p := preps[i]

		// Pre-resolved error (tool missing / validation / hook block).
		if p.isError {
			results[i] = llm.ToolResultMessage{
				ToolCallID: p.call.ID,
				ToolName:   p.call.Name,
				Content:    p.content,
				IsError:    true,
				Timestamp:  time.Now(),
			}
			cfg.emit(Event{Kind: EventToolExecutionStart, Timestamp: time.Now(), Data: ToolStartData{
				ToolCallID: p.call.ID, ToolName: p.call.Name, Args: p.args,
			}})
			cfg.emit(Event{Kind: EventToolExecutionEnd, Timestamp: time.Now(), Data: ToolEndData{
				ToolCallID: p.call.ID, ToolName: p.call.Name, Result: p.content, IsError: true,
			}})
			return
		}

		cfg.emit(Event{Kind: EventToolExecutionStart, Timestamp: time.Now(), Data: ToolStartData{
			ToolCallID: p.call.ID, ToolName: p.call.Name, Args: p.args,
		}})

		update := func(partial tool.Result) {
			cfg.emit(Event{Kind: EventToolExecUpdate, Timestamp: time.Now(), Data: ToolUpdateData{
				ToolCallID: p.call.ID, ToolName: p.call.Name, Args: p.args, Partial: partial,
			}})
		}

		res, err := p.tool.Execute(ctx, p.call.ID, p.args, update)

		var resultMsg llm.ToolResultMessage
		var isError bool

		if err != nil {
			isError = true
			resultMsg = llm.ToolResultMessage{
				ToolCallID: p.call.ID,
				ToolName:   p.call.Name,
				Content:    []llm.Content{llm.TextContent{Text: err.Error()}},
				IsError:    true,
				Timestamp:  time.Now(),
			}
		} else {
			if res == nil {
				res = &tool.Result{}
			}
			resultMsg = llm.ToolResultMessage{
				ToolCallID: p.call.ID,
				ToolName:   p.call.Name,
				Content:    res.Content,
				Details:    res.Details,
				IsError:    false,
				Terminate:  res.Terminate,
				Timestamp:  time.Now(),
			}
		}

		// afterToolCall hook.
		if cfg.afterToolCall != nil {
			hookRes, hookErr := cfg.afterToolCall(ctx, AfterToolCallContext{
				Assistant: assistant,
				ToolCall:  p.call,
				Args:      p.args,
				Result: &tool.Result{
					Content:   resultMsg.Content,
					Details:   resultMsg.Details,
					Terminate: resultMsg.Terminate,
				},
				IsError: isError,
			})
			if hookErr == nil && hookRes != nil {
				if hookRes.Content != nil {
					resultMsg.Content = hookRes.Content
				}
				if hookRes.Details != nil {
					resultMsg.Details = hookRes.Details
				}
				if hookRes.IsError != nil {
					resultMsg.IsError = *hookRes.IsError
					isError = *hookRes.IsError
				}
				if hookRes.Terminate != nil {
					resultMsg.Terminate = *hookRes.Terminate
				}
			}
		}

		results[i] = resultMsg

		cfg.emit(Event{Kind: EventToolExecutionEnd, Timestamp: time.Now(), Data: ToolEndData{
			ToolCallID: p.call.ID, ToolName: p.call.Name, Result: resultMsg.Content, IsError: isError,
		}})
	}

	if cfg.toolExecution == tool.ExecutionParallel {
		var wg sync.WaitGroup
		for i := range preps {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				execOne(idx)
			}(i)
		}
		wg.Wait()
	} else {
		for i := range preps {
			execOne(i)
		}
	}

	return results, nil
}

func resultTerminate(r *tool.Result) bool {
	if r == nil {
		return false
	}
	return r.Terminate
}

// collectToolCalls extracts the tool_call blocks from an assistant message.
func collectToolCalls(m llm.AssistantMessage) []llm.ToolCallContent {
	var out []llm.ToolCallContent
	for _, block := range m.Content {
		if tc, ok := block.(llm.ToolCallContent); ok {
			out = append(out, tc)
		}
	}
	return out
}

func lookupTool(cfg loopConfig, name string) tool.AgentTool {
	if t, ok := cfg.toolIndex[name]; ok {
		return t
	}
	return nil
}

// earlyTerminate reports whether every tool result in the batch is flagged
// terminate and there is at least one. Matches pi-mono's semantics: the hint
// only takes effect when all tools in the batch agree.
func earlyTerminate(trs []llm.ToolResultMessage) bool {
	if len(trs) == 0 {
		return false
	}
	for _, tr := range trs {
		if !tr.Terminate {
			return false
		}
	}
	return true
}

// toolDefs converts the runtime's AgentTool list to the slim llm.Tool defs
// that are visible to the model.
func toolDefs(tools []tool.AgentTool) []llm.Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]llm.Tool, 0, len(tools))
	for _, t := range tools {
		out = append(out, tool.Definition(t))
	}
	return out
}

func (cfg loopConfig) emit(e Event) {
	if cfg.sink != nil {
		if e.SessionID == "" {
			e.SessionID = cfg.sessionID
		}
		cfg.sink(e)
	}
}

func (cfg loopConfig) emitAgentEnd(newMessages []AgentMessage) {
	cfg.emit(Event{
		Kind:      EventAgentEnd,
		Timestamp: time.Now(),
		Data:      AgentEndData{Messages: newMessages},
	})
}
