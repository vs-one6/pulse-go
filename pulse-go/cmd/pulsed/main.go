package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/vishnucs/pulse-go/internals"
)

func main() {
	// Check if we're being called as a child process
	if len(os.Args) > 1 && os.Args[1] == "child" {
		if err := internals.ChildProcess(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Container error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	socketPath := "/tmp/pulse.sock"
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		panic(err)
	}
	defer os.Remove(socketPath)

	fmt.Println("🔧 Pulse daemon listening on", socketPath)
	os.Chmod(socketPath, 0666)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "Pulse Daemon is healthy ✅")
	})
	mux.HandleFunc("/pull", handlePull)
	mux.HandleFunc("/images", handleListImages)
	mux.HandleFunc("/remove", handleRemove)
	mux.HandleFunc("/run", handleRun)

	server := &http.Server{Handler: mux}

	// Graceful shutdown when Ctrl+C or SIGTERM
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Println("Server error:", err)
		}
	}()

	fmt.Println("🚀 Pulse Daemon started successfully.")

	<-stop
	fmt.Println("\n🛑 Shutting down Pulse Daemon...")
	server.Close()
	os.Remove(socketPath)
	fmt.Println("✅ Clean exit.")
}
