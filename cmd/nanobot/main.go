package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"nanobot-go/internal/agent"
	"nanobot-go/internal/bus"
	"nanobot-go/internal/channels"
	"nanobot-go/internal/config"
	"nanobot-go/internal/cron"
	"nanobot-go/internal/providers"
	"nanobot-go/internal/session"
	"nanobot-go/internal/tools"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	version = "0.1.0-go"
	logo    = `
    _   _
   | | | |
  / __/ __|
  \__ \__ \
  (   (   )
   |_| |_|
  AI Assistant
`
)

// Lipgloss styles for enhanced TUI
var (
	spinnerStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("86")).Bold(true)

	userPromptStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("75")).Bold(true)

	assistantLabelStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("130")).Bold(true)

	toolEntryStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	toolRunningStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("75"))

	toolDoneStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("76"))

	toolErrorStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("196"))

	toolIconStyle = lipgloss.NewStyle().
		Bold(true)

	toolArgsStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Italic(true)

	toolDurationStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("245"))

	contentStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("white"))

	streamingCursorStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("86")).
		Bold(true)

	inputStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	borderStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))
)

// ============================================================================
// Root Command
// ============================================================================

var rootCmd = &cobra.Command{
	Use:   "nanobot",
	Short: "Nanobot AI Assistant",
	Long:  fmt.Sprintf("%s nanobot - Personal AI Assistant", logo),
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("%s nanobot v%s\n", logo, version)
	},
}

// ============================================================================
// Onboard Command
// ============================================================================

var onboardCmd = &cobra.Command{
	Use:   "onboard",
	Short: "Initialize nanobot configuration and workspace",
	Run:   runOnboard,
}

var onboardWizardFlag bool
var onboardWorkspaceFlag string
var onboardConfigFlag string

func init() {
	onboardCmd.Flags().BoolVarP(&onboardWizardFlag, "wizard", "w", false, "Use interactive configuration wizard")
	onboardCmd.Flags().StringVarP(&onboardWorkspaceFlag, "workspace", "", "", "Workspace directory")
	onboardCmd.Flags().StringVarP(&onboardConfigFlag, "config", "c", "", "Path to config file")
}

// ============================================================================
// Agent Command
// ============================================================================

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Interact with the agent",
	Run:   runAgent,
}

var (
	agentMessageFlag    string
	agentSessionFlag    string
	agentWorkspaceFlag  string
	agentConfigFlag     string
	agentMarkdownFlag   bool
	agentLogsFlag       bool
)

func init() {
	agentCmd.Flags().StringVarP(&agentMessageFlag, "message", "m", "", "Message to send to the agent")
	agentCmd.Flags().StringVarP(&agentSessionFlag, "session", "s", "cli:direct", "Session ID")
	agentCmd.Flags().StringVarP(&agentWorkspaceFlag, "workspace", "w", "", "Workspace directory")
	agentCmd.Flags().StringVarP(&agentConfigFlag, "config", "c", "", "Config file path")
	agentCmd.Flags().BoolVarP(&agentMarkdownFlag, "markdown", "", true, "Render assistant output as Markdown")
	agentCmd.Flags().BoolVarP(&agentLogsFlag, "logs", "", false, "Show nanobot runtime logs during chat")
}

// ============================================================================
// Gateway Command
// ============================================================================

var gatewayCmd = &cobra.Command{
	Use:   "gateway",
	Short: "Start the nanobot gateway server",
	Run:   runGateway,
}

var (
	gatewayPortFlag     int
	gatewayWorkspaceFlag string
	gatewayVerboseFlag   bool
	gatewayConfigFlag    string
)

func init() {
	gatewayCmd.Flags().IntVarP(&gatewayPortFlag, "port", "p", 0, "Gateway port")
	gatewayCmd.Flags().StringVarP(&gatewayWorkspaceFlag, "workspace", "w", "", "Workspace directory")
	gatewayCmd.Flags().BoolVarP(&gatewayVerboseFlag, "verbose", "v", false, "Verbose output")
	gatewayCmd.Flags().StringVarP(&gatewayConfigFlag, "config", "c", "", "Config file path")
}

// ============================================================================
// Status Command
// ============================================================================

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show nanobot status",
	Run:   runStatus,
}

var statusConfigFlag string

func init() {
	statusCmd.Flags().StringVarP(&statusConfigFlag, "config", "c", "", "Config file path")
}

// ============================================================================
// Channels Command
// ============================================================================

