// Command headless brings up Mead's MCP bridge WITHOUT the Wails GUI.
//
// It constructs the exact same meadcore.Core + bridge the GUI wires in
// app.go, so external MCP clients (e.g. the spike harness in
// ~/mead-scratch/spike) can drive it headlessly. Throwaway dev tool —
// not part of the v0.1 product surface; untracked.
//
//	go run ./cmd/headless        # Ctrl-C to stop
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/StephenSHorton/mead/internal/bridge"
	"github.com/StephenSHorton/mead/internal/meadcore"
)

func main() {
	c, err := meadcore.New()
	if err != nil {
		// meadcore.New degrades gracefully on store failures; log + continue.
		log.Printf("meadcore.New: %v", err)
	}

	b := bridge.New(bridge.Config{AppName: "mead"})
	meadcore.RegisterAll(b, c)

	if err := b.Start(); err != nil {
		log.Fatalf("bridge start: %v", err)
	}
	log.Printf("MCP bridge listening on 127.0.0.1:%d (token %s…) — Ctrl-C to stop",
		b.Port(), b.TokenShort())

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	log.Printf("shutting down bridge")
	b.Stop()
}
