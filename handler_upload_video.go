package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {

	const maxMemory = 10 << 30

	r.Body = http.MaxBytesReader(w, r.Body, maxMemory)

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

	vid_mdata, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Unable to find video in database", err)
		return
	}

	if userID != vid_mdata.UserID {
		respondWithError(w, http.StatusUnauthorized, "not allowed to upload video for other users", err)
		return
	}

	r.ParseMultipartForm(maxMemory)

	file, fheader, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	f_ctype := fheader.Header.Get("Content-Type")

	mediaType, _, err := mime.ParseMediaType(f_ctype)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Content-Type", err)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid file type", nil)
		return
	}
	
	tmp_file, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create temp file", err)
		return
	}
	defer os.Remove(tmp_file.Name())
	defer tmp_file.Close()

	_, err = io.Copy(tmp_file, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to copy contents to temp file", err)
		return
	}
	
	_, err = tmp_file.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not reset file pointer", err)
		return
	}

	ct := strings.Split(mediaType, "/")

	key := make([]byte, 32)
	rand.Read(key)
	encodedString := base64.RawURLEncoding.EncodeToString(key)

	file_key := fmt.Sprintf("%v.%v", encodedString, ct[1])

	s3c_params := s3.PutObjectInput {
		Bucket: &cfg.s3Bucket,
		Key: &file_key,
		Body: tmp_file,
		ContentType: &mediaType,
	}

	_, err = cfg.s3Client.PutObject(r.Context(), &s3c_params)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to put object to s3", err)
		return
	}

	new_url := fmt.Sprintf("https://%v.s3.%v.amazonaws.com/%v", cfg.s3Bucket, cfg.s3Region, file_key)

	vid_mdata.VideoURL = &new_url

	err = cfg.db.UpdateVideo(vid_mdata)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update the database", err)
		return
	}
}