var channelsCmd = &cobra.Command{
	Use:   "channels",
	Short: "Manage channels",
}

var channelsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show channel status",
	Run:   runChannelsStatus,
}

var channelsLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Login to a channel",
	Run:   runChannelsLogin,
}

func init() {
	channelsCmd.AddCommand(channelsStatusCmd)
	channelsCmd.AddCommand(channelsLoginCmd)
}

// ============================================================================
// Main Entry Point
// ============================================================================

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	})))

	rootCmd.AddCommand(onboardCmd)
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(gatewayCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(channelsCmd)
	rootCmd.AddCommand(versionCmd)

	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// ============================================================================
// Onboard Implementation
// ============================================================================

func runOnboard(cmd *cobra.Command, args []string) {
	configPath := onboardConfigFlag
	if configPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting home directory: %v\n", err)
			os.Exit(1)
		}
		configPath = filepath.Join(homeDir, ".nanobot", "config.json")
	}

	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating config directory: %v\n", err)
		os.Exit(1)
	}

	// Run wizard if requested
	if onboardWizardFlag {
		_, saved, err := RunWizard()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error running wizard: %v\n", err)
			os.Exit(1)
		}
		if !saved {
			fmt.Println("Wizard cancelled, no changes made.")
			return
		}
		fmt.Println()
		fmt.Printf("Configuration saved to %s\n", configPath)
		fmt.Println()
		printOnboardNextSteps(configPath)
		return
	}

	// Non-interactive mode: check if config exists
	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Config already exists at %s\n", configPath)
		fmt.Println("Use --wizard flag for interactive configuration, or manually edit the config file.")
		return
	}

	// Create default configuration
	cfg := config.Config{
		Agents: config.AgentDefaults{
			Model:               "claude-opus-4-5",
			Provider:            "auto",
			MaxTokens:           8192,
			ContextWindowTokens: 65536,
			Temperature:         0.1,
			MaxToolIterations:   40,
		},
		Gateway: config.GatewayConfig{
			Host: "0.0.0.0",
			Port: 18790,
		},
		Tools: config.ToolsConfig{
			Web: config.WebConfig{
				SearchProvider: "brave",
			},
		},
		Channels: config.ChannelsConfig{
			SendProgress:  true,
			SendToolHints: true,
		},
	}

	// Apply workspace override if specified
	if onboardWorkspaceFlag != "" {
		cfg.Agents.Workspace = onboardWorkspaceFlag
	}

	// Save config
	file, err := os.Create(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating config file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing config file: %v\n", err)
		os.Exit(1)
	}

	// Create workspace directory
	workspace := cfg.Agents.Workspace
	if workspace == "" {
		homeDir, _ := os.UserHomeDir()
		workspace = filepath.Join(homeDir, ".nanobot", "workspace")
	}
	if err := os.MkdirAll(workspace, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating workspace: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Printf("Created config at %s\n", configPath)
	fmt.Printf("Created workspace at %s\n", workspace)
	fmt.Println()
	printOnboardNextSteps(configPath)
}

func printOnboardNextSteps(configPath string) {
	fmt.Println("Next steps:")
	fmt.Printf("  1. Add your API key to %s\n", configPath)
	fmt.Println("     Get one at: https://openrouter.ai/keys")
	fmt.Printf("  2. Chat: ./nanobot agent -m \"Hello!\"\n")
	fmt.Println()
	fmt.Println("Want Telegram/WhatsApp? See: https://github.com/HKUDS/nanobot#-chat-apps")
}

// ============================================================================
// Agent Implementation
// ============================================================================

func runAgent(cmd *cobra.Command, args []string) {
	ctx := context.Background()
	cfg, err := config.Load(agentConfigFlag)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Override workspace if specified
	workspace := agentWorkspaceFlag
	if workspace == "" {
		workspace = cfg.Agents.Workspace
	}
	if workspace == "" {
		homeDir, _ := os.UserHomeDir()
		workspace = filepath.Join(homeDir, ".nanobot", "workspace")
	}

	// Create session store
	sessionStore, err := session.NewFileSessionStore("sessions")
	if err != nil {
		slog.Error("failed to create session store", "error", err)
		os.Exit(1)
	}

	// Create tool registry
	toolRegistry := tools.NewRegistry()
	toolRegistry.Register(tools.NewMessageTool())
	toolRegistry.Register(tools.NewFilesystemTool(nil))
	toolRegistry.Register(tools.NewShellTool(true, nil, nil))
	toolRegistry.Register(tools.NewWebTool())
	toolRegistry.Register(tools.NewCronTool(nil, nil))
	toolRegistry.Register(tools.NewSpawnTool(nil))

	// Create provider based on config
	provider := createProvider(cfg)

	// Create agent loop
	maxIterations := cfg.Agents.MaxToolIterations
	if maxIterations <= 0 {
		maxIterations = 40
	}
	agentLoop := agent.NewAgentLoop(nil, sessionStore, toolRegistry, provider, maxIterations, false)

	if agentMessageFlag != "" {
		// Single message mode
		runAgentSingle(ctx, agentLoop, sessionStore, toolRegistry, provider, maxIterations)
	} else {
		// Interactive mode
		runAgentInteractive(ctx, agentLoop, sessionStore, toolRegistry, provider, maxIterations)
	}
}

