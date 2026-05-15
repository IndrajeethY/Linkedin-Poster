package bot

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func DownloadFile(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	tokens := strings.Split(url, "/")
	fileName := filepath.Base(tokens[len(tokens)-1])

	tmpFile, err := os.CreateTemp("", "libot-*-"+fileName)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer tmpFile.Close()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("write file: %w", err)
	}

	return tmpFile.Name(), nil
}
