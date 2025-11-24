package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "pulse",
	Short: "Pulse — lightweight container engine CLI",
	Long:  "Pulse is a minimal container engine CLI that talks to a background daemon via Unix sockets.",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Welcome to Pulse CLI. Use `pulse pull <image>` to pull an image.")
	},
}

func Execute() error {
	return rootCmd.Execute()
}