func createProvider(cfg *config.Config) providers.LLMProvider {
	model := cfg.Agents.Model
	providerName := cfg.Agents.Provider

	// Try to get API key from providers config
	var apiKey string
	var apiBase string

	switch providerName {
	case "openai":
		if cfg.Providers.OpenAI != nil {
			if k, ok := cfg.Providers.OpenAI["api_key"].(string); ok {
				apiKey = k
			}
			if b, ok := cfg.Providers.OpenAI["api_base"].(string); ok {
				apiBase = b
			}
		}
		if apiBase == "" {
			apiBase = "https://api.openai.com/v1"
		}
		return providers.NewOpenAIProvider(apiBase, apiKey, model)

	case "openrouter":
		if cfg.Providers.OpenRouter != nil {
			if k, ok := cfg.Providers.OpenRouter["api_key"].(string); ok {
				apiKey = k
			}
		}
		return providers.NewOpenRouterProvider(apiKey, model)

	case "anthropic":
		if cfg.Providers.Anthropic != nil {
			if k, ok := cfg.Providers.Anthropic["api_key"].(string); ok {
				apiKey = k
			}
		}
		// Anthropic uses OpenAI-compatible API
		if apiBase == "" {
			apiBase = "https://api.anthropic.com/v1"
		}
		return providers.NewOpenAIProvider(apiBase, apiKey, model)

	case "azure":
		if cfg.Providers.Azure != nil {
			if k, ok := cfg.Providers.Azure["api_key"].(string); ok {
				apiKey = k
			}
			if b, ok := cfg.Providers.Azure["api_base"].(string); ok {
				apiBase = b
			}
		}
		return providers.NewOpenAIProvider(apiBase, apiKey, model)

	case "minimax":
		if cfg.Providers.Minimax != nil {
			if k, ok := cfg.Providers.Minimax["api_key"].(string); ok {
				apiKey = k
			}
			if b, ok := cfg.Providers.Minimax["api_base"].(string); ok {
				apiBase = b
			}
		}
		return providers.NewMinimaxProvider(apiKey, apiBase, model)

	default:
		// Default to OpenRouter for "auto" or unknown
		if cfg.Providers.OpenRouter != nil {
			if k, ok := cfg.Providers.OpenRouter["api_key"].(string); ok {
				apiKey = k
			}
		}
		if apiKey == "" {
			apiKey = os.Getenv("OPENROUTER_API_KEY")
		}
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
		return providers.NewOpenRouterProvider(apiKey, model)
	}
}

func runAgentSingle(ctx context.Context, agentLoop *agent.AgentLoop, sessionStore session.SessionStore, toolRegistry tools.ToolRegistry, provider providers.LLMProvider, maxIterations int) {
	messageBus := bus.New(100)
	agentLoop = agent.NewAgentLoop(messageBus, sessionStore, toolRegistry, provider, maxIterations, false)

	// Parse session
	sessionKey := agentSessionFlag
	var chatID string
	if idx := strings.Index(sessionKey, ":"); idx != -1 {
		chatID = sessionKey[idx+1:]
	} else {
		chatID = sessionKey
	}

	inbound := bus.InboundMessage{
		Channel:    "cli",
		SenderID:   "user",
		ChatID:     chatID,
		Content:    agentMessageFlag,
		SessionKey: sessionKey,
	}
	// Start agent loop in background
	go func() {
		if err := agentLoop.Start(ctx); err != nil && err != context.Canceled {
			slog.Error("agent loop error", "error", err)
		}
	}()
	// Small delay to let goroutine start

	messageBus.PublishInbound(inbound)

	// Wait for response
	for msg := range messageBus.ConsumeOutbound() {
		fmt.Println()
		fmt.Println(assistantLabelStyle.Render("nanobot:"))
		fmt.Println()
		fmt.Println(msg.Content)
		break
	}
}

