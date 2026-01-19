package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"k8s.io/klog/v2"
)

var kamateraHTTPClient = &http.Client{Timeout: 5 * time.Minute}

// ProviderConfig is the configuration for the Kamatera cloud provider
type ProviderConfig struct {
	ApiUrl      string
	ApiClientID string
	ApiSecret   string
}

func request(ctx context.Context, provider ProviderConfig, method string, path string, body interface{}, numRetries int, secondsBetweenRetries int, ignoreErrorMessage string) (bool, interface{}, error) {
	buf := new(bytes.Buffer)
	if body != nil {
		if err := json.NewEncoder(buf).Encode(body); err != nil {
			return false, nil, err
		}
	}
	path = strings.TrimPrefix(path, "/")
	isQueueRequest := strings.HasPrefix(path, "service/queue")
	logLevel := klog.Level(2)
	if isQueueRequest {
		logLevel = klog.Level(4)
	}
	url := fmt.Sprintf("%s/%s", provider.ApiUrl, path)
	var result interface{}
	var err error
	for attempt := 0; attempt < numRetries; attempt++ {
		if !isQueueRequest {
			klog.V(logLevel).Infof("kamatera request: %s %s %s", method, url, buf.String())
		}
		if attempt > 0 {
			if !isQueueRequest {
				klog.V(logLevel).Infof("kamatera request retry %d", attempt)
			}
			time.Sleep(time.Duration(secondsBetweenRetries<<attempt) * time.Second)
		}
		req, e := http.NewRequestWithContext(ctx, method, url, buf)
		if e != nil {
			err = e
			continue
		}
		req.Header.Add("AuthClientId", provider.ApiClientID)
		req.Header.Add("AuthSecret", provider.ApiSecret)
		req.Header.Add("Accept", "application/json")
		req.Header.Add("Content-Type", "application/json")
		res, e := kamateraHTTPClient.Do(req)
		if e != nil {
			err = e
			continue
		}
		defer res.Body.Close()
		e = json.NewDecoder(res.Body).Decode(&result)
		if e != nil {
			if res.StatusCode != 200 {
				err = fmt.Errorf("bad status code from Kamatera API: %d", res.StatusCode)
			} else {
				err = fmt.Errorf("invalid response from Kamatera API: %+v", result)
			}
			continue
		}
		if res.StatusCode == 500 && ignoreErrorMessage != "" {
			resultMap, ok := result.(map[string]interface{})
			if ok {
				message, ok := resultMap["message"].(string)
				if ok && strings.Contains(message, ignoreErrorMessage) {
					return true, result, nil
				}
			}
		}
		if res.StatusCode != 200 {
			err = fmt.Errorf("error response from Kamatera API (%d): %+v", res.StatusCode, result)
			continue
		}
		break
	}
	return false, result, err
}
