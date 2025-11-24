package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vishnucs/pulse-go/internals"
)

var (
	runCmdFlags struct {
		cmd         string
		envVars     []string
		network     bool
		interactive bool
	}
)

var runCmd = &cobra.Command{
	Use:   "run <image> [command...]",
	Short: "Run a container from an image",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		image := args[0]

		var containerCmd []string

		if len(args) > 1 {
			// pulse run alpine ls -l /
			containerCmd = args[1:]
		} else if runCmdFlags.cmd != "" {
			// pulse run alpine --cmd "sleep 5"
			containerCmd = strings.Split(runCmdFlags.cmd, " ")
		} else {
			containerCmd = []string{"/bin/sh"}
		}

		if runCmdFlags.interactive {
			// Check if running as root when networking is enabled
			if runCmdFlags.network && os.Geteuid() != 0 {
				fmt.Println("❌ Networking requires root privileges")
				fmt.Println("   Please run with sudo:")
				fmt.Printf("   sudo pulse run -i -n %s\n", image)
				os.Exit(1)
			}

			fmt.Printf("📦 Extracting image %s...\n", image)

			// Setup networking if enabled
			if runCmdFlags.network {
				fmt.Println("🌐 Setting up container networking...")
				if err := internals.SetupNetworking(); err != nil {
					fmt.Printf("❌ Failed to setup networking: %v\n", err)
					fmt.Println("   Try running with sudo or check system requirements")
					os.Exit(1)
				}
				fmt.Println("✅ Container networking ready (bridge: pulse0)")
			}

			// Extract the image
			rootfs, err := internals.Extract(image)
			if err != nil {
				fmt.Printf("❌ Failed to extract image: %v\n", err)
				return
			}

			// Ensure rootfs is readable when running with sudo
			if runCmdFlags.network {
				// Make rootfs accessible to the container process
				sudoUID := os.Getenv("SUDO_UID")
				if sudoUID != "" {
					// Set proper permissions on rootfs for access
					os.Chmod(rootfs, 0755)
					// Make parent directories traversable
					parent := filepath.Dir(rootfs)
					os.Chmod(parent, 0755)
					os.Chmod(filepath.Dir(parent), 0755)
				}
			}

			fmt.Printf("✅ Image extracted to %s\n", rootfs)

			if runCmdFlags.network {
				fmt.Printf("🚀 Starting container with network access...\n\n")
			} else {
				fmt.Printf("🚀 Starting container (network isolated)...\n\n")
			}

			// Run container directly (not through daemon)
			if err := internals.RunContainer(rootfs, containerCmd, runCmdFlags.envVars, runCmdFlags.network, true); err != nil {
				fmt.Printf("\n❌ Container failed: %v\n", err)
				return
			}

			fmt.Printf("\n✅ Container exited successfully\n")
			return
		}

		client, err := getDaemonClient()
		if err != nil {
			fmt.Println("❌ Failed to connect runtime:", err)
			return
		}

		req := map[string]any{
			"image":       image,
			"cmd":         containerCmd,
			"env":         runCmdFlags.envVars,
			"network":     runCmdFlags.network,
			"interactive": false,
		}

		body, _ := json.Marshal(req)
		resp, err := client.Post("http://unix/run", "application/json", bytes.NewBuffer(body))
		if err != nil {
			fmt.Println("❌ Failed to contact daemon:", err)
			return
		}
		defer resp.Body.Close()

		io.Copy(os.Stdout, resp.Body)
	},
}

func init() {
	runCmd.Flags().StringVarP(&runCmdFlags.cmd, "cmd", "c", "", "Command to run, e.g. --cmd 'sleep 5'")
	runCmd.Flags().StringSliceVarP(&runCmdFlags.envVars, "env", "e", nil, "Env variables: -e FOO=bar")
	runCmd.Flags().BoolVarP(&runCmdFlags.network, "net", "n", false, "Enable networking")
	runCmd.Flags().BoolVarP(&runCmdFlags.interactive, "interactive", "i", false, "Interactive mode with TTY")

	rootCmd.AddCommand(runCmd)
}