func runAgentInteractive(ctx context.Context, agentLoop *agent.AgentLoop, sessionStore session.SessionStore, toolRegistry tools.ToolRegistry, provider providers.LLMProvider, maxIterations int) {
	messageBus := bus.New(100)
	agentLoop = agent.NewAgentLoop(messageBus, sessionStore, toolRegistry, provider, maxIterations, false)

	// Parse session
	sessionKey := agentSessionFlag
	var chatID string
	if idx := strings.Index(sessionKey, ":"); idx != -1 {
		chatID = sessionKey[idx+1:]
	} else {
		chatID = sessionKey
	}

	fmt.Printf("%s Interactive mode (type 'exit' or Ctrl+C to quit)\n\n", logo)

	// Start agent loop in background
	go func() {
		if err := agentLoop.Start(ctx); err != nil && err != context.Canceled {
			slog.Error("agent loop error", "error", err)
		}
	}()

	// Create text input for line editing
	model := newInteractiveModel(messageBus, sessionKey, chatID)
	p := tea.NewProgram(model, tea.WithoutSignals())
	if _, err := p.Run(); err != nil {
		slog.Error("interactive mode error", "error", err)
	}
}

type interactiveModel struct {
	textInput     textinput.Model
	messageBus    bus.MessageBus
	sessionKey     string
	chatID        string
	waiting       bool
	messages      []conversationEntry
	quitting      bool
	done          chan struct{}
	mu            sync.Mutex
	spinnerIdx    int
	toolEventCh   <-chan bus.ToolEvent
	agentEventCh  <-chan bus.AgentEvent
	outboundCh    <-chan bus.OutboundMessage
}

type toolCallEntry struct {
	name         string
	args         string
	status       string // "pending" | "running" | "done" | "error"
	result       string
	durationMs   int64
	expanded     bool
}

type conversationEntry struct {
	role          string
	content       string
	toolCalls     []toolCallEntry
	isLoading     bool
	streamingText string
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type spinnerTickMsg struct{}

type responseMsg struct {
	content string
}

type toolEventMsg struct {
	ev bus.ToolEvent
}

type agentEventMsg struct {
	ev bus.AgentEvent
}

type pollTickMsg struct{}

func newInteractiveModel(messageBus bus.MessageBus, sessionKey, chatID string) *interactiveModel {
	ti := textinput.New()
	ti.Placeholder = "Type a message..."
	ti.Focus()
	ti.Prompt = "You: "

	return &interactiveModel{
		textInput:    ti,
		messageBus:   messageBus,
		sessionKey:   sessionKey,
		chatID:       chatID,
		done:         make(chan struct{}),
		toolEventCh:  messageBus.SubscribeToolEvents(),
		agentEventCh: messageBus.SubscribeAgentEvents(),
		outboundCh:   messageBus.ConsumeOutbound(),
	}
}

func (m *interactiveModel) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.tickSpinner(), m.pollEvents())
}

func (m *interactiveModel) pollEvents() tea.Cmd {
	return func() tea.Msg {
		select {
		case ev := <-m.toolEventCh:
			return toolEventMsg{ev: ev}
		case ev := <-m.agentEventCh:
			return agentEventMsg{ev: ev}
		case resp, ok := <-m.outboundCh:
			if !ok {
				return nil
			}
			return responseMsg{content: resp.Content}
		case <-m.done:
			return nil
		case <-time.After(50 * time.Millisecond):
			return pollTickMsg{}
		}
	}
}

func (m *interactiveModel) tickSpinner() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(time.Second / 10)
		return spinnerTickMsg{}
	}
}

