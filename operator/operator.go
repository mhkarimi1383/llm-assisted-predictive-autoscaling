// Operator entry point: the controller-runtime manager hosting the
// LLMAutoscaler reconciler. This is the only component that connects
// to all backing services (through helpers.ServiceClients) and to the
// Kubernetes API.
package operator

import (
	"context"
	"fmt"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	autoscalingaiv1alpha1 "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/operator/api/v1alpha1"

	config "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/config"
	helpers "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/helpers"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(autoscalingaiv1alpha1.AddToScheme(scheme))
}

// Options configures the operator run.
type Options struct {
	// MetricsAddr binds the controller-runtime metrics endpoint;
	// "0" disables it.
	MetricsAddr string
	// ProbeAddr binds the health endpoints.
	ProbeAddr string
	// WatchNamespace restricts watching to one namespace; empty
	// watches all namespaces.
	WatchNamespace string
	// RequeueAfter is the control period between decision cycles.
	RequeueAfter time.Duration
}

// Start runs the operator until ctx is cancelled.
func Start(ctx context.Context, cfg config.Config, opts Options) error {
	// SetLogger must receive a real logger implementation; passing
	// ctrl.Log (a DelegatingLogSink) creates a forwarding loop that
	// explodes on the first log call.
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	logger := ctrl.Log.WithName("operator")

	managerOpts := ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: opts.MetricsAddr,
		},
		HealthProbeBindAddress: opts.ProbeAddr,
		LeaderElection:         false,
	}
	if opts.WatchNamespace != "" {
		managerOpts.Cache = cache.Options{
			DefaultNamespaces: map[string]cache.Config{opts.WatchNamespace: {}},
		}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), managerOpts)
	if err != nil {
		return fmt.Errorf("failed to create manager: %w", err)
	}

	// Connect to the backing services through the shared registry.
	services, err := helpers.NewServiceClients(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to dial backing services: %w", err)
	}
	defer services.Close()

	// Holder identity for the per-workload scaling leases.
	holder, err := os.Hostname()
	if err != nil || holder == "" {
		holder = "operator"
	}

	reconciler := &LLMAutoscalerReconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		Services:     services,
		Ownership:    NewScalingOwnership(mgr.GetAPIReader(), mgr.GetClient(), holder),
		RequeueAfter: opts.RequeueAfter,
	}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("failed to set up controller: %w", err)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("failed to add healthz check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("failed to add readyz check: %w", err)
	}

	logger.Info("starting operator manager")
	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("manager exited: %w", err)
	}
	return nil
}
