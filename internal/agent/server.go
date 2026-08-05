package agent

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Server is the guest-side grain-agent HTTP server.
type Server struct {
	Addr    string // listen address, default DefaultListen
	Log     *slog.Logger
	started time.Time
	clip    *clipboardBridge

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
		clip:    newClipboardBridge(),
	}
}

// Handler returns the HTTP mux for the agent endpoints.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("HEAD /health", s.handleHealth)
	mux.HandleFunc("GET /readiness", s.handleReadiness)
	mux.HandleFunc("GET /stats", s.handleStats)
	mux.HandleFunc("GET /clipboard", s.handleClipboard)
	mux.HandleFunc("POST /exec", s.handleExec)
	mux.HandleFunc("POST /cp", s.handleCPPut)
	mux.HandleFunc("GET /cp", s.handleCPGet)
	mux.HandleFunc("GET /fs/readdir", s.handleFSReaddir)
	mux.HandleFunc("GET /fs/stat", s.handleFSStat)
	mux.HandleFunc("POST /fs/mkdir", s.handleFSMkdir)
	mux.HandleFunc("POST /fs/symlink", s.handleFSSymlink)
	mux.HandleFunc("DELETE /fs/remove", s.handleFSRemove)
	mux.HandleFunc("POST /secrets/materialize", s.handleMaterializeSecret)
	mux.HandleFunc("GET /shell", s.handleShell)
	return mux
}

// ListenAndServe starts the HTTP server on TCP (always) and optionally on
// virtio-vsock (Linux guests when AF_VSOCK is available). Blocks until
// Shutdown or a fatal TCP listen error. Vsock listen failure is non-fatal.
func (s *Server) ListenAndServe() error {
	// Guest clipboard shims (OSC 52) for TUIs over grain sh — best-effort.
	if dir, err := ensureClipboardHelpers(); err != nil {
		s.Log.Debug("clipboard helpers unavailable", "err", err)
	} else {
		s.Log.Info("clipboard helpers ready", "dir", dir)
	}
	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}
	httpSrv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	s.mu.Lock()
	s.listener = ln
	s.httpSrv = httpSrv
	s.started = time.Now()
	s.mu.Unlock()

	s.Log.Info("grain-agent listening", "addr", ln.Addr().String(), "version", Version)

	// Best-effort vsock listener (same HTTP mux). Skipped on non-Linux builds
	// or when the guest has no virtio-vsock device.
	if vln, verr := listenVsock(DefaultVsockPort); verr == nil {
		s.Log.Info("grain-agent vsock listening", "port", DefaultVsockPort)
		go func() {
			if err := httpSrv.Serve(vln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				s.Log.Warn("vsock serve ended", "err", err)
			}
		}()
	} else {
		s.Log.Debug("vsock listen unavailable", "err", verr)
	}

	err = httpSrv.Serve(ln)
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

func (s *Server) handleReadiness(w http.ResponseWriter, _ *http.Request) {
	r := LoadReadiness()
	if r == nil {
		// Explicit empty object so clients can distinguish "no protocol files".
		writeJSON(w, http.StatusOK, Readiness{})
		return
	}
	writeJSON(w, http.StatusOK, r)
}

func (s *Server) handleStats(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, CollectStats())
}

func (s *Server) handleMaterializeSecret(w http.ResponseWriter, r *http.Request) {
	var req MaterializeSecretRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Name == "" && req.Path == "" {
		http.Error(w, "name or path is required", http.StatusBadRequest)
		return
	}
	if req.DataBase64 == "" {
		http.Error(w, "data_base64 is required", http.StatusBadRequest)
		return
	}
	data, err := decodeBase64(req.DataBase64)
	if err != nil {
		http.Error(w, "invalid data_base64", http.StatusBadRequest)
		return
	}
	path := req.Path
	if path == "" {
		path = filepath.Join(DefaultSecretsDir, req.Name)
	}
	mode := "0600"
	if req.Mode != "" {
		mode = req.Mode
	}
	perm, err := parseOctalMode(mode)
	if err != nil {
		http.Error(w, "invalid mode", http.StatusBadRequest)
		return
	}
	if err := putBinary(path, bytes.NewReader(data), perm, req.UID, req.GID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, MaterializeSecretResponse{
		Path: path,
		Mode: fmt.Sprintf("%04o", perm),
	})
}

