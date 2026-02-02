package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	// Set limit on response body to 1 GB
	const uploadLimit = 1 << 30
	r.Body = http.MaxBytesReader(w, r.Body, uploadLimit)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Invalid JWT", err)
		return
	}

	dbVideo, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(
			w, http.StatusInternalServerError, "Couldn't find video metadata in db", err,
		)
		return
	}

	if dbVideo.UserID != userID {
		respondWithError(
			w, http.StatusUnauthorized, "Not authorized to update this video", nil,
		)
		return
	}

	file, fileHeader, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	mediaType, _, err := mime.ParseMediaType(fileHeader.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Content-Type", err)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(
			w, http.StatusBadRequest, "Invalid file type, only MP4 is allowed", nil,
		)
		return
	}

	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(
			w, http.StatusInternalServerError, "Couldn't create temp file", err,
		)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	_, err = io.Copy(tempFile, file)
	if err != nil {
		respondWithError(
			w, http.StatusInternalServerError, "Couldn't save video to disk", err,
		)
		return
	}

	_, err = tempFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(
			w, http.StatusInternalServerError, "Couldn't reset file pointer", err,
		)
		return
	}

	fastStartFileName, err := processVideoForFastStart(tempFile.Name())
	if err != nil {
		respondWithError(
			w, http.StatusInternalServerError, "Couldn't process video for fast start", err,
		)
		return
	}
	defer os.Remove(fastStartFileName)

	fastStartVideo, err := os.Open(fastStartFileName)
	if err != nil {
		respondWithError(
			w, http.StatusInternalServerError, "Couldn't open fast start video", err,
		)
		return
	}
	defer fastStartVideo.Close()

	aspectRatio, err := getVideoAspecRatio(tempFile.Name())
	if err != nil {
		respondWithError(
			w, http.StatusInternalServerError, "Unable to deterime video aspect ratio", err,
		)
		return
	}

	var dir string
	switch aspectRatio {
	case "16:9":
		dir = "landscape"
	case "9:16":
		dir = "portrait"
	default:
		dir = "other"
	}

	assetName := getAssetPath(mediaType)
	key := path.Join(dir, assetName)

	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(key),
		Body:        fastStartVideo,
		ContentType: aws.String(mediaType),
	})
	if err != nil {
		respondWithError(
			w, http.StatusInternalServerError, "Couldn't put object in s3", err,
		)
		return
	}

	url := fmt.Sprintf("%s,%s", cfg.s3Bucket, key)
	dbVideo.VideoURL = &url

	err = cfg.db.UpdateVideo(dbVideo)
	if err != nil {
		respondWithError(
			w, http.StatusInternalServerError, "Couldn't update videoURL", err,
		)
		return
	}

	signedVideo, err := cfg.dbVideoToSignedVideo(dbVideo)
	if err != nil {
		respondWithError(
			w, http.StatusInternalServerError, "Failed to get signed video", err,
		)
		return
	}

	respondWithJSON(w, http.StatusOK, signedVideo)
}

func getVideoAspecRatio(filePath string) (string, error) {
	cmd := exec.Command(
		"ffprobe",
		"-v", "error",
		"-print_format", "json",
		"-show_streams",
		filePath,
	)

	var buf bytes.Buffer
	cmd.Stdout = &buf

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("ffprobe error: %w", err)
	}

	var output struct {
		Streams []struct {
			Height int `json:"height"`
			Width  int `json:"width"`
		} `json:"streams"`
	}

	err = json.Unmarshal(buf.Bytes(), &output)
	if err != nil {
		return "", fmt.Errorf("could not parse ffprobe output: %w", err)
	}
	if len(output.Streams) == 0 {
		return "", errors.New("no video streams found")
	}

	vidWidth := output.Streams[0].Width
	vidHeight := output.Streams[0].Height
	aspectRatio := float64(vidWidth) / float64(vidHeight)
	tolerance := 0.1
	horizontalRatio := float64(16) / float64(9)
	verticalRatio := float64(9) / float64(16)

	if aspectRatio > horizontalRatio-tolerance {
		return "16:9", nil
	} else if aspectRatio < verticalRatio+tolerance {
		return "9:16", nil
	} else {
		return "other", nil
	}
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
