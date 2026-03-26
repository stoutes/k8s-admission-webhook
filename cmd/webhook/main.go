package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/yourorg/k8s-admission-webhook/internal/mutate"
	"github.com/yourorg/k8s-admission-webhook/internal/validate"
	"go.uber.org/zap"
)

func main() {
	var (
		certFile = flag.String("tls-cert", "/etc/webhook/certs/tls.crt", "TLS certificate file")
		keyFile  = flag.String("tls-key", "/etc/webhook/certs/tls.key", "TLS key file")
		port     = flag.Int("port", 8443, "HTTPS port")
	)
	flag.Parse()

	logger, _ := zap.NewProduction()
	defer logger.Sync()

	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", mutate.Handler(logger))
	mux.HandleFunc("/validate", validate.Handler(logger))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	addr := fmt.Sprintf(":%d", *port)
	logger.Info("starting webhook server", zap.String("addr", addr))

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	if err := server.ListenAndServeTLS(*certFile, *keyFile); err != nil {
		logger.Error("server failed", zap.Error(err))
		os.Exit(1)
	}
}