func decodeBase64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}

func (s *Server) health() Health {
	hostname, _ := os.Hostname()
	return Health{
		Hostname:     hostname,
		AgentVersion: Version,
		AgentUptime:  int64(time.Since(s.started).Seconds()),
		UserdataRan:  userdataRan(),
		Readiness:    LoadReadiness(),
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

	// buffered query: absent or "true" → buffered JSON ExecResult (M1 default).
	// Explicit "false" → NDJSON streaming frames.
	buffered := q.Get("buffered")
	stream := buffered == "false"

	var uid, gid *uint32
	if v := q.Get("uid"); v != "" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			if stream {
				writeNDJSONError(w, "invalid uid")
			} else {
				writeJSON(w, http.StatusBadRequest, ExecResult{Error: "invalid uid", ExitCode: -1})
			}
			return
		}
		u := uint32(n)
		uid = &u
	}
	if v := q.Get("gid"); v != "" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			if stream {
				writeNDJSONError(w, "invalid gid")
			} else {
				writeJSON(w, http.StatusBadRequest, ExecResult{Error: "invalid gid", ExitCode: -1})
			}
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

	if stream {
		s.execStream(ctx, w, cmdName, args, cwd, uid, gid)
		return
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
	applyCredential(cmd, uid, gid)

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

// execStream writes application/x-ndjson frames and flushes after each.
// Order: started → stdout/stderr* → exit. Start failure: single error frame.
func (s *Server) execStream(ctx context.Context, w http.ResponseWriter, name string, args []string, cwd string, uid, gid *uint32) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	var mu sync.Mutex
	writeFrame := func(f ExecFrame) {
		if f.Timestamp == "" {
			f.Timestamp = time.Now().UTC().Format(time.RFC3339)
		}
		mu.Lock()
		defer mu.Unlock()
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(f) // trailing newline
		if flusher != nil {
			flusher.Flush()
		}
	}

	cmd := exec.CommandContext(ctx, name, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	applyCredential(cmd, uid, gid)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeFrame(ExecFrame{Type: "error", Error: err.Error()})
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		writeFrame(ExecFrame{Type: "error", Error: err.Error()})
		return
	}

	startedAt := time.Now().UTC()
	if err := cmd.Start(); err != nil {
		writeFrame(ExecFrame{Type: "error", Error: err.Error()})
		return
	}

	writeFrame(ExecFrame{
		Type:      "started",
		PID:       cmd.Process.Pid,
		StartedAt: startedAt.Format(time.RFC3339),
	})

	var wg sync.WaitGroup
	pipeCopy := func(r io.Reader, typ string) {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				writeFrame(ExecFrame{Type: typ, Data: string(buf[:n])})
			}
			if err != nil {
				return
			}
		}
	}
	wg.Add(2)
	go pipeCopy(stdout, "stdout")
	go pipeCopy(stderr, "stderr")
	wg.Wait()

	waitErr := cmd.Wait()
	endedAt := time.Now().UTC().Format(time.RFC3339)
	startedAtStr := startedAt.Format(time.RFC3339)

	exitCode := 0
	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			exitCode = ee.ExitCode()
		} else {
			// Process did not start properly or was aborted without exit code.
			writeFrame(ExecFrame{
				Type:      "error",
				Error:     waitErr.Error(),
				StartedAt: startedAtStr,
				EndedAt:   endedAt,
			})
			return
		}
	}
	writeFrame(ExecFrame{
		Type:      "exit",
		ExitCode:  &exitCode,
		StartedAt: startedAtStr,
		EndedAt:   endedAt,
	})
}

func applyCredential(cmd *exec.Cmd, uid, gid *uint32) {
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
}

// --- /cp -------------------------------------------------------------------

