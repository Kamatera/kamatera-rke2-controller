package controller

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const defaultNodeDeletePollInterval = time.Minute

type NodeDeletePoller struct {
	client.Client
	ServerStore *ServerStateStore
	Matcher     NameMatcher

	PollInterval                 time.Duration
	NotReadyDuration             time.Duration
	ServerRunningRecheckInterval time.Duration
	AllowControlPlane            bool
	Now                          func() time.Time

	Log logr.Logger
}

func (p *NodeDeletePoller) Start(ctx context.Context) error {
	if p.Log.GetSink() == nil {
		p.Log = ctrl.Log.WithName("controllers").WithName("NodeDeletePoller")
	}
	ticker := time.NewTicker(p.interval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := p.poll(ctx); err != nil {
				p.Log.Error(err, "failed to poll nodes for deletion")
			}
		}
	}
}

func (p *NodeDeletePoller) NeedLeaderElection() bool {
	return true
}

func (p *NodeDeletePoller) interval() time.Duration {
	if p.PollInterval <= 0 {
		return defaultNodeDeletePollInterval
	}
	return p.PollInterval
}

func (p *NodeDeletePoller) poll(ctx context.Context) error {
	var nodes corev1.NodeList
	if err := p.List(ctx, &nodes); err != nil {
		return err
	}
	for i := range nodes.Items {
		_, err := (&NodeReconciler{
			Client:                       p.Client,
			NotReadyDuration:             p.NotReadyDuration,
			ServerRunningRecheckInterval: p.ServerRunningRecheckInterval,
			AllowControlPlane:            p.AllowControlPlane,
			Now:                          p.Now,
			Log:                          p.Log,
			ServerStore:                  p.ServerStore,
			Matcher:                      p.Matcher,
		}).Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKey{Name: nodes.Items[i].Name}})
		if err != nil {
			return err
		}
	}
	return nil
}
