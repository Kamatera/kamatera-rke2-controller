package controller

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

const defaultNodeDeletePollInterval = time.Minute

type NodeDeletePoller struct {
	NodeStore  *NodeStateStore
	Reconciler *NodeReconciler

	PollInterval time.Duration

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
	if p.NodeStore == nil {
		p.Log.Info("skipping node deletion poll because node snapshot store is unavailable")
		return nil
	}
	if p.Reconciler == nil {
		p.Log.Info("skipping node deletion poll because node reconciler is unavailable")
		return nil
	}
	for _, node := range p.NodeStore.List() {
		if err := p.Reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: node.Name}}); err != nil {
			return err
		}
	}
	return nil
}
