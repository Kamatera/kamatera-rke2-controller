package controller

import (
	"context"

	"github.com/stretchr/testify/mock"
)

type kamateraClientMock struct {
	mock.Mock
}

func (c *kamateraClientMock) IsServerRunning(ctx context.Context, name string) (bool, error) {
	args := c.Called(ctx, name)
	return args.Get(0).(bool), args.Error(1)
}
