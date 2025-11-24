package internals

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func extractTar(tarPath, destDir string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("not a gzip: %v", err)
	}
	defer gz.Close()

	tarReader := tar.NewReader(gz)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(destDir, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			// Create parent directories
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}

			// Create the file
			out, err := os.Create(target)
			if err != nil {
				return err
			}

			// Copy content
			if _, err := io.Copy(out, tarReader); err != nil {
				out.Close()
				return err
			}
			out.Close()

			// CRITICAL: Preserve file permissions (including execute bit)
			if err := os.Chmod(target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Handle symbolic links
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				// Ignore errors if symlink already exists
				if !os.IsExist(err) {
					return err
				}
			}
		}
	}
	return nil
}
