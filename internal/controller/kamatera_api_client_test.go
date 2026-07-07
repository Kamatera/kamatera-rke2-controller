package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/mock"
)

type kamateraClientMock struct {
	mock.Mock
}

func (c *kamateraClientMock) IsServerRunning(ctx context.Context, name string) (bool, error) {
	args := c.Called(ctx, name)
	return args.Get(0).(bool), args.Error(1)
}

func (c *kamateraClientMock) ListServers(ctx context.Context) ([]KamateraServer, error) {
	args := c.Called(ctx)
	servers, _ := args.Get(0).([]KamateraServer)
	return servers, args.Error(1)
}

func TestBuildKamateraAPIClientReturnsClient(t *testing.T) {
	client := BuildKamateraAPIClient("client-id", "secret", "https://example.invalid")
	if client == nil {
		t.Fatalf("expected Kamatera API client")
	}
}

func TestKamateraApiClientRestListServersParsesValidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/service/servers" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("AuthClientId") != "client-id" || r.Header.Get("AuthSecret") != "secret" {
			t.Fatalf("unexpected auth headers")
		}
		fmt.Fprint(w, `[{"name":"node-1","datacenter":"EU","power":"on"},{"name":"node-2","datacenter":"US","power":"off"}]`)
	}))
	defer server.Close()

	client := NewKamateraApiClientRest("client-id", "secret", server.URL)
	client.maxRetries = 1
	servers, err := client.ListServers(context.Background())
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}

	want := []KamateraServer{{Name: "node-1", Datacenter: "EU", Power: "on"}, {Name: "node-2", Datacenter: "US", Power: "off"}}
	if len(servers) != len(want) {
		t.Fatalf("expected %d servers, got %+v", len(want), servers)
	}
	for i := range want {
		if servers[i] != want[i] {
			t.Fatalf("expected servers %+v, got %+v", want, servers)
		}
	}
}

func TestKamateraApiClientRestListServersRejectsInvalidResponseShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"servers":[]}`)
	}))
	defer server.Close()

	client := NewKamateraApiClientRest("client-id", "secret", server.URL)
	client.maxRetries = 1
	_, err := client.ListServers(context.Background())
	if err == nil {
		t.Fatalf("expected invalid response shape error")
	}
}
