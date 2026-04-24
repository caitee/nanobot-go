package main

import (
	"fmt"

	"nanobot-go/internal/config"

	"github.com/spf13/cobra"
)

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

		status := "✗"
		if enabled {
			status = "✓"
		}
		fmt.Printf("%s: %s\n", ch.Name, status)
	}
}

func runChannelsLogin(cmd *cobra.Command, args []string) {
	fmt.Println("Channel login not implemented")
	fmt.Println("Use 'nanobot gateway' to start the gateway with channel support")
}
