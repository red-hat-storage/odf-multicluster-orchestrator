package setup

import (
	"context"
	"log/slog"
	"net"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/healthz"
)

const (
	MaintainAgentName = "maintenance"
	TokenExchangeName = "tokenexchange"
)

// ServeHealthProbes starts a server to check healthz and readyz probes
func ServeHealthProbes(stop <-chan struct{}, healthProbeBindAddress string, configCheck healthz.Checker, logger *slog.Logger) {
	healthzHandler := &healthz.Handler{Checks: map[string]healthz.Checker{
		"healthz-ping": healthz.Ping,
		"configz-ping": configCheck,
	}}
	readyzHandler := &healthz.Handler{Checks: map[string]healthz.Checker{
		"readyz-ping": healthz.Ping,
	}}

	mux := http.NewServeMux()
	mux.Handle("/readyz", http.StripPrefix("/readyz", readyzHandler))
	mux.Handle("/healthz", http.StripPrefix("/healthz", healthzHandler))

	server := http.Server{
		Handler: mux,
	}

	ln, err := net.Listen("tcp", healthProbeBindAddress)
	if err != nil {
		logger.Error("Failed to listen on address", "address", healthProbeBindAddress, "error", err)
		return
	}
	logger.Info("Server listening", "address", healthProbeBindAddress)

	// Run server in a goroutine
	go func() {
		err := server.Serve(ln)
		if err != nil && err != http.ErrServerClosed {
			logger.Error("Server failed", "error", err)
		}
	}()

	logger.Info("Health probes server is running")

	// Shutdown the server when stop is closed
	<-stop
	if err := server.Shutdown(context.Background()); err != nil {
		logger.Error("Failed to shutdown server", "error", err)
	} else {
		logger.Info("Server shutdown successfully")
	}
}