func (m *interactiveModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pollTickMsg:
		// Keep polling for events
		return m, m.pollEvents()

	case spinnerTickMsg:
		m.mu.Lock()
		if m.waiting {
			m.spinnerIdx = (m.spinnerIdx + 1) % len(spinnerFrames)
		}
		m.mu.Unlock()
		return m, m.tickSpinner()

	case responseMsg:
		m.mu.Lock()
		m.waiting = false
		for i := len(m.messages) - 1; i >= 0; i-- {
			if m.messages[i].role == "assistant" && m.messages[i].isLoading {
				m.messages[i].content = msg.content
				m.messages[i].isLoading = false
				break
			}
		}
		m.mu.Unlock()
		return m, m.pollEvents()

	case toolEventMsg:
		m.mu.Lock()
		if msg.ev.SessionKey == m.sessionKey {
			for i := len(m.messages) - 1; i >= 0; i-- {
				if m.messages[i].role == "assistant" && m.messages[i].isLoading {
					entry := &m.messages[i]
					switch msg.ev.Type {
					case "tool_start":
						entry.toolCalls = append(entry.toolCalls, toolCallEntry{
							name:   msg.ev.ToolName,
							args:   msg.ev.Args,
							status: "running",
						})
					case "tool_end":
						for j := range entry.toolCalls {
							if entry.toolCalls[j].name == msg.ev.ToolName {
								entry.toolCalls[j].status = "done"
								entry.toolCalls[j].result = msg.ev.Result
								break
							}
						}
					case "tool_error":
						for j := range entry.toolCalls {
							if entry.toolCalls[j].name == msg.ev.ToolName {
								entry.toolCalls[j].status = "error"
								entry.toolCalls[j].result = msg.ev.Result
								break
							}
						}
					}
					break
				}
			}
		}
		m.mu.Unlock()
		return m, m.pollEvents()

	case agentEventMsg:
		m.mu.Lock()
		if msg.ev.SessionKey == m.sessionKey {
			var loadingIdx int = -1
			for i := len(m.messages) - 1; i >= 0; i-- {
				if m.messages[i].role == "assistant" && m.messages[i].isLoading {
					loadingIdx = i
					break
				}
			}
			if loadingIdx >= 0 {
				entry := &m.messages[loadingIdx]
				switch msg.ev.Type {
				case "llm_thinking":
					entry.content = ""
				case "llm_responding":
					entry.content = ""
				case "llm_stream_chunk":
					if data, ok := msg.ev.Data["data"].(bus.StreamChunkData); ok {
						entry.streamingText = data.FullText
						entry.content = data.FullText
					}
				case "llm_stream_end":
					// Stream ended
				case "llm_final":
					if data, ok := msg.ev.Data["data"].(bus.LLMFinalData); ok {
						entry.content = data.Content
					}
					m.waiting = false
					entry.isLoading = false
				case "llm_tool_calls":
					if data, ok := msg.ev.Data["data"].(bus.ToolCallEventData); ok {
						for _, tc := range data.ToolCalls {
							entry.toolCalls = append(entry.toolCalls, toolCallEntry{
								name:   tc.Name,
								args:   formatArgs(tc.Args),
								status: "pending",
							})
						}
					}
				case "tool_start":
					if data, ok := msg.ev.Data["data"].(bus.ToolCallEventData); ok {
						for _, tc := range data.ToolCalls {
							found := false
							for i := range entry.toolCalls {
								if entry.toolCalls[i].name == tc.Name && (entry.toolCalls[i].status == "pending" || entry.toolCalls[i].status == "") {
									entry.toolCalls[i].status = "running"
									entry.toolCalls[i].args = formatArgs(tc.Args)
									found = true
									break
								}
							}
							if !found {
								entry.toolCalls = append(entry.toolCalls, toolCallEntry{
									name:   tc.Name,
									args:   formatArgs(tc.Args),
									status: "running",
								})
							}
						}
					}
				case "tool_end":
					if data, ok := msg.ev.Data["data"].(bus.ToolResultEventData); ok {
						for i := range entry.toolCalls {
							if entry.toolCalls[i].name == data.ToolName {
								if data.Success {
									entry.toolCalls[i].status = "done"
								} else {
									entry.toolCalls[i].status = "error"
									entry.toolCalls[i].result = data.Error
								}
								entry.toolCalls[i].durationMs = data.DurationMs
								break
							}
						}
					}
				case "tool_error":
					if data, ok := msg.ev.Data["data"].(bus.ToolResultEventData); ok {
						for i := range entry.toolCalls {
							if entry.toolCalls[i].name == data.ToolName {
								entry.toolCalls[i].status = "error"
								entry.toolCalls[i].result = data.Error
								entry.toolCalls[i].durationMs = data.DurationMs
								break
							}
						}
					}
				case "session_end":
					m.waiting = false
					entry.isLoading = false
				}
			}
		}
		m.mu.Unlock()
		return m, m.pollEvents()

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyCtrlD:
			m.quitting = true
			return m, tea.Quit
		}
		if m.waiting {
			return m, nil
		}
		switch msg.Type {
		case tea.KeyEnter:
			userInput := strings.TrimSpace(m.textInput.Value())
			m.textInput.SetValue("")
			if userInput == "" {
				return m, nil
			}
			lower := strings.ToLower(userInput)
			if lower == "exit" || lower == "quit" || lower == "/exit" || lower == "/quit" || lower == ":q" {
				m.quitting = true
				return m, tea.Quit
			}
			m.mu.Lock()
			m.messages = append(m.messages, conversationEntry{role: "user", content: userInput})
			m.messages = append(m.messages, conversationEntry{role: "assistant", content: "", isLoading: true})
			m.waiting = true
			m.spinnerIdx = 0
			m.mu.Unlock()

			inbound := bus.InboundMessage{
				Channel:    "cli",
				SenderID:   "user",
				ChatID:     m.chatID,
				Content:    userInput,
				SessionKey: m.sessionKey,
			}
			m.messageBus.PublishInbound(inbound)

			return m, m.pollEvents()
		}
	}
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// formatArgs formats tool arguments as a pretty-printed string
func formatArgs(args map[string]any) string {
	if args == nil {
		return "{}"
	}
	var lines []string
	for k, v := range args {
		lines = append(lines, fmt.Sprintf("%s: %v", k, v))
	}
	if len(lines) == 0 {
		return "{}"
	}
	return "{\n  " + strings.Join(lines, "\n  ") + "\n}"
}

