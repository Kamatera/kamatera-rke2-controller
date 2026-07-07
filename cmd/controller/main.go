package main

import (
	"flag"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	nodecontroller "github.com/kamatera/kamatera-rke2-controller/internal/controller"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var healthProbeAddr string
	var enableLeaderElection bool
	var leaderElectionID string
	var notReadyDuration time.Duration
	var allowControlPlane bool
	var kamateraServerListInterval time.Duration
	var nodeDeletePollInterval time.Duration
	var snapshotsLogInterval time.Duration
	var kamateraServerDatacenters string
	var kamateraServerNameGlob string
	var nodeTrackedTaints string
	var nodeTrackedAnnotations string
	var matchNodeToServerTemplate string
	var matchServerToNodeTemplate string

	zapOpts := zap.Options{Development: false}
	zapOpts.BindFlags(flag.CommandLine)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&healthProbeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")
	flag.StringVar(&leaderElectionID, "leader-election-id", "kamatera-rke2-controller.kamatera.io", "Leader election ID to use for the controller manager.")

	flag.DurationVar(&notReadyDuration, "not-ready-duration", 15*time.Minute, "Minimum time a Node must be NotReady before deletion is considered.")
	flag.BoolVar(&allowControlPlane, "allow-control-plane", false, "Allow deleting nodes labeled as control-plane/master/etcd.")
	flag.DurationVar(&kamateraServerListInterval, "kamatera-server-list-interval", time.Minute, "Interval for polling Kamatera server list.")
	flag.DurationVar(&nodeDeletePollInterval, "node-delete-poll-interval", time.Minute, "Interval for polling Kubernetes Nodes for deletion eligibility.")
	flag.DurationVar(&snapshotsLogInterval, "snapshots-log-interval", time.Minute, "Interval for logging current node and Kamatera server snapshots.")
	flag.StringVar(&kamateraServerDatacenters, "kamatera-server-datacenters", "", "Comma-separated Kamatera datacenters to include. Empty includes all datacenters.")
	flag.StringVar(&kamateraServerNameGlob, "kamatera-server-name-glob", "", "Glob pattern for Kamatera server names. Empty includes all names.")
	flag.StringVar(&nodeTrackedTaints, "node-tracked-taints", nodecontroller.DefaultTrackedTaintsCSV(), "Comma-separated node taint keys to track in node snapshots.")
	flag.StringVar(&nodeTrackedAnnotations, "node-tracked-annotations", "", "Comma-separated node annotation keys to track in node snapshots.")
	flag.StringVar(&matchNodeToServerTemplate, "match-node-to-server-template", "", "Template applied to Node name to produce matching Kamatera server name. Must contain exactly one %s. Mutually exclusive with --match-server-to-node-template.")
	flag.StringVar(&matchServerToNodeTemplate, "match-server-to-node-template", "", "Template applied to Kamatera server name to produce matching Node name. Must contain exactly one %s. Mutually exclusive with --match-node-to-server-template.")

	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOpts)))
	setupLog := ctrl.Log.WithName("setup")

	if notReadyDuration <= 0 {
		setupLog.Error(nil, "--not-ready-duration must be greater than 0")
		os.Exit(1)
	}
	if kamateraServerListInterval <= 0 {
		setupLog.Error(nil, "--kamatera-server-list-interval must be greater than 0")
		os.Exit(1)
	}
	if nodeDeletePollInterval <= 0 {
		setupLog.Error(nil, "--node-delete-poll-interval must be greater than 0")
		os.Exit(1)
	}
	if snapshotsLogInterval <= 0 {
		setupLog.Error(nil, "--snapshots-log-interval must be greater than 0")
		os.Exit(1)
	}
	serverFilter, err := nodecontroller.NewServerFilter(kamateraServerDatacenters, kamateraServerNameGlob)
	if err != nil {
		setupLog.Error(err, "invalid --kamatera-server-name-glob")
		os.Exit(1)
	}
	matcher, err := nodecontroller.NewNameMatcher(matchNodeToServerTemplate, matchServerToNodeTemplate)
	if err != nil {
		setupLog.Error(err, "invalid node/server matching configuration")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: healthProbeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       leaderElectionID,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	serverStore := nodecontroller.NewServerStateStore()
	nodeStore := nodecontroller.NewNodeStateStore()
	kamateraApiUrl := os.Getenv("KAMATERA_API_URL")
	if kamateraApiUrl == "" {
		kamateraApiUrl = "https://cloudcli.cloudwm.com"
	}
	kamateraClient := nodecontroller.BuildKamateraAPIClient(
		os.Getenv("KAMATERA_API_CLIENT_ID"),
		os.Getenv("KAMATERA_API_SECRET"),
		kamateraApiUrl,
	)

	if err := mgr.Add(&nodecontroller.KamateraServersController{
		Client:    kamateraClient,
		Store:     serverStore,
		NodeStore: nodeStore,
		Matcher:   matcher,
		Filter:    serverFilter,
		Interval:  kamateraServerListInterval,
		Log:       ctrl.Log.WithName("controllers").WithName("KamateraServers"),
	}); err != nil {
		setupLog.Error(err, "unable to add controller", "controller", "KamateraServers")
		os.Exit(1)
	}

	if err := (&nodecontroller.NodeListReconciler{
		Client:             mgr.GetClient(),
		Store:              nodeStore,
		ServerStore:        serverStore,
		Matcher:            matcher,
		TrackedTaints:      nodecontroller.ParseTrackedKeys(nodeTrackedTaints),
		TrackedAnnotations: nodecontroller.ParseTrackedKeys(nodeTrackedAnnotations),
		Log:                ctrl.Log.WithName("controllers").WithName("NodeList"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "NodeList")
		os.Exit(1)
	}

	if err := mgr.Add(&nodecontroller.NodeDeletePoller{
		Client:            mgr.GetClient(),
		ServerStore:       serverStore,
		Matcher:           matcher,
		PollInterval:      nodeDeletePollInterval,
		NotReadyDuration:  notReadyDuration,
		AllowControlPlane: allowControlPlane,
		Log:               ctrl.Log.WithName("controllers").WithName("NodeDeletePoller"),
	}); err != nil {
		setupLog.Error(err, "unable to add controller", "controller", "NodeDeletePoller")
		os.Exit(1)
	}

	if err := mgr.Add(&nodecontroller.SnapshotLogger{
		ServerStore: serverStore,
		NodeStore:   nodeStore,
		Matcher:     matcher,
		Interval:    snapshotsLogInterval,
		Log:         ctrl.Log.WithName("controllers").WithName("SnapshotLogger"),
	}); err != nil {
		setupLog.Error(err, "unable to add controller", "controller", "SnapshotLogger")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
