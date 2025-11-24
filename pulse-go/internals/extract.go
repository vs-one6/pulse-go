package internals

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type OCIIndex struct {
	Manifests []struct {
		Digest string `json:"digest"`
	} `json:"manifests"`
}

type OCIManifest struct {
	Layers []struct {
		Digest string `json:"digest"`
	} `json:"layers"`
}

func Extract(image string) (string, error) {
	imageDir := filepath.Join(getImagesDir(), fmt.Sprintf("%s-oci", sanitize(image)))
	extractDir := filepath.Join(imageDir, "rootfs")

	// Check if extraction already exists and is complete
	markerFile := filepath.Join(extractDir, ".extraction_complete")
	if _, err := os.Stat(markerFile); err == nil {
		// Already extracted, return existing directory
		return extractDir, nil
	}

	// Remove any incomplete extraction
	if err := os.RemoveAll(extractDir); err != nil {
		return "", fmt.Errorf("failed to clean existing rootfs: %v", err)
	}

	// Create fresh extraction directory
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create rootfs directory: %v", err)
	}

	indexFile := filepath.Join(imageDir, "index.json")
	indexData, err := os.ReadFile(indexFile)
	if err != nil {
		return "", fmt.Errorf("failed to read index.json: %v", err)
	}

	var index OCIIndex
	if err := json.Unmarshal(indexData, &index); err != nil {
		return "", fmt.Errorf("invalid index.json")
	}

	if len(index.Manifests) == 0 {
		return "", fmt.Errorf("no manifest found")
	}

	manifestDigest := strings.TrimPrefix(index.Manifests[0].Digest, "sha256:")
	manifestPath := filepath.Join(imageDir, "blobs/sha256", manifestDigest)
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", fmt.Errorf("failed to read manifest: %v", err)
	}

	var manifest OCIManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return "", fmt.Errorf("invalid manifest JSON: %v", err)
	}

	// Extract all layers
	for _, layer := range manifest.Layers {
		layerDigest := strings.TrimPrefix(layer.Digest, "sha256:")
		layerPath := filepath.Join(imageDir, "blobs/sha256", layerDigest)

		if err := extractTar(layerPath, extractDir); err != nil {
			// Clean up on failure
			os.RemoveAll(extractDir)
			return "", fmt.Errorf("failed to extract layer %s: %v", layerDigest, err)
		}
	}

	// Mark extraction as complete
	if f, err := os.Create(markerFile); err == nil {
		f.Close()
	}

	return extractDir, nil
}