// formatDuration formats duration in milliseconds to a human-readable string
func formatDuration(ms int64) string {
	if ms < 0 {
		return ""
	}
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	if ms < 60000 {
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	}
	return fmt.Sprintf("%.1fm", float64(ms)/60000)
}


func (m *interactiveModel) View() string {
	if m.quitting {
		return "\n\n" + lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Render("Goodbye!\n")
	}

	var s strings.Builder
	m.mu.Lock()

	// Draw separator line
	separator := borderStyle.Render(strings.Repeat("─", min(60, getTerminalWidth())))

	for _, msg := range m.messages {
		if msg.role == "user" {
			s.WriteString("\n")
			s.WriteString(userPromptStyle.Render("You:") + " ")
			s.WriteString(msg.content)
			s.WriteString("\n")
		} else {
			s.WriteString("\n")
			s.WriteString(separator)
			s.WriteString("\n")
			s.WriteString(assistantLabelStyle.Render("nanobot") + "\n")

			// Show tool calls first (if any)
			if len(msg.toolCalls) > 0 {
				s.WriteString("\n")
				for _, tc := range msg.toolCalls {
					var icon, statusText string
					var iconStyle lipgloss.Style

					switch tc.status {
					case "done":
						icon = "✓"
						statusText = fmt.Sprintf(" • %s", formatDuration(tc.durationMs))
						iconStyle = toolDoneStyle
					case "error":
						icon = "✗"
						if tc.durationMs > 0 {
							statusText = fmt.Sprintf(" • %s", formatDuration(tc.durationMs))
						}
						iconStyle = toolErrorStyle
					case "running":
						icon = spinnerFrames[m.spinnerIdx]
						statusText = " running..."
						iconStyle = toolRunningStyle
					default:
						icon = "○"
						statusText = " pending"
						iconStyle = toolEntryStyle
					}

					// Tool entry with styled icon and name
					s.WriteString("  ")
					s.WriteString(iconStyle.Render(icon) + " ")
					s.WriteString(toolEntryStyle.Render(tc.name))
					s.WriteString(toolDurationStyle.Render(statusText))
					s.WriteString("\n")

					// Show args (collapsed by default)
					if tc.args != "" {
						argsLines := strings.Split(tc.args, "\n")
						if len(argsLines) > 1 {
							// Multi-line args - show preview
							s.WriteString(toolArgsStyle.Render(fmt.Sprintf("    ┌ Args: %s ...", strings.TrimSpace(argsLines[0]))))
							s.WriteString("\n")
						} else {
							// Single line args
							s.WriteString(toolArgsStyle.Render(fmt.Sprintf("    └ %s", strings.TrimSpace(argsLines[0]))))
							s.WriteString("\n")
						}
					}

					// Show error if failed
					if tc.status == "error" && tc.result != "" {
						s.WriteString("    ")
						s.WriteString(toolErrorStyle.Render("✗ Error: ") + tc.result + "\n")
					}
				}
				s.WriteString("\n")
			}

			// Show main content or thinking spinner
			if msg.isLoading {
				if msg.streamingText != "" {
					// Plain streaming text without styling (avoid escape issues)
					s.WriteString(msg.streamingText)
					s.WriteString("█") // cursor
					s.WriteString("\n")
				} else {
					// Thinking state with spinner
					s.WriteString(spinnerFrames[m.spinnerIdx])
					s.WriteString(" Thinking...\n")
				}
			} else {
				// Final content - plain text for stability
				s.WriteString(msg.content)
				s.WriteString("\n")
			}
		}
	}
	m.mu.Unlock()

	// Footer separator
	s.WriteString(separator)
	s.WriteString("\n")

	// Input field with styled prompt
	if m.waiting {
		s.WriteString(waitingStyle.Render("> waiting for response..."))
		s.WriteString("\n\n")
	}
	// Use the text input's native View for proper rendering
	s.WriteString(m.textInput.View())
	s.WriteString("\n")

	return s.String()
}

