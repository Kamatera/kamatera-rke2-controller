package controller

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
)

const defaultSnapshotsLogInterval = time.Minute

type SnapshotLogger struct {
	ServerStore *ServerStateStore
	NodeStore   *NodeStateStore
	Matcher     NameMatcher
	Interval    time.Duration
	Log         logr.Logger
}

func (l *SnapshotLogger) Start(ctx context.Context) error {
	if l.Log.GetSink() == nil {
		l.Log = ctrl.Log.WithName("controllers").WithName("SnapshotLogger")
	}
	ticker := time.NewTicker(l.interval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			l.logSnapshots()
		}
	}
}

func (l *SnapshotLogger) NeedLeaderElection() bool {
	return true
}

func (l *SnapshotLogger) interval() time.Duration {
	if l.Interval <= 0 {
		return defaultSnapshotsLogInterval
	}
	return l.Interval
}

func (l *SnapshotLogger) logSnapshots() {
	if l.ServerStore == nil || l.NodeStore == nil {
		return
	}
	loggedServers := map[string]struct{}{}
	for _, node := range l.NodeStore.List() {
		server, matched := l.Matcher.FindServerForNode(node.Name, l.ServerStore)
		if matched {
			loggedServers[serverStateKey(server)] = struct{}{}
			l.Log.Info("snapshot node/server match", "nodeName", node.Name, "nodeReady", node.Ready, "serverName", server.Name, "serverPower", server.Power)
			continue
		}
		l.Log.Info("snapshot node unmatched", "nodeName", node.Name, "nodeReady", node.Ready)
	}
	for _, server := range l.ServerStore.List() {
		if _, ok := loggedServers[serverStateKey(server)]; ok {
			continue
		}
		l.Log.Info("snapshot server unmatched", "serverName", server.Name, "serverPower", server.Power)
	}
}
