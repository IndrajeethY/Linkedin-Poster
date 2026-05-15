package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

const (
	linkedInBaseURL = "https://api.linkedin.com"
	linkedInVersion = "202401"
)

func setLinkedInHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+config.LinkedInToken)
	req.Header.Set("LinkedIn-Version", linkedInVersion)
	req.Header.Set("X-Restli-Protocol-Version", "2.0.0")
	req.Header.Set("Content-Type", "application/json")
}

func validateLinkedIn() error {
	if config.LinkedInToken == "" {
		return fmt.Errorf("LINKEDIN_TOKEN not configured — set it in .env")
	}
	if config.LinkedInAuthor == "" {
		return fmt.Errorf("AUTHOR_ID not configured — set it in .env")
	}
	return nil
}

func PostToLinkedIn(text string) (string, error) {
	if err := validateLinkedIn(); err != nil {
		return "", err
	}

	payload := map[string]interface{}{
		"author":         fmt.Sprintf("urn:li:person:%s", config.LinkedInAuthor),
		"commentary":     text,
		"visibility":     "PUBLIC",
		"distribution": map[string]interface{}{
			"feedDistribution":               "MAIN_FEED",
			"targetEntities":                 []interface{}{},
			"thirdPartyDistributionChannels": []interface{}{},
		},
		"lifecycleState": "PUBLISHED",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", linkedInBaseURL+"/rest/posts", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	setLinkedInHeaders(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("linkedin request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("linkedin API error (%d): %s", resp.StatusCode, body)
	}

	postURN := resp.Header.Get("X-Restli-Id")
	if postURN == "" {
		postURN = resp.Header.Get("x-restli-id")
	}
	return fmt.Sprintf("https://www.linkedin.com/feed/update/%s", postURN), nil
}

func InitializeImageUpload() (uploadURL string, imageURN string, err error) {
	if err := validateLinkedIn(); err != nil {
		return "", "", err
	}

	payload := map[string]interface{}{
		"initializeUploadRequest": map[string]interface{}{
			"owner": fmt.Sprintf("urn:li:person:%s", config.LinkedInAuthor),
		},
	}

	jsonData, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", linkedInBaseURL+"/rest/images?action=initializeUpload", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", "", fmt.Errorf("create request: %w", err)
	}
	setLinkedInHeaders(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("linkedin request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("linkedin image init error (%d): %s", resp.StatusCode, body)
	}

	var result struct {
		Value struct {
			UploadURL string `json:"uploadUrl"`
			Image     string `json:"image"`
		} `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("decode response: %w", err)
	}

	return result.Value.UploadURL, result.Value.Image, nil
}

func UploadImage(uploadURL, imagePath string) error {
	file, err := os.Open(imagePath)
	if err != nil {
		return fmt.Errorf("open image: %w", err)
	}
	defer file.Close()

	req, err := http.NewRequest("PUT", uploadURL, file)
	if err != nil {
		return fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+config.LinkedInToken)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("image upload error (%d): %s", resp.StatusCode, body)
	}

	return nil
}

func PostToLinkedInWithImage(text, imageURN string) (string, error) {
	if err := validateLinkedIn(); err != nil {
		return "", err
	}

	payload := map[string]interface{}{
		"author":         fmt.Sprintf("urn:li:person:%s", config.LinkedInAuthor),
		"commentary":     text,
		"visibility":     "PUBLIC",
		"distribution": map[string]interface{}{
			"feedDistribution":               "MAIN_FEED",
			"targetEntities":                 []interface{}{},
			"thirdPartyDistributionChannels": []interface{}{},
		},
		"content": map[string]interface{}{
			"media": map[string]interface{}{
				"id": imageURN,
			},
		},
		"lifecycleState": "PUBLISHED",
	}

	jsonData, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", linkedInBaseURL+"/rest/posts", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	setLinkedInHeaders(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("linkedin request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("linkedin API error (%d): %s", resp.StatusCode, body)
	}

	postURN := resp.Header.Get("X-Restli-Id")
	if postURN == "" {
		postURN = resp.Header.Get("x-restli-id")
	}
	return fmt.Sprintf("https://www.linkedin.com/feed/update/%s", postURN), nil
}
