package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

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

func init() {
	rootCmd.AddCommand(onboardCmd)
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(gatewayCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(channelsCmd)
	rootCmd.AddCommand(versionCmd)
}
