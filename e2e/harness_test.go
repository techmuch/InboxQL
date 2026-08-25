//go:build e2e

// Package e2e drives the real iql binary the way a person or a script would.
//
// Everything else in this repository tests Go functions. Nothing tested the
// program: not argument handling through main, not the HTTP server over a real
// socket, not the exit codes the AGENTS.md contract promises. Three separate
// rounds of that verification have been done by hand and thrown away, and the
// authentication regression these tests now cover slipped through in between.
//
// Run with:
//
//	go test -tags e2e ./e2e/... -v
//
// The build tag keeps it out of `go test ./...`, because this suite compiles
// the binary and binds ports — fine in CI and on request, needlessly slow as
// part of an inner loop.
package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const adminUser = "admin@inboxql.local"
const adminPassword = "e2e-password-not-a-secret"

var (
	buildOnce  sync.Once
	binaryPath string
	buildErr   error
)

// binary compiles iql once per test run and returns its path.
func binary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "iql-e2e-bin")
		if err != nil {
			buildErr = err
			return
		}
		name := "iql"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		binaryPath = filepath.Join(dir, name)

		// ../cmd/iql relative to this package.
		cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/iql")
		cmd.Dir = ".."
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("building iql: %v\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatalf("%v", buildErr)
	}
	return binaryPath
}

// result is one completed invocation of the CLI.
type result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// JSON decodes stdout, failing the test if it is not valid JSON.
//
// This doubles as an assertion of the AGENTS.md promise that --json commands
// put a JSON document on stdout and nothing else.
func (r result) JSON(t *testing.T, into any) {
	t.Helper()
	if err := json.Unmarshal([]byte(r.Stdout), into); err != nil {
		t.Fatalf("stdout was not valid JSON: %v\n--- stdout ---\n%s\n--- stderr ---\n%s",
			err, r.Stdout, r.Stderr)
	}
}

// env is one isolated InboxQL installation.
type env struct {
	t       *testing.T
	dataDir string
	bin     string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, dataDir: t.TempDir(), bin: binary(t)}

	r := e.run("init")
	if r.ExitCode != 0 {
		t.Fatalf("init failed (exit %d)\n%s\n%s", r.ExitCode, r.Stdout, r.Stderr)
	}
	return e
}

// run invokes the CLI against this environment's data directory.
//
// --data is appended rather than prepended on purpose: it exercises the
// globals-anywhere behaviour, which used to be broken in exactly this position.
func (e *env) run(args ...string) result {
	e.t.Helper()
	return e.runWithStdin(nil, args...)
}

func (e *env) runWithStdin(stdin io.Reader, args ...string) result {
	e.t.Helper()

	full := append(append([]string{}, args...), "--data", e.dataDir)
	cmd := exec.Command(e.bin, full...)
	cmd.Stdin = stdin
	// Secrets come from the environment because the CLI deliberately refuses
	// to take them as flags — argv is visible in shell history and to ps.
	cmd.Env = append(os.Environ(),
		"INBOXQL_ADMIN_PASSWORD="+adminPassword,
		"INBOXQL_ACCOUNT_PASSWORD="+adminPassword,
		"INBOXQL_NEW_PASSWORD="+adminPassword,
		"INBOXQL_DATA=", // must not leak in from the developer's shell
		"NO_COLOR=1",    // assert on text, not escape sequences
	)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		e.t.Fatalf("running %v: %v", args, err)
	}

	return result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: code}
}

// safeBuffer collects a child process's output for later assertions.
//
// os/exec writes into it from a goroutine it owns while the test reads it, so
// a plain strings.Builder is a data race — one the race detector caught the
// first time this suite was run under -race.
type safeBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// server is a running `iql start`.
type server struct {
	t    *testing.T
	cmd  *exec.Cmd
	Addr string
	out  *safeBuffer
}

// startServer launches the web server on a free loopback port.
func (e *env) startServer(extraArgs ...string) *server {
	e.t.Helper()
	return e.start(fmt.Sprintf("127.0.0.1:%d", freePort(e.t)), nil, extraArgs...)
}

// startServerEnv launches the web server with extra environment variables, for
// the settings that can come from the environment as well as from a flag.
func (e *env) startServerEnv(env []string, extraArgs ...string) *server {
	e.t.Helper()
	return e.start(fmt.Sprintf("127.0.0.1:%d", freePort(e.t)), env, extraArgs...)
}

// startServerAt launches the web server on an explicit listen address, for the
// cases where the address itself is what is under test.
func (e *env) startServerAt(addr string, extraArgs ...string) *server {
	e.t.Helper()
	return e.start(addr, nil, extraArgs...)
}

func (e *env) start(addr string, extraEnv []string, extraArgs ...string) *server {
	e.t.Helper()

	args := append([]string{"start", "--addr", addr, "--data", e.dataDir}, extraArgs...)
	cmd := exec.Command(e.bin, args...)
	cmd.Env = append(os.Environ(), "INBOXQL_DATA=", "NO_COLOR=1", "INBOXQL_TRUST_LOCAL=")
	cmd.Env = append(cmd.Env, extraEnv...)

	out := &safeBuffer{}
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Start(); err != nil {
		e.t.Fatalf("starting server: %v", err)
	}

	// A bare ":port" listens everywhere; connect to it over loopback.
	dialAddr := addr
	if strings.HasPrefix(dialAddr, ":") {
		dialAddr = "127.0.0.1" + dialAddr
	}

	s := &server{t: e.t, cmd: cmd, Addr: dialAddr, out: out}
	e.t.Cleanup(s.stop)
	s.waitReady()
	return s
}

func (s *server) stop() {
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_ = s.cmd.Wait()
	}
}

// Output returns everything the server has printed, for assertions on the
// startup banner.
func (s *server) Output() string { return s.out.String() }

func (s *server) waitReady() {
	s.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", s.Addr, 250*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.t.Fatalf("server did not start listening on %s within 20s\n%s", s.Addr, s.out.String())
}

// URL builds an absolute URL against the server.
func (s *server) URL(path string) string { return "http://" + s.Addr + path }

// get issues a request with optional headers and returns status and body.
func (s *server) get(t *testing.T, path string, headers map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, s.URL(path), nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// itoa keeps the port-formatting call sites short.
func itoa(n int) string { return strconv.Itoa(n) }
