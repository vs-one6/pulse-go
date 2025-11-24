package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"time"
)

func getDaemonClient() (*http.Client, error) {
	socketPath := "/tmp/pulse.sock"
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		fmt.Println("Stating Pulse daemon.......")
		cmd := exec.Command("pulsed")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("failed to start pulsed: %v", err)

		}

	}
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	tr := &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", socketPath)

		},
	}
	client := &http.Client{Transport: tr}
	// Wait until daemon health endpoint responds
	for i := 0; i < 50; i++ {
		resp, err := client.Get("http://unix/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return client, nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return nil, fmt.Errorf("!Daemon not responding after startup")

}