// waitingStyle for waiting indicator
var waitingStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("245")).
	Italic(true)

// getTerminalWidth returns a reasonable terminal width (fallback to 60)
func getTerminalWidth() int {
	// Try to get from environment or use default
	if w := os.Getenv("COLUMNS"); w != "" {
		var cw int
		if _, err := fmt.Sscanf(w, "%d", &cw); err == nil && cw > 0 {
			return cw
		}
	}
	return 60
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ============================================================================
// Gateway Implementation
// ============================================================================

func runGateway(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set log level
	if gatewayVerboseFlag {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})))
	}

	cfg, err := config.Load(gatewayConfigFlag)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Override port if specified
	port := gatewayPortFlag
	if port == 0 {
		port = cfg.Gateway.Port
	}

	// Override workspace if specified
	workspace := gatewayWorkspaceFlag
	if workspace == "" {
		workspace = cfg.Agents.Workspace
	}
	if workspace == "" {
		homeDir, _ := os.UserHomeDir()
		workspace = filepath.Join(homeDir, ".nanobot", "workspace")
	}

	// Create workspace directory
	if err := os.MkdirAll(workspace, 0755); err != nil {
		slog.Error("failed to create workspace", "error", err)
		os.Exit(1)
	}

	// Create session store
	sessionStore, err := session.NewFileSessionStore(filepath.Join(workspace, "sessions"))
	if err != nil {
		slog.Error("failed to create session store", "error", err)
		os.Exit(1)
	}

	// Create message bus
	messageBus := bus.New(100)

	// Create cron service
	cronStorePath := filepath.Join(workspace, "data", "cron", "jobs.json")
	cronService := cron.NewCronService(cronStorePath, func(job *cron.CronJob) {
		slog.Info("cron job executing", "name", job.Name)
		msg := bus.InboundMessage{
			Channel:    job.Payload.Channel,
			ChatID:     job.Payload.To,
			Content:    job.Payload.Message,
			SessionKey: fmt.Sprintf("%s:%s", job.Payload.Channel, job.Payload.To),
		}
		messageBus.PublishInbound(msg)
	})

	// Create tool registry
	toolRegistry := tools.NewRegistry()
	toolRegistry.Register(tools.NewMessageTool())
	toolRegistry.Register(tools.NewFilesystemTool(nil))
	toolRegistry.Register(tools.NewShellTool(true, nil, nil))
	toolRegistry.Register(tools.NewWebTool())
	toolRegistry.Register(tools.NewCronTool(cronService, messageBus))
	toolRegistry.Register(tools.NewSpawnTool(nil))

	// Create provider
	provider := createProvider(cfg)

	// Create agent loop
	maxIterations := cfg.Agents.MaxToolIterations
	if maxIterations <= 0 {
		maxIterations = 40
	}
	agentLoop := agent.NewAgentLoop(messageBus, sessionStore, toolRegistry, provider, maxIterations, false)

	// Create channel manager
	channelManager := channels.NewManager(messageBus)

	// Handle shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		slog.Info("received signal, shutting down", "signal", sig)
		cancel()
		channelManager.StopAll()
		cronService.Stop()
		messageBus.Close()
	}()

	// Start cron service
	if err := cronService.Start(ctx); err != nil {
		slog.Error("failed to start cron service", "error", err)
	}

	// Start channel manager
	if err := channelManager.StartAll(ctx); err != nil {
		slog.Error("failed to start channels", "error", err)
	}

	// Start agent loop
	go func() {
		if err := agentLoop.Start(ctx); err != nil && err != context.Canceled {
			slog.Error("agent loop error", "error", err)
		}
	}()

	gatewayAddr := fmt.Sprintf("%s:%d", cfg.Gateway.Host, port)
	slog.Info("gateway starting", "address", gatewayAddr)
	fmt.Printf("%s Starting nanobot gateway v%s on port %d...\n", logo, version, port)

	<-ctx.Done()
	slog.Info("gateway stopped")
}

