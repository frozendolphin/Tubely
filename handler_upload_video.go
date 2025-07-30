package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"

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

	ratio, err := getVideoAspectRatio(tmp_file.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to get the aspect ratio of file", err)
		return
	}
	
	name_ratio := getAspectRatioName(ratio)
	
	_, err = tmp_file.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not reset file pointer", err)
		return
	}

	ct := strings.Split(mediaType, "/")

	key := make([]byte, 32)
	rand.Read(key)
	encodedString := base64.RawURLEncoding.EncodeToString(key)

	file_key := fmt.Sprintf("%v/%v.%v", name_ratio, encodedString, ct[1])

	path, err := processVideoForFastStart(tmp_file.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to process the video for fast start", err)
		return
	}

	processed_file, err := os.Open(path)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to read the processed data", err)
		return
	}
	defer os.Remove(path)
	defer processed_file.Close()

	s3c_params := s3.PutObjectInput {
		Bucket: &cfg.s3Bucket,
		Key: &file_key,
		Body: processed_file,
		ContentType: &mediaType,
	}

	_, err = cfg.s3Client.PutObject(r.Context(), &s3c_params)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to put object to s3", err)
		return
	}

	new_url := fmt.Sprintf("%v,%v", cfg.s3Bucket, file_key)
	vid_mdata.VideoURL = &new_url

	err = cfg.db.UpdateVideo(vid_mdata)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update the database", err)
		return
	}

	new_video, err := cfg.dbVideoToSignedVideo(vid_mdata)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to make dbvideo to signed video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, new_video)
}


func processVideoForFastStart(filePath string) (string, error) {

	new_path := fmt.Sprintf("%v.processing", filePath)

	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", new_path)

	err := cmd.Run()
	if err != nil {
		return "", err
	}

	return new_path, nil
}

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	
	presign_client := s3.NewPresignClient(s3Client)

	params := s3.GetObjectInput {
		Bucket: &bucket,
		Key: &key,
	}

	req, err := presign_client.PresignGetObject(context.Background(), &params, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", err
	}

	return req.URL, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {

	if video.VideoURL == nil {
		return video, nil
	}

	splitted_url := strings.Split(*video.VideoURL, ",")

	if len(splitted_url) < 2 {
		return video, nil
	}

	bucket := splitted_url[0]
	key := splitted_url[1]

	presign_url, err := generatePresignedURL(cfg.s3Client, bucket, key, 5 * time.Minute) 
	if err != nil {
		return video, err
	}

	video.VideoURL = &presign_url

	return video, nil
}