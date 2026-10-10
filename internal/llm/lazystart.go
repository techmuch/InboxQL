package llm

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/user/inboxql/internal/store"
)

// # Starting the local model when it is first needed
//
// `llm_auto_start` used to mean "start the local model server with the web
// server". As a login service that is every login: gigabytes of memory, and on
// Apple Silicon the GPU, held from the moment somebody opens their laptop to
// the moment they close it, whether or not anything asks a model anything that
// day.
//
// In service mode the start moves here, to the first time a provider is built
// — the moment something actually wants a model. That costs the first request
// the model's start-up time, once, which is the right person to pay it.
var lazy struct {
	sync.Mutex
	dataDir string
	started bool
}

// StartOnFirstUse switches auto-start from "at launch" to "on first use".
func StartOnFirstUse(dataDir string) {
	lazy.Lock()
	defer lazy.Unlock()
	lazy.dataDir = dataDir
}

// startIfDeferred starts the configured local server if auto-start was
// deferred and it has not been started yet. A failure is logged and retried on
// the next use rather than remembered: the next request is a better time to
// try than never.
func startIfDeferred(cfg store.LLMConfig) {
	lazy.Lock()
	defer lazy.Unlock()
	if lazy.dataDir == "" || lazy.started || !cfg.AutoStart {
		return
	}
	if cfg.Provider != ProviderSwama && cfg.Provider != ProviderOllama {
		return
	}
	if cfg.Endpoint != "" && cfg.Endpoint != DefaultEndpoints[cfg.Provider] {
		// Pointed at a server somebody else runs; not ours to start.
		lazy.started = true
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	log.Printf("[llm] starting %s on first use", cfg.Provider)
	if _, err := globalManager.StartServer(ctx, cfg.Provider, cfg.LaunchMode, lazy.dataDir); err != nil {
		log.Printf("[llm] starting %s on first use failed: %v", cfg.Provider, err)
		return
	}
	lazy.started = true
}