func (s *Server) handleCPPut(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path := q.Get("path")
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	mode := q.Get("mode")
	if mode == "" {
		mode = "binary"
	}

	var uid, gid *uint32
	if v := q.Get("uid"); v != "" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			http.Error(w, "invalid uid", http.StatusBadRequest)
			return
		}
		u := uint32(n)
		uid = &u
	}
	if v := q.Get("gid"); v != "" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			http.Error(w, "invalid gid", http.StatusBadRequest)
			return
		}
		g := uint32(n)
		gid = &g
	}

	perm := os.FileMode(DefaultFileMode)
	if v := q.Get("permissions"); v != "" {
		p, err := parseOctalMode(v)
		if err != nil {
			http.Error(w, "invalid permissions", http.StatusBadRequest)
			return
		}
		perm = p
	}

	switch mode {
	case "binary":
		if err := putBinary(path, r.Body, perm, uid, gid); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case "tar":
		if err := putTar(path, r.Body, uid, gid); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, "mode must be binary or tar", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCPGet(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path := q.Get("path")
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	mode := q.Get("mode")
	if mode == "" {
		mode = "binary"
	}

	st, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	switch mode {
	case "binary":
		if st.IsDir() {
			http.Error(w, "path is a directory; use mode=tar", http.StatusBadRequest)
			return
		}
		f, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer func() { _ = f.Close() }()
		w.Header().Set("Content-Type", "application/octet-stream")
		if st.Size() >= 0 {
			w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, f)
	case "tar":
		w.Header().Set("Content-Type", "application/x-tar")
		w.WriteHeader(http.StatusOK)
		if err := writeTar(w, path); err != nil {
			// Headers may already be sent; best-effort log.
			s.Log.Error("tar stream failed", "path", path, "err", err)
		}
	default:
		http.Error(w, "mode must be binary or tar", http.StatusBadRequest)
	}
}

func putBinary(path string, r io.Reader, perm os.FileMode, uid, gid *uint32) error {
	if err := os.MkdirAll(filepath.Dir(path), DefaultDirMode); err != nil {
		return fmt.Errorf("mkdir parent: %w", err)
	}
	// Write via temp file then rename for atomicity when possible.
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".grain-cp-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := io.Copy(tmp, r); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		// Cross-device or replace: fall back to copy-over.
		if err2 := copyFileReplace(tmpName, path, perm); err2 != nil {
			return fmt.Errorf("rename: %w", err)
		}
		_ = os.Remove(tmpName)
	}
	cleanup = false
	if err := applyOwnership(path, uid, gid); err != nil {
		return err
	}
	return nil
}

func copyFileReplace(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Chmod(perm)
}

func putTar(dest string, r io.Reader, uid, gid *uint32) error {
	if err := os.MkdirAll(dest, DefaultDirMode); err != nil {
		return fmt.Errorf("mkdir dest: %w", err)
	}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}
		target, err := safeTarPath(dest, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)&os.ModePerm); err != nil {
				return err
			}
			if os.FileMode(hdr.Mode)&os.ModePerm == 0 {
				_ = os.Chmod(target, DefaultDirMode)
			}
			_ = applyOwnership(target, uid, gid)
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), DefaultDirMode); err != nil {
				return err
			}
			perm := os.FileMode(hdr.Mode) & os.ModePerm
			if perm == 0 {
				perm = DefaultFileMode
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			_ = applyOwnership(target, uid, gid)
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), DefaultDirMode); err != nil {
				return err
			}
			_ = os.Remove(target) // replace existing
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		default:
			// Skip other types (hard links, devices, etc.).
		}
	}
	return nil
}

func safeTarPath(dest, name string) (string, error) {
	// Prevent path traversal: join under dest and verify prefix.
	name = filepath.FromSlash(name)
	// Drop absolute prefix so Join does not discard dest on Unix.
	name = strings.TrimPrefix(name, string(filepath.Separator))
	if name == "" || name == "." {
		return dest, nil
	}
	clean := filepath.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("illegal tar path: %s", name)
	}
	target := filepath.Join(dest, clean)
	// Ensure target stays under dest.
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return "", err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	sep := string(filepath.Separator)
	if absTarget != absDest && !strings.HasPrefix(absTarget, absDest+sep) {
		return "", fmt.Errorf("illegal tar path: %s", name)
	}
	return target, nil
}

func writeTar(w io.Writer, src string) error {
	tw := tar.NewWriter(w)
	defer func() { _ = tw.Close() }()

	info, err := os.Lstat(src)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		return addToTar(tw, src, filepath.Base(src), info)
	}

	return filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		// Re-Lstat for symlink typeflag accuracy (Walk follows dirs only).
		li, err := os.Lstat(path)
		if err != nil {
			return err
		}
		return addToTar(tw, path, rel, li)
	})
}

