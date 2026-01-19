package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestKamateraHTTPClientTimeout(t *testing.T) {
	assert.NotNil(t, kamateraHTTPClient, "kamateraHTTPClient should not be nil")
	assert.Equal(t, 5*time.Minute, kamateraHTTPClient.Timeout, "HTTP client timeout should be 5 minutes")
}
