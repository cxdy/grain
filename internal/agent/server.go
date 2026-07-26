package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Server is the guest-side grain-agent HTTP server.
type Server struct {
	Addr   string // listen address, default DefaultListen
	Log    *slog.Logger
	started time.Time

	mu       sync.Mutex
	listener net.Listener
	httpSrv  *http.Server
}

// NewServer returns a Server ready to ListenAndServe.
func NewServer(addr string, log *slog.Logger) *Server {
	if addr == "" {
		addr = DefaultListen
	}
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		Addr:    addr,
		Log:     log,
		started: time.Now(),
	}
}

// Handler returns the HTTP mux for the agent endpoints.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("HEAD /health", s.handleHealth)
	mux.HandleFunc("POST /exec", s.handleExec)
	return mux
}

// ListenAndServe starts the HTTP server. Blocks until Shutdown or fatal error.
func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.listener = ln
	s.httpSrv = &http.Server{Handler: s.Handler()}
	s.started = time.Now()
	s.mu.Unlock()

	s.Log.Info("grain-agent listening", "addr", ln.Addr().String(), "version", Version)
	err = s.httpSrv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// AddrString returns the bound address once listening, or the configured Addr.
func (s *Server) AddrString() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.Addr
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	srv := s.httpSrv
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	h := s.health()
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

func (s *Server) health() Health {
	hostname, _ := os.Hostname()
	return Health{
		Hostname:     hostname,
		AgentVersion: Version,
		AgentUptime:  int64(time.Since(s.started).Seconds()),
		UserdataRan:  userdataRan(),
	}
}

func userdataRan() bool {
	if _, err := os.Stat(UserdataRanPath); err == nil {
		return true
	}
	// Optional env override for testing / alternate markers.
	v := strings.TrimSpace(os.Getenv("GRAIN_USERDATA_RAN"))
	return v == "1" || strings.EqualFold(v, "true")
}

func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cmdName := q.Get("cmd")
	if cmdName == "" {
		writeJSON(w, http.StatusBadRequest, ExecResult{Error: "cmd is required", ExitCode: -1})
		return
	}
	args := q["args"]

	// M1: buffered mode only. Missing or true → single JSON ExecResult.
	// Explicit false → 501 (streaming not implemented yet).
	buffered := q.Get("buffered")
	if buffered == "false" {
		http.Error(w, "streaming exec not implemented", http.StatusNotImplemented)
		return
	}

	var uid, gid *uint32
	if v := q.Get("uid"); v != "" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, ExecResult{Error: "invalid uid", ExitCode: -1})
			return
		}
		u := uint32(n)
		uid = &u
	}
	if v := q.Get("gid"); v != "" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, ExecResult{Error: "invalid gid", ExitCode: -1})
			return
		}
		g := uint32(n)
		gid = &g
	}
	cwd := q.Get("cwd")

	// Respect request context; default wall-clock timeout is 5 minutes.
	ctx := r.Context()
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultExecTimeout)
		defer cancel()
	}

	result := s.execBuffered(ctx, cmdName, args, cwd, uid, gid)
	// Always 200 with ExecResult; non-zero exit is carried in the body.
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) execBuffered(ctx context.Context, name string, args []string, cwd string, uid, gid *uint32) ExecResult {
	cmd := exec.CommandContext(ctx, name, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}

	// Best-effort setuid/setgid when running as root.
	if (uid != nil || gid != nil) && os.Geteuid() == 0 {
		cred := &syscall.Credential{}
		if uid != nil {
			cred.Uid = *uid
		} else {
			cred.Uid = uint32(os.Geteuid())
		}
		if gid != nil {
			cred.Gid = *gid
		} else {
			cred.Gid = uint32(os.Getegid())
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: cred}
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := ExecResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
		} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			res.ExitCode = -1
			res.Error = "exec timeout"
		} else if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			res.ExitCode = -1
			res.Error = "exec canceled"
		} else {
			res.ExitCode = -1
			res.Error = err.Error()
		}
		return res
	}
	res.ExitCode = 0
	return res
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		// Best-effort; headers may already be flushed.
		_, _ = fmt.Fprintf(w, `{"error":%q}`, err.Error())
	}
}
