package main

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"os/exec"
)

func (cfg apiConfig) ensureAssetsDir() error {
	if _, err := os.Stat(cfg.assetsRoot); os.IsNotExist(err) {
		return os.Mkdir(cfg.assetsRoot, 0755)
	}
	return nil
}

func getVideoAspectRatio(filePath string) (string, error) {

	type ffprobeout struct {
		Streams []struct {
			Width int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}

	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)

	var out bytes.Buffer
	cmd.Stdout = &out

	err := cmd.Run()
	if err != nil {
		return "", err
	}

	var data ffprobeout

	err = json.Unmarshal(out.Bytes(), &data)
	if err != nil {
		return "", err
	}

	ratio := getAspectRatio(data.Streams[0].Width, data.Streams[0].Height, 0.01)

	return ratio, nil
}

func getAspectRatio(w, h int, tolarance float64) string {
	
	if math.Abs((float64(w)/float64(h)) - (16.0/9.0)) <= tolarance {
		return "16:9"
	}  
	
	if math.Abs((float64(w)/float64(h)) - (9.0/16.0)) <= tolarance {
		return "9:16"
	}
	
	return "other"
}

func getAspectRatioName(ratio string) string {
	
	if ratio == "16:9" {
		return "landscape"
	}
	if ratio == "9:16" {
		return "portrait"
	}
	return "other"
}