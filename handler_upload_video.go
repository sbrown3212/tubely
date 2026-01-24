package main

import (
	"io"
	"mime"
	"net/http"
	"os"

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
		respondWithError(w, http.StatusInternalServerError, "Couldn't save video to disk", err)
		return
	}

	_, err = tempFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't reset file pointer", err)
		return
	}

	key := getAssetPath(mediaType)

	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(key),
		Body:        tempFile,
		ContentType: aws.String(mediaType),
	})
	if err != nil {
		respondWithError(
			w, http.StatusInternalServerError, "Couldn't put object in s3", err,
		)
		return
	}

	url := cfg.getObjectURL(key)
	dbVideo.VideoURL = &url

	err = cfg.db.UpdateVideo(dbVideo)
	if err != nil {
		respondWithError(
			w, http.StatusInternalServerError, "Couldn't update videoURL", err,
		)
		return
	}

	respondWithJSON(w, http.StatusOK, dbVideo)
}
