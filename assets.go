package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (cfg apiConfig) ensureAssetsDir() error {
	if _, err := os.Stat(cfg.assetsRoot); os.IsNotExist(err) {
		return os.Mkdir(cfg.assetsRoot, 0755)
	}
	return nil
}

func getAssetPath(mediaType string) string {
	randomSlice := make([]byte, 32)
	_, err := rand.Read(randomSlice)
	if err != nil {
		panic("failed to generate random bytes")
	}
	name := base64.RawURLEncoding.EncodeToString(randomSlice)

	ext := mediaTypeToExt(mediaType)
	return fmt.Sprintf("%s%s", name, ext)
}

func (cfg apiConfig) getObjectURL(key string) string {
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, key)
}

func (cfg apiConfig) getAssetDiskPath(assetPath string) string {
	return filepath.Join(cfg.assetsRoot, assetPath)
}

func (cfg apiConfig) getAssetURL(assetPath string) string {
	return fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port, assetPath)
}

func mediaTypeToExt(mediaType string) string {
	parts := strings.Split(mediaType, "/")
	if len(parts) != 2 {
		return ".bin"
	}
	return "." + parts[1]
}

func processVideoForFastStart(inputFilePath string) (string, error) {
	outputFilePath := inputFilePath + ".processing"

	cmd := exec.Command("ffmpeg",
		"-i", inputFilePath,
		"-c", "copy",
		"-movflags", "faststart",
		"-f", "mp4", outputFilePath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("error processing video: %s, %w", stderr.String(), err)
	}

	fileInfo, err := os.Stat(outputFilePath)
	if err != nil {
		return "", fmt.Errorf("could not stat processed file: %w", err)
	}
	if fileInfo.Size() == 0 {
		return "", errors.New("processed file is empty")
	}

	return outputFilePath, nil
}
