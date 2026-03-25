package agent

// Agent event type constants
const (
	// LLM related events
	EventLLMThinking    = "llm_thinking"     // Agent started thinking
	EventLLMResponding  = "llm_responding"  // Agent started generating response
	EventLLMStreamChunk = "llm_stream_chunk" // Streaming content chunk received
	EventLLMStreamEnd   = "llm_stream_end"   // Streaming ended
	EventLLMToolCalls   = "llm_tool_calls"   // LLM requested tool calls
	EventLLMFinal       = "llm_final"        // Final response completed

	// Tool related events
	EventToolStart    = "tool_start"     // Tool execution started
	EventToolProgress = "tool_progress"  // Tool execution progress update
	EventToolEnd      = "tool_end"       // Tool execution completed
	EventToolError    = "tool_error"     // Tool execution failed

	// Session related events
	EventSessionStart    = "session_start"     // Session started
	EventSessionEnd      = "session_end"       // Session ended
	EventCommandReceived = "command_received"  // Command received (e.g., /help, /stop)
)
