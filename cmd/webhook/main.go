package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"syscall"

	"github.com/stoutes/k8s-admission-webhook/internal/controller"
	"github.com/stoutes/k8s-admission-webhook/internal/mutate"
	"github.com/stoutes/k8s-admission-webhook/internal/validate"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var scheme = runtime.NewScheme()

func init() {
	_ = corev1.AddToScheme(scheme)
}

func main() {
	var (
		certFile    = flag.String("tls-cert", "/etc/webhook/certs/tls.crt", "TLS certificate file")
		keyFile     = flag.String("tls-key", "/etc/webhook/certs/tls.key", "TLS key file")
		webhookPort = flag.Int("webhook-port", 8443, "Webhook HTTPS port")
		metricsAddr = flag.String("metrics-addr", ":8080", "Controller metrics address")
		probeAddr   = flag.String("probe-addr", ":8081", "Controller health probe address")
		leaderElect = flag.Bool("leader-elect", true, "Enable leader election for controller")
	)
	flag.Parse()

	ctrllog.SetLogger(zap.New(zap.UseDevMode(false)))
	log := ctrl.Log.WithName("main")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	// --- Controller manager ---
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: *metricsAddr,
		},
		HealthProbeBindAddress: *probeAddr,
		LeaderElection:         *leaderElect,
		LeaderElectionID:       "admission-webhook.webhook-system",
	})
	if err != nil {
		log.Error(err, "unable to create manager")
		os.Exit(1)
	}

	if err := (&controller.InjectedPodReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		log.Error(err, "unable to set up injected pod controller")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// --- Webhook HTTP server (runs alongside the manager) ---
	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", mutate.Handler(ctrl.Log.WithName("webhook")))
	mux.HandleFunc("/validate", validate.Handler(ctrl.Log.WithName("webhook")))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	webhookServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", *webhookPort),
		Handler: mux,
	}

	// Run webhook server in a goroutine so the manager runs in the main goroutine.
	go func() {
		log.Info("starting webhook server", "port", *webhookPort)
		if err := webhookServer.ListenAndServeTLS(*certFile, *keyFile); err != nil && err != http.ErrServerClosed {
			log.Error(err, "webhook server failed")
			cancel()
		}
	}()

	// Shut webhook down cleanly when context is cancelled.
	go func() {
		<-ctx.Done()
		log.Info("shutting down webhook server")
		_ = webhookServer.Shutdown(context.Background())
	}()

	log.Info("starting controller manager")
	if err := mgr.Start(ctx); err != nil {
		log.Error(err, "manager exited with error")
		os.Exit(1)
	}
}
