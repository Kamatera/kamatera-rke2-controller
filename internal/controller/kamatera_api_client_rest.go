package controller

import (
	"context"
	"fmt"
)

const (
	userAgent = "kamatera/kamatera-rke2-controller"
)

// NewKamateraApiClientRest factory to create new Rest API Client struct
func NewKamateraApiClientRest(clientId string, secret string, url string) (client KamateraApiClientRest) {
	return KamateraApiClientRest{
		userAgent:                userAgent,
		clientId:                 clientId,
		secret:                   secret,
		url:                      url,
		maxRetries:               5,
		expSecondsBetweenRetries: 1,
	}
}

// KamateraApiClientRest is the struct to perform API calls
type KamateraApiClientRest struct {
	userAgent                string
	clientId                 string
	secret                   string
	url                      string
	maxRetries               int
	expSecondsBetweenRetries int
}

type KamateraServerPostRequest struct {
	ServerName string `json:"name"`
}

func (c *KamateraApiClientRest) IsServerRunning(ctx context.Context, name string) (bool, error) {
	gotErrorMessage, res, err := request(
		ctx,
		ProviderConfig{ApiUrl: c.url, ApiClientID: c.clientId, ApiSecret: c.secret},
		"POST",
		"/service/server/info",
		KamateraServerPostRequest{ServerName: name},
		c.maxRetries,
		c.expSecondsBetweenRetries,
		"No servers found",
	)
	if err != nil {
		return false, err
	}
	if gotErrorMessage {
		return false, nil
	}
	serverInfoList, ok := res.([]interface{})
	if !ok || len(serverInfoList) == 0 {
		return false, nil
	}
	if len(serverInfoList) != 1 {
		return false, fmt.Errorf("expected one server info, got %d", len(serverInfoList))
	}
	serverInfo, ok := serverInfoList[0].(map[string]interface{})
	if !ok {
		return false, fmt.Errorf("invalid server info format")
	}
	powerState, ok := serverInfo["power"].(string)
	if !ok {
		return false, fmt.Errorf("invalid power state format")
	}
	return powerState == "on", nil
}
