package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"
)

var imagesCmd = &cobra.Command{
	Use:   "images",
	Short: "List all images pulled by the Pulse daemon",
	Run: func(cmd *cobra.Command, args []string) {
		client, err := getDaemonClient()
		if err != nil {
			fmt.Println(err)
			return
		}
		resp, err := client.Get("http://unix/images")
		if err != nil {
			fmt.Println(" Failed to reach daemon:", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			fmt.Printf(" Daemon error (%d): %s\n", resp.StatusCode, string(body))
			return
		}

		var images []map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&images); err != nil {
			fmt.Println(" Invalid response from daemon:", err)
			return
		}

		if len(images) == 0 {
			fmt.Println("No images found.")
			return
		}

		fmt.Printf("%-30s %-20s\n", "IMAGE", "LAST MODIFIED")
		fmt.Println("-----------------------------------------------------------")
		for _, img := range images {
			fmt.Printf("%-30s %-20s\n", img["name"], img["modified"])
		}
	},
}

func init() {
	rootCmd.AddCommand(imagesCmd)
}
