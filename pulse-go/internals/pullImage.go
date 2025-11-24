package internals

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/containers/image/v5/copy"
	"github.com/containers/image/v5/signature"
	"github.com/containers/image/v5/transports/alltransports"
	"github.com/containers/image/v5/types"
)

func PullImage(image string, w io.Writer, flusher http.Flusher) (string, error) {
	ctx := context.Background()
	fmt.Fprintf(w, "⬇️ Pulling image: %s\n", image)
	flusher.Flush()

	localDir := filepath.Join(getImagesDir(), fmt.Sprintf("%s-oci", sanitize(image)))
	manifestPath := filepath.Join(localDir, "index.json")
	if _, err := os.Stat(manifestPath); err == nil {
		msg := fmt.Sprintf("✅ Image %s already available locally. Skipping pull.\n", image)
		fmt.Fprint(w, msg)
		flusher.Flush()
		return msg, nil
	}

	fmt.Fprintf(w, "Pulling image from Docker registry...\n")
	flusher.Flush()

	// Try to load system policy, fallback if missing
	policy, err := signature.DefaultPolicy(nil)
	if err != nil {
		fmt.Fprintf(w, "⚠️ No system policy found, using insecure fallback.\n")
		flusher.Flush()

		policy = &signature.Policy{
			Default: []signature.PolicyRequirement{
				signature.NewPRInsecureAcceptAnything(),
			},
		}
	}

	policyCtx, err := signature.NewPolicyContext(policy)
	if err != nil {
		return "", fmt.Errorf("failed to create policy context: %v", err)
	}
	defer policyCtx.Destroy()

	srcRef, err := alltransports.ParseImageName(fmt.Sprintf("docker://%s", image))
	if err != nil {
		return "", fmt.Errorf("invalid image name: %v", err)
	}

	destPath := filepath.Join(getImagesDir(), fmt.Sprintf("%s-oci", sanitize(image)))
	if err := os.MkdirAll(destPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create destination: %v", err)
	}

	destRef, err := alltransports.ParseImageName(fmt.Sprintf("oci:%s", destPath))
	if err != nil {
		return "", fmt.Errorf("invalid destination path: %v", err)
	}

	systemCtx := &types.SystemContext{
		DockerInsecureSkipTLSVerify: types.NewOptionalBool(true),
	}

	progress := func(format string, args ...interface{}) {
		fmt.Fprintf(w, format+"\n", args...)
		flusher.Flush()
	}

	_, err = copy.Image(ctx, policyCtx, destRef, srcRef, &copy.Options{
		SourceCtx:      systemCtx,
		DestinationCtx: systemCtx,
		ReportWriter: writerFunc(func(msg string, args ...interface{}) {
			fmt.Fprintf(w, msg, args...)
			flusher.Flush()
		}),
	})
	if err != nil {
		progress("❌ Pull failed: %v", err)
		os.RemoveAll(destPath)
		return "", err
	}

	msg := fmt.Sprintf("✅ Successfully pulled image: %s", image)
	progress(msg)
	return msg, nil
}

type writerFunc func(string, ...interface{})

func (fn writerFunc) Write(p []byte) (n int, err error) {
	fn("%s", string(p))
	return len(p), nil
}

func getImagesDir() string {
	baseDir := getPulseHome()
	imagesDir := filepath.Join(baseDir, "images")

	// Ensure the directory exists with proper permissions
	if err := os.MkdirAll(imagesDir, 0755); err == nil {
		// If running under sudo, fix ownership
		fixDirOwnership(imagesDir)
	}

	return imagesDir
}

// fixDirOwnership fixes ownership when running under sudo
func fixDirOwnership(dir string) {
	sudoUID := os.Getenv("SUDO_UID")
	sudoGID := os.Getenv("SUDO_GID")

	if sudoUID != "" && sudoGID != "" {
		uid, _ := strconv.Atoi(sudoUID)
		gid, _ := strconv.Atoi(sudoGID)
		os.Chown(dir, uid, gid)
	}
}

// getPulseHome returns the .pulse directory, handling sudo correctly
func getPulseHome() string {
	// Check if running under sudo by checking SUDO_UID
	sudoUID := os.Getenv("SUDO_UID")
	if sudoUID != "" {
		// Running under sudo, get the original user's home directory
		uid, err := strconv.Atoi(sudoUID)
		if err == nil {
			// Look up user by UID
			if u, err := user.LookupId(strconv.Itoa(uid)); err == nil {
				return filepath.Join(u.HomeDir, ".pulse")
			}
		}

		// Fallback: try SUDO_USER environment variable
		sudoUser := os.Getenv("SUDO_USER")
		if sudoUser != "" {
			// Construct path from username
			return filepath.Join("/home", sudoUser, ".pulse")
		}
	}

	// Normal case: use current user's home
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = os.Getenv("HOME")
		if homeDir == "" {
			homeDir = "/tmp" // Last resort fallback
		}
	}
	return filepath.Join(homeDir, ".pulse")
}

func sanitize(image string) string {
	replacer := strings.NewReplacer("/", "_", ":", "_")
	return replacer.Replace(image)
}
