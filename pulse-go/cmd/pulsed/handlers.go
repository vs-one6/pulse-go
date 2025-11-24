package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vishnucs/pulse-go/internals"
)

type PullRequest struct {
	Image string `json:"image"`
}

type RemoveRequest struct {
	Image string `json:"image"`
}

type RunRequest struct {
	Image       string   `json:"image"`
	Cmd         []string `json:"cmd"`
	Env         []string `json:"env"`
	Network     bool     `json:"network"`
	Interactive bool     `json:"interactive"`
}

func handlePull(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Cache-Control", "no-cache")

	var req PullRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	image := req.Image
	fmt.Fprintf(w, "Starting pull for %s...\n", image)
	flusher.Flush()

	_, err := internals.PullImage(image, w, flusher)
	if err != nil {
		return
	}
	flusher.Flush()
}

func handleListImages(w http.ResponseWriter, r *http.Request) {
	imagesDir := filepath.Join(os.Getenv("HOME"), ".pulse", "images")

	files, err := os.ReadDir(imagesDir)
	if err != nil {
		http.Error(w, "failed to read images directory\n", http.StatusInternalServerError)
		return
	}

	var imageList []map[string]string
	for _, f := range files {
		if f.IsDir() {
			info, _ := f.Info()
			imageList = append(imageList, map[string]string{
				"name":     f.Name(),
				"modified": info.ModTime().Format(time.RFC822),
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(imageList)
}

func handleRemove(w http.ResponseWriter, r *http.Request) {
	var req RemoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	image := strings.TrimSpace(req.Image)
	if image == "" {
		http.Error(w, "Missing image parameter", http.StatusBadRequest)
		return
	}

	msg, err := internals.RemoveImage(image)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": msg,
	})
}

func handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Validate command
	if len(req.Cmd) == 0 {
		req.Cmd = []string{"/bin/sh"}
	}

	// Extract the image
	fmt.Fprintf(w, "📦 Extracting image %s...\n", req.Image)
	w.(http.Flusher).Flush()

	rootfs, err := internals.Extract(req.Image)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to extract image: %v", err), http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "✅ Image extracted to %s\n", rootfs)
	fmt.Fprintf(w, "🚀 Starting container...\n\n")
	w.(http.Flusher).Flush()

	// Run the container with interactive flag
	if err := internals.RunContainer(rootfs, req.Cmd, req.Env, req.Network, req.Interactive); err != nil {
		fmt.Fprintf(w, "\n❌ Container failed: %v\n", err)
		return
	}

	fmt.Fprintf(w, "\n✅ Container exited successfully\n")
}
