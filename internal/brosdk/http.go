package brosdk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	defaultGetUserSigURL = "https://api.brosdk.com/api/v2/browser/getUserSig"
	defaultDuration      = 86400 // 1 day in seconds
)

// getUserSigReq is the JSON body for POST /api/v2/browser/getUserSig.
type getUserSigReq struct {
	CustomerID string `json:"customerId"`
	Duration   int    `json:"duration"`
}

// getUserSigResp is the parsed response from the getUserSig endpoint.
type getUserSigResp struct {
	Code int `json:"code"`
	Data struct {
		UserSig    string `json:"userSig"`
		ExpireTime int64  `json:"expireTime"`
	} `json:"data"`
}

// fetchUserSig calls the BroSDK API to obtain a userSig using the given apiKey.
func fetchUserSig(apiKey string) (string, error) {
	body := getUserSigReq{
		CustomerID: apiKey,
		Duration:   defaultDuration,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal getUserSig request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, defaultGetUserSigURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create getUserSig request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("getUserSig request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read getUserSig response: %w", err)
	}

	var r getUserSigResp
	if err := json.Unmarshal(respBytes, &r); err != nil {
		return "", fmt.Errorf("unmarshal getUserSig response: %w", err)
	}
	if r.Code != 200 {
		return "", fmt.Errorf("getUserSig returned code %d: %s", r.Code, string(respBytes))
	}
	if r.Data.UserSig == "" {
		return "", fmt.Errorf("getUserSig returned empty userSig")
	}
	return r.Data.UserSig, nil
}

// httpGetBytes performs a simple HTTP GET and returns the body.
func httpGetBytes(urlStr string) ([]byte, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(urlStr)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
