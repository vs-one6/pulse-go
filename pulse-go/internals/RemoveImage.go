package internals

import (
	"fmt"
	"os"
	"path/filepath"
)

func RemoveImage(image string) (string, error) {
	imageDir := filepath.Join(getImagesDir(), fmt.Sprintf("%s-oci", sanitize(image)))

	if _, err := os.Stat(imageDir); os.IsNotExist(err) {
		return "", fmt.Errorf("❌ image %s not found locally", image)
	}

	if err := os.RemoveAll(imageDir); err != nil {
		return "", fmt.Errorf("❌ failed to remove image %s: %v", image, err)
	}

	return fmt.Sprintf("🗑️ Successfully removed image: %s", image), nil
}
