package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

var pullCmd = &cobra.Command{
	Use:   "pull <image>",
	Short: "pull an image via the pulse daemon",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		image := args[0]
		client, err := getDaemonClient()
		if err != nil {
			fmt.Println("Error", err)
			return
		}

		body, _ := json.Marshal(map[string]string{"image": image})
		resp, err := client.Post("http://unix/pull", "application/json", bytes.NewBuffer(body))
		if err != nil {
			fmt.Println("❌ Failed to connect to daemon:", err)
			return
		}
		defer resp.Body.Close()

		io.Copy(os.Stdout, resp.Body)

	},
}

func init() {
	rootCmd.AddCommand(pullCmd)
}
