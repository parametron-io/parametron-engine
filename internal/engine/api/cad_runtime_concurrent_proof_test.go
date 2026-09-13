package api

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"testing"

	"parametron/internal/engine/artifact"
)

// TestCADRuntimeConcurrentProofServer is a subprocess entry point for the Python
// proof. Only worker configuration differs from the normal API server.
func TestCADRuntimeConcurrentProofServer(t *testing.T) {
	addr := os.Getenv("PARAMETRON_CAD_PROOF_SERVER_ADDR")
	if addr == "" {
		t.Skip("opt-in Python concurrent proof server")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := Server{Addr: addr, Output: os.Stdout, Handler: newHandler(HandlerOptions{
		ArtifactStore: artifact.NewFileSystemStore(os.Getenv("PARAMETRON_CAD_PROOF_ARTIFACT_DIR")),
		WorkerCount:   3,
	})}
	if err := server.Run(ctx); err != nil {
		t.Fatal(err)
	}
}
