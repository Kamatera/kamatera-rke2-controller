package controller

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
)

const defaultKamateraServerListInterval = time.Minute

type KamateraServersController struct {
	Client    kamateraAPIClient
	Store     *ServerStateStore
	NodeStore *NodeStateStore
	Matcher   NameMatcher
	Filter    ServerFilter
	Interval  time.Duration

	Log logr.Logger
}

func (c *KamateraServersController) Start(ctx context.Context) error {
	if c.Log.GetSink() == nil {
		c.Log = ctrl.Log.WithName("controllers").WithName("KamateraServers")
	}
	interval := c.Interval
	if interval <= 0 {
		interval = defaultKamateraServerListInterval
	}
	if err := c.poll(ctx); err != nil {
		c.Log.Error(err, "failed to list Kamatera servers")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := c.poll(ctx); err != nil {
				c.Log.Error(err, "failed to list Kamatera servers")
			}
		}
	}
}

func (c *KamateraServersController) NeedLeaderElection() bool {
	return true
}

func (c *KamateraServersController) poll(ctx context.Context) error {
	servers, err := c.Client.ListServers(ctx)
	if err != nil {
		return err
	}
	filtered := make([]KamateraServer, 0, len(servers))
	for _, server := range servers {
		if c.Filter.Match(server) {
			filtered = append(filtered, server)
		}
	}
	diff := c.Store.Replace(filtered)
	c.logDiff(diff)
	return nil
}

func (c *KamateraServersController) logDiff(diff ServerStateDiff) {
	if diff.Initial {
		for _, server := range diff.Current {
			c.Log.Info("kamatera server observed", c.serverLogValues(server)...)
		}
		return
	}
	for _, server := range diff.Added {
		c.Log.Info("server added", c.serverLogValues(server)...)
	}
	for _, server := range diff.Removed {
		c.Log.Info("server removed", c.serverLogValues(server)...)
	}
	for _, change := range diff.PowerChanged {
		server := KamateraServer{Name: change.Name, Datacenter: change.Datacenter, Power: change.NewPower}
		c.Log.Info("server power changed", append(c.serverLogValues(server), "oldPower", change.OldPower, "newPower", change.NewPower)...)
	}
}

func (c *KamateraServersController) serverLogValues(server KamateraServer) []interface{} {
	node, matched := c.Matcher.FindNodeForServer(server.Name, c.NodeStore)
	return []interface{}{
		"name", server.Name,
		"datacenter", server.Datacenter,
		"power", server.Power,
		"matchedNode", matched,
		"nodeName", node.Name,
		"nodeReady", node.Ready,
		"nodeDeleting", node.Deleting,
		"nodeUnschedulable", node.Unschedulable,
	}
}
