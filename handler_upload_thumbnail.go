package main

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
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
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	const maxMemory = 10 << 20
	err = r.ParseMultipartForm(int64(maxMemory))
	if err != nil {
		fmt.Printf("parse multipart form error: %s", err)
		respondWithError(
			w, http.StatusBadRequest, "unable to parse multipart form", err,
		)
		return
	}

	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		fmt.Printf("formfile error: %s", err)
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Content-Type", err)
		return
	}
	if mediaType != "image/jpeg" && mediaType != "image/png" {
		respondWithError(w, http.StatusBadRequest, "Invalid file type", nil)
		return
	}

	dbVideo, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(
			w,
			http.StatusInternalServerError,
			"Unable to get video metadeta from db",
			err,
		)
		return
	}

	if dbVideo.UserID != userID {
		respondWithError(
			w, http.StatusUnauthorized, "Not authorized to update this video", nil,
		)
		return
	}

	assetPath := getAssetPath(mediaType)
	assetDiskPath := cfg.getAssetDiskPath(assetPath)

	dest, err := os.Create(assetDiskPath)
	if err != nil {
		respondWithError(
			w, http.StatusInternalServerError, "Unable to create file on server", err,
		)
		return
	}
	defer dest.Close()
	if _, err = io.Copy(dest, file); err != nil {
		respondWithError(
			w, http.StatusInternalServerError, "Error saving file", err,
		)
		return
	}

	url := cfg.getAssetURL(assetPath)
	dbVideo.ThumbnailURL = &url

	err = cfg.db.UpdateVideo(dbVideo)
	if err != nil {
		respondWithError(
			w, http.StatusInternalServerError, "Unable to update thumbnail URL", err,
		)
		return
	}

	respondWithJSON(w, http.StatusOK, dbVideo)
}
