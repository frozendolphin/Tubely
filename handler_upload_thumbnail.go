package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
	r.ParseMultipartForm(maxMemory)

	file, fheader, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	f_ctype := fheader.Header.Get("Content-Type")

	vid_mdata, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Unable to find video in database", err)
		return
	}

	if userID != vid_mdata.UserID {
		respondWithError(w, http.StatusUnauthorized, "not allowed to upload thumbnail for other users", err)
		return
	}

	mediaType, _, err := mime.ParseMediaType(f_ctype)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Content-Type", err)
		return
	}
	if mediaType != "image/jpeg" && mediaType != "image/png" {
		respondWithError(w, http.StatusBadRequest, "Invalid file type", nil)
		return
	}

	ct := strings.Split(mediaType, "/")

	key := make([]byte, 32)
	rand.Read(key)
	encodedString := base64.RawURLEncoding.EncodeToString(key)

	new_file_path := filepath.Join(cfg.assetsRoot, fmt.Sprintf("%v.%v", encodedString, ct[1]))

	created_file, err := os.Create(new_file_path)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create file path", err)
		return
	}
	defer created_file.Close()

	_, err = io.Copy(created_file, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to copy contents to new file", err)
		return
	}

	new_url := fmt.Sprintf("http://localhost:%v/assets/%v.%v", cfg.port, encodedString, ct[1])

	vid_mdata.ThumbnailURL = &new_url

	err = cfg.db.UpdateVideo(vid_mdata)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update the database", err)
		return
	}

	respondWithJSON(w, http.StatusOK, vid_mdata)
}
