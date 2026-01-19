package controller

import (
	"context"
)

// kamateraAPIClient is the interface used to call kamatera API
type kamateraAPIClient interface {
	IsServerRunning(ctx context.Context, name string) (bool, error)
}

// buildKamateraAPIClient returns the struct ready to perform calls to kamatera API
func buildKamateraAPIClient(clientId string, secret string, url string) kamateraAPIClient {
	client := NewKamateraApiClientRest(clientId, secret, url)
	return &client
}
