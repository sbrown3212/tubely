package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
)

const presignedURLExpire = time.Minute * 5

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	parts := strings.Split(*video.VideoURL, ",")
	if len(parts) != 2 {
		return database.Video{}, errors.New("invalid format, want \"<bucket>,<key>\"")
	}

	bucket := parts[0]
	key := parts[1]

	signedURL, err := generatePresignedURL(cfg.s3Client, bucket, key, presignedURLExpire)
	if err != nil {
		return database.Video{}, fmt.Errorf("failed to generate presigned url: %w", err)
	}

	video.VideoURL = &signedURL
	return video, nil
}