// ============================================================================
// Status Implementation
// ============================================================================

func runStatus(cmd *cobra.Command, args []string) {
	cfg, err := config.Load(statusConfigFlag)
	if err != nil {
		slog.Warn("failed to load config", "error", err)
		cfg = &config.Config{}
	}

	// Get config path
	configPath := statusConfigFlag
	if configPath == "" {
		homeDir, _ := os.UserHomeDir()
		configPath = filepath.Join(homeDir, ".nanobot", "config.json")
	}

	// Get workspace path
	workspace := cfg.Agents.Workspace
	if workspace == "" {
		homeDir, _ := os.UserHomeDir()
		workspace = filepath.Join(homeDir, ".nanobot", "workspace")
	}

	fmt.Printf("%s nanobot Status\n\n", logo)

	// Config status
	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Config: %s \u2713\n", configPath)
	} else {
		fmt.Printf("Config: %s \u2717 (not found)\n", configPath)
	}

	// Workspace status
	if _, err := os.Stat(workspace); err == nil {
		fmt.Printf("Workspace: %s \u2713\n", workspace)
	} else {
		fmt.Printf("Workspace: %s \u2717 (not found)\n", workspace)
	}

	fmt.Println()

	// Model info
	fmt.Printf("Model: %s\n", cfg.Agents.Model)
	fmt.Printf("Provider: %s\n", cfg.Agents.Provider)

	// Check API keys
	if cfg.Providers.OpenAI != nil {
		if k, ok := cfg.Providers.OpenAI["api_key"].(string); ok && k != "" {
			fmt.Println("OpenAI: \u2713 configured")
		} else {
			fmt.Println("OpenAI: not configured")
		}
	}

	if cfg.Providers.OpenRouter != nil {
		if k, ok := cfg.Providers.OpenRouter["api_key"].(string); ok && k != "" {
			fmt.Println("OpenRouter: \u2713 configured")
		} else {
			fmt.Println("OpenRouter: not configured")
		}
	}

	if cfg.Providers.Anthropic != nil {
		if k, ok := cfg.Providers.Anthropic["api_key"].(string); ok && k != "" {
			fmt.Println("Anthropic: \u2713 configured")
		} else {
			fmt.Println("Anthropic: not configured")
		}
	}

	if cfg.Providers.Minimax != nil {
		if k, ok := cfg.Providers.Minimax["api_key"].(string); ok && k != "" {
			fmt.Println("MiniMax: \u2713 configured")
		} else {
			fmt.Println("MiniMax: not configured")
		}
	}
}

// ============================================================================
// Channels Implementation
// ============================================================================

func runChannelsStatus(cmd *cobra.Command, args []string) {
	cfg, err := config.Load("")
	if err != nil {
		cfg = &config.Config{}
	}

	fmt.Println("Channel Status")
	fmt.Println("==============")
	fmt.Println()

	channels := map[string]struct {
		Name    string
		Enabled bool
	}{
		"telegram": {Name: "Telegram", Enabled: false},
		"discord":  {Name: "Discord", Enabled: false},
		"slack":    {Name: "Slack", Enabled: false},
		"whatsapp": {Name: "WhatsApp", Enabled: false},
		"feishu":   {Name: "Feishu", Enabled: false},
		"dingtalk": {Name: "DingTalk", Enabled: false},
		"wecom":    {Name: "WeCom", Enabled: false},
		"email":    {Name: "Email", Enabled: false},
	}

	// Check enabled channels from config
	// Note: The actual channel enabling is determined by the config

	for name, ch := range channels {
		enabled := false
		// Check if channel is configured in config.Channels
		// This is a simplified check - actual implementation would check the specific channel config
		_ = name
		_ = cfg

		status := "\u2717"
		if enabled {
			status = "\u2713"
		}
		fmt.Printf("%s: %s\n", ch.Name, status)
	}
}

func runChannelsLogin(cmd *cobra.Command, args []string) {
	fmt.Println("Channel login not implemented")
	fmt.Println("Use 'nanobot gateway' to start the gateway with channel support")
}