func addToTar(tw *tar.Writer, path, name string, fi os.FileInfo) error {
	name = filepath.ToSlash(name)
	hdr, err := tar.FileInfoHeader(fi, "")
	if err != nil {
		return err
	}
	hdr.Name = name
	if fi.Mode()&os.ModeSymlink != 0 {
		link, err := os.Readlink(path)
		if err != nil {
			return err
		}
		hdr.Typeflag = tar.TypeSymlink
		hdr.Linkname = link
		hdr.Size = 0
	} else if fi.IsDir() {
		if !strings.HasSuffix(hdr.Name, "/") {
			hdr.Name += "/"
		}
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if fi.Mode().IsRegular() {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		if _, err := io.Copy(tw, f); err != nil {
			return err
		}
	}
	return nil
}

func applyOwnership(path string, uid, gid *uint32) error {
	if uid == nil && gid == nil {
		return nil
	}
	if os.Geteuid() != 0 {
		// Best-effort: ignore when not root.
		return nil
	}
	u := -1
	g := -1
	if uid != nil {
		u = int(*uid)
	}
	if gid != nil {
		g = int(*gid)
	}
	if err := os.Chown(path, u, g); err != nil {
		return fmt.Errorf("chown: %w", err)
	}
	return nil
}

// --- /fs -------------------------------------------------------------------

func (s *Server) handleFSReaddir(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]FSInfo, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			// Entry disappeared; skip.
			continue
		}
		// Prefer Lstat-style type for symlinks.
		full := filepath.Join(path, e.Name())
		if li, err := os.Lstat(full); err == nil {
			info = li
		}
		out = append(out, fsInfoFrom(e.Name(), full, info))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleFSStat(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, fsInfoFrom(filepath.Base(path), path, info))
}

func (s *Server) handleFSMkdir(w http.ResponseWriter, r *http.Request) {
	var req MkdirRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	perm := os.FileMode(DefaultDirMode)
	if req.Mode != "" {
		p, err := parseOctalMode(req.Mode)
		if err != nil {
			http.Error(w, "invalid mode", http.StatusBadRequest)
			return
		}
		perm = p
	}
	var err error
	if req.Recursive {
		err = os.MkdirAll(req.Path, perm)
	} else {
		err = os.Mkdir(req.Path, perm)
	}
	if err != nil {
		if os.IsExist(err) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// MkdirAll may not set mode on existing parents; ensure leaf mode.
	_ = os.Chmod(req.Path, perm)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleFSSymlink(w http.ResponseWriter, r *http.Request) {
	var req SymlinkRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	if req.Target == "" {
		http.Error(w, "target is required", http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(filepath.Dir(req.Path), DefaultDirMode); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Replace existing path (file, dir, or prior link) so sync can update.
	_ = os.RemoveAll(req.Path)
	if err := os.Symlink(req.Target, req.Path); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleFSRemove(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path := q.Get("path")
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	recursive := q.Get("recursive") == "true"
	var err error
	if recursive {
		err = os.RemoveAll(path)
	} else {
		err = os.Remove(path)
	}
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// fsInfoFrom builds FSInfo from Lstat results. fullPath is used to resolve
// symlink targets via Readlink (empty path skips target lookup).
func fsInfoFrom(name, fullPath string, info os.FileInfo) FSInfo {
	mode := info.Mode()
	var typ string
	var target string
	switch {
	case mode&os.ModeSymlink != 0:
		typ = "symlink"
		if fullPath != "" {
			if t, err := os.Readlink(fullPath); err == nil {
				target = t
			}
		}
	case mode.IsDir():
		typ = "directory"
	default:
		typ = "file"
	}
	return FSInfo{
		Name:   name,
		Type:   typ,
		Size:   info.Size(),
		Mtime:  info.ModTime().Unix(),
		Mode:   fmt.Sprintf("%04o", mode.Perm()),
		Target: target,
	}
}

func parseOctalMode(s string) (os.FileMode, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "0o")
	s = strings.TrimPrefix(s, "0O")
	// strconv.ParseUint with base 8 accepts optional leading 0.
	n, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return 0, err
	}
	return os.FileMode(n) & os.ModePerm, nil
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

// writeNDJSONError writes a single error frame (used for pre-stream validation).
func writeNDJSONError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(ExecFrame{
		Type:      "error",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Error:     msg,
	})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
