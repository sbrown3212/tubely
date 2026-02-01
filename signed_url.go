package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
)

const presignedURLExpire = time.Minute * 5

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	fmt.Printf("\nVideo URL: %s\n\n", *video.VideoURL)

	parts := strings.Split(*video.VideoURL, ",")
	// fmt.Printf("Parts: %s, %s\n", parts[0], parts[1])

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

func generatePresignedURL(
	s3client *s3.Client, bucket, key string, expireTime time.Duration,
) (string, error) {
	presignedClient := s3.NewPresignClient(s3client)
	req, err := presignedClient.PresignGetObject(context.Background(), &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	}, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", fmt.Errorf("PresignGetObject error: %w", err)
	}
	url := req.URL
	return url, nil
}
