package llm

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/user/inboxql/internal/store"
)

// ManagedProcess tracks a local server process started by InboxQL.
type ManagedProcess struct {
	Provider   string    `json:"provider"`
	LaunchMode string    `json:"launchMode"`
	PID        int       `json:"pid"`
	StartedAt  time.Time `json:"startedAt"`
	LogPath    string    `json:"logPath"`
	cmd        *exec.Cmd
}

type processManager struct {
	mu        sync.Mutex
	processes map[string]*ManagedProcess
}

var globalManager = &processManager{
	processes: make(map[string]*ManagedProcess),
}

// GetProcessManager returns the global process manager instance.
func GetProcessManager() *processManager {
	return globalManager
}

// StartServer starts a local runtime (swama or ollama) in the background.
func (m *processManager) StartServer(ctx context.Context, provider, launchMode, dataDir string) (*ManagedProcess, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if provider != ProviderSwama && provider != ProviderOllama {
		return nil, fmt.Errorf("unsupported provider for local execution: %q", provider)
	}

	// 1. Probe if already running
	healthURL := DefaultEndpoints[provider]
	if provider == ProviderSwama {
		healthURL = "http://localhost:28100/v1/models"
	} else if provider == ProviderOllama {
		healthURL = "http://localhost:11434/api/tags"
	}

	if ProbeEndpoint(ctx, healthURL) {
		if existing, ok := m.processes[provider]; ok {
			return existing, nil
		}
		// Running externally
		return &ManagedProcess{
			Provider:   provider,
			LaunchMode: launchMode,
			PID:        findPIDForPort(provider),
			StartedAt:  time.Now(),
		}, nil
	}

	// 2. Discover binary
	detectorInfo := DetectRuntimes(ctx)
	var runtimeEntry *RuntimeInfo
	for i := range detectorInfo {
		if detectorInfo[i].Provider == provider {
			runtimeEntry = &detectorInfo[i]
			break
		}
	}

	if runtimeEntry == nil || (!runtimeEntry.Installed && runtimeEntry.BinaryPath == "") {
		return nil, fmt.Errorf("%s is not installed on this system", provider)
	}

	if launchMode == "" {
		launchMode = "headless"
	}

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to prepare data directory: %w", err)
	}
	logPath := filepath.Join(dataDir, "llm-"+provider+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file %s: %w", logPath, err)
	}

	var cmd *exec.Cmd
	switch provider {
	case ProviderSwama:
		if launchMode == "menubar" {
			cmd = exec.Command(runtimeEntry.BinaryPath, "menubar")
		} else {
			cmd = exec.Command(runtimeEntry.BinaryPath, "serve", "--port", "28100")
		}
	case ProviderOllama:
		if launchMode == "app" {
			cmd = exec.Command("open", "-a", "Ollama")
		} else {
			cmd = exec.Command(runtimeEntry.BinaryPath, "serve")
		}
	}

	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("failed to start %s: %w", provider, err)
	}

	pid := cmd.Process.Pid
	proc := &ManagedProcess{
		Provider:   provider,
		LaunchMode: launchMode,
		PID:        pid,
		StartedAt:  time.Now(),
		LogPath:    logPath,
		cmd:        cmd,
	}
	m.processes[provider] = proc

	// Monitor child in background
	go func(p *ManagedProcess) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Recovered in process monitor: %v", r)
			}
		}()
		_ = p.cmd.Wait()
		logFile.Close()
		m.mu.Lock()
		if curr, ok := m.processes[p.Provider]; ok && curr.PID == p.PID {
			delete(m.processes, p.Provider)
		}
		m.mu.Unlock()
	}(proc)

	// Poll until ready (up to 8s)
	ready := false
	for i := 0; i < 32; i++ {
		time.Sleep(250 * time.Millisecond)
		if ProbeEndpoint(ctx, healthURL) {
			ready = true
			break
		}
	}

	if !ready {
		// Read tail of log file to report helpful diagnostic
		snippet := readLogTail(logPath, 5)
		if snippet != "" {
			return proc, fmt.Errorf("%s started (PID %d) but failed to respond on %s. Log snippet:\n%s",
				provider, pid, healthURL, snippet)
		}
		return proc, fmt.Errorf("%s started (PID %d) but timed out waiting for %s", provider, pid, healthURL)
	}

	return proc, nil
}

// StopServer terminates a running local server.
func (m *processManager) StopServer(provider string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. Managed process
	if proc, ok := m.processes[provider]; ok && proc.cmd != nil && proc.cmd.Process != nil {
		_ = proc.cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() {
			time.Sleep(2 * time.Second)
			close(done)
		}()
		<-done
		_ = proc.cmd.Process.Kill()
		delete(m.processes, provider)
		return nil
	}

	// 2. Fallback: find PID for port and terminate
	pid := findPIDForPort(provider)
	if pid > 0 {
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return fmt.Errorf("failed to terminate PID %d: %w", pid, err)
		}
		return nil
	}

	return fmt.Errorf("no running process detected for %s", provider)
}

// GetStatus returns the process status for provider.
func (m *processManager) GetStatus(provider string) *ManagedProcess {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.processes[provider]
}

// AutoStartIfConfigured starts the local server if llm_auto_start is true.
func AutoStartIfConfigured(ctx context.Context, dataDir string) error {
	cfg, err := store.GetLLMConfig()
	if err != nil || !cfg.AutoStart {
		return nil
	}

	if cfg.Provider != ProviderSwama && cfg.Provider != ProviderOllama {
		return nil
	}

	log.Printf("[llm] Auto-start configured for %s (%s)", cfg.Provider, cfg.LaunchMode)
	_, err = globalManager.StartServer(ctx, cfg.Provider, cfg.LaunchMode, dataDir)
	if err != nil {
		log.Printf("[llm] Auto-start failed for %s: %v", cfg.Provider, err)
		return err
	}
	log.Printf("[llm] %s is running and ready.", cfg.Provider)
	return nil
}

func findPIDForPort(provider string) int {
	port := "28100"
	if provider == ProviderOllama {
		port = "11434"
	}
	cmd := exec.Command("lsof", "-ti", ":"+port)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 0 {
		if pid, err := strconv.Atoi(lines[0]); err == nil {
			return pid
		}
	}
	return 0
}

func readLogTail(path string, lines int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	allLines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(allLines) <= lines {
		return strings.Join(allLines, "\n")
	}
	return strings.Join(allLines[len(allLines)-lines:], "\n")
}

// TestCompletion sends a probe prompt to verify connectivity and measures latency.
func TestCompletion(ctx context.Context, cfg store.LLMConfig) (map[string]any, error) {
	provider, err := New(cfg)
	if err != nil {
		return nil, err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	start := time.Now()
	reply, err := provider.Complete(timeoutCtx,
		"You are a health check. Reply with exactly the word: ok",
		"Reply with exactly the word: ok",
	)
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"ok":        true,
		"provider":  provider.Name(),
		"elapsedMs": elapsed.Milliseconds(),
		"reply":     strings.TrimSpace(reply),
	}, nil
}
