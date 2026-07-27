package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	grainapi "github.com/cxdy/grain/api"
	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/manager"
	"github.com/cxdy/grain/internal/observability"
	"github.com/cxdy/grain/internal/secrets"
	"github.com/cxdy/grain/internal/vm"
)

// Server is the local control plane HTTP API (unix socket or TCP).
type Server struct {
	mgr *manager.Manager
	met *observability.Metrics
	log *slog.Logger
	// Secrets is the host-side secret store (optional; nil disables /secrets).
	Secrets *secrets.Store
	// APIToken, when non-empty, requires Authorization: Bearer <token>
	// on all routes except GET /healthz.
	APIToken string
}

func New(mgr *manager.Manager, met *observability.Metrics, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	if met == nil {
		met = observability.NewMetrics()
	}
	return &Server{mgr: mgr, met: met, log: log}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /info", s.info)
	mux.Handle("GET /metrics", s.met.Handler())
	mux.HandleFunc("GET /openapi.yaml", s.serveOpenAPI)
	mux.HandleFunc("GET /openapi.json", s.serveOpenAPI)
	mux.HandleFunc("GET /vms", s.listVMs)
	mux.HandleFunc("POST /vms", s.createVM)
	mux.HandleFunc("GET /vms/{name}", s.getVM)
	mux.HandleFunc("DELETE /vms/{name}", s.deleteVM)
	mux.HandleFunc("POST /vms/{name}/shutdown", s.shutdownVM)
	mux.HandleFunc("POST /vms/{name}/start", s.startVM)
	mux.HandleFunc("POST /vms/{name}/pause", s.pauseVM)
	mux.HandleFunc("POST /vms/{name}/resume", s.resumeVM)
	mux.HandleFunc("POST /vms/{name}/suspend", s.suspendVM)
	mux.HandleFunc("POST /vms/{name}/restore", s.restoreVM)
	mux.HandleFunc("POST /vms/{name}/forwards", s.addForward)
	mux.HandleFunc("DELETE /vms/{name}/forwards/{hostPort}", s.removeForward)
	mux.HandleFunc("POST /vms/{name}/exec", s.execVM)
	// Interactive PTY shell (WebSocket) — required for remote CLI `grain sh`
	mux.HandleFunc("GET /vms/{name}/shell", s.shellVM)
	mux.HandleFunc("GET /vms/{name}/agent/health", s.agentHealth)
	mux.HandleFunc("GET /vms/{name}/stats", s.vmStats)
	mux.HandleFunc("PUT /vms/{name}/cp", s.putCP)
	mux.HandleFunc("GET /vms/{name}/cp", s.getCP)
	mux.HandleFunc("GET /vms/{name}/fs/readdir", s.fsReadDir)
	mux.HandleFunc("GET /vms/{name}/fs/stat", s.fsStat)
	mux.HandleFunc("POST /vms/{name}/fs/mkdir", s.fsMkdir)
	mux.HandleFunc("DELETE /vms/{name}/fs/remove", s.fsRemove)
	// Host secrets store
	mux.HandleFunc("GET /secrets", s.listSecrets)
	mux.HandleFunc("POST /secrets", s.createSecret)
	mux.HandleFunc("GET /secrets/{name}", s.getSecret)
	mux.HandleFunc("PATCH /secrets/{name}", s.patchSecret)
	mux.HandleFunc("DELETE /secrets/{name}", s.deleteSecret)
	// Materialize host secret into guest
	mux.HandleFunc("POST /vms/{name}/secrets/{secretName}", s.injectSecret)
	return loggingMiddleware(s.log, authMiddleware(s.APIToken, mux))
}

func (s *Server) serveOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(grainapi.OpenAPIYAML)
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) info(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"version": Version,
		"name":    "grain",
	})
}

// Version is set from main via init or ldflags later.
var Version = "0.1.0"

func (s *Server) listVMs(w http.ResponseWriter, r *http.Request) {
	list, err := s.mgr.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if list == nil {
		list = []*vm.Instance{}
	}
	writeJSON(w, http.StatusOK, list)
}

type createBody struct {
	Name           string             `json:"name"`
	Persistent     bool               `json:"persistent"`
	CPUs           int                `json:"cpus"`
	MemoryMB       int                `json:"memory_mb"`
	DiskGB         int                `json:"disk_gb"`
	Image          string             `json:"image"`
	Arch           string             `json:"arch"`
	GPU            string             `json:"gpu"`
	Network        string             `json:"network"`
	Tags           map[string]string  `json:"tags"`
	Userdata       string             `json:"userdata"`
	Forwards       []vm.PortForward   `json:"forwards"`
	Mounts         []vm.Mount         `json:"mounts"`
	SocketForwards []vm.SocketForward `json:"socket_forwards"`
}

func wantsCreateStream(r *http.Request) bool {
	if r.URL.Query().Get("stream") == "1" {
		return true
	}
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/x-ndjson")
}

func (s *Server) createVM(w http.ResponseWriter, r *http.Request) {
	var body createBody
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	}

	q := r.URL.Query()
	// wait=auto|ssh|agent|userdata (empty/auto → manager picks agent for golden images).
	// Legacy wait=1/true → ssh. wait=false/0 is not supported.
	waitRaw := q.Get("wait")
	var waitMode string
	var waitTimeout time.Duration
	switch {
	case waitRaw == "":
		waitMode = "" // manager auto
	case waitRaw == "1" || strings.EqualFold(waitRaw, "true"):
		waitMode = vm.WaitSSH
	case strings.EqualFold(waitRaw, "false") || waitRaw == "0":
		writeErr(w, http.StatusBadRequest, errors.New("wait=false is not supported; use stream=1 for progress events"))
		return
	default:
		mode, err := manager.NormalizeWaitMode(waitRaw)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		waitMode = mode
	}

	timeout := s.mgr.CreateTimeout()
	if t := q.Get("timeout"); t != "" {
		d, err := time.ParseDuration(t)
		if err != nil || d <= 0 {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid timeout %q (want Go duration e.g. 30s)", t))
			return
		}
		timeout = d
		waitTimeout = d
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	opts := vm.CreateOpts{
		Name:           body.Name,
		Persistent:     body.Persistent,
		CPUs:           body.CPUs,
		MemoryMB:       body.MemoryMB,
		DiskGB:         body.DiskGB,
		Image:          body.Image,
		Arch:           body.Arch,
		GPU:            body.GPU,
		Network:        body.Network,
		Tags:           body.Tags,
		Userdata:       body.Userdata,
		Forwards:       body.Forwards,
		Mounts:         body.Mounts,
		SocketForwards: body.SocketForwards,
		WaitMode:       waitMode,
		WaitTimeout:    waitTimeout,
	}

	if wantsCreateStream(r) {
		s.createVMStream(w, ctx, opts)
		return
	}

	inst, err := s.mgr.Create(ctx, opts)
	if err != nil {
		s.met.CreateErrors.Add(1)
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.met.VMsCreated.Add(1)
	s.met.VMsRunning.Add(1)
	writeJSON(w, http.StatusCreated, inst)
}

// createVMStream writes NDJSON CreateEvent lines as create proceeds.
func (s *Server) createVMStream(w http.ResponseWriter, ctx context.Context, opts vm.CreateOpts) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		// fallback: buffer all events then write
		var events []vm.CreateEvent
		opts.OnEvent = func(ev vm.CreateEvent) {
			events = append(events, ev)
		}
		inst, err := s.mgr.Create(ctx, opts)
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		enc := json.NewEncoder(w)
		for _, ev := range events {
			_ = enc.Encode(ev)
		}
		if err != nil && (len(events) == 0 || events[len(events)-1].Phase != vm.PhaseError) {
			_ = enc.Encode(vm.CreateEvent{Phase: vm.PhaseError, Error: err.Error(), Message: err.Error()})
		}
		if err != nil {
			s.met.CreateErrors.Add(1)
			return
		}
		_ = inst
		s.met.VMsCreated.Add(1)
		s.met.VMsRunning.Add(1)
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	enc := json.NewEncoder(w)
	var writeMu sync.Mutex
	writeEv := func(ev vm.CreateEvent) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = enc.Encode(ev)
		flusher.Flush()
	}
	opts.OnEvent = writeEv

	var sawError bool
	prev := opts.OnEvent
	opts.OnEvent = func(ev vm.CreateEvent) {
		if ev.Phase == vm.PhaseError {
			sawError = true
		}
		if prev != nil {
			prev(ev)
		}
	}

	inst, err := s.mgr.Create(ctx, opts)
	if err != nil {
		s.met.CreateErrors.Add(1)
		if !sawError {
			writeEv(vm.CreateEvent{Phase: vm.PhaseError, Error: err.Error(), Message: err.Error()})
		}
		return
	}
	_ = inst
	s.met.VMsCreated.Add(1)
	s.met.VMsRunning.Add(1)
}

func (s *Server) getVM(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	inst, err := s.mgr.Get(name)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (s *Server) deleteVM(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.mgr.Delete(r.Context(), name); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	s.met.VMsDeleted.Add(1)
	s.met.VMsRunning.Add(-1)
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted", "name": name})
}

func (s *Server) shutdownVM(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.mgr.Shutdown(r.Context(), name); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "shutdown", "name": name})
}

func (s *Server) startVM(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctx, cancel := context.WithTimeout(r.Context(), s.mgr.CreateTimeout())
	defer cancel()
	inst, err := s.mgr.Start(ctx, name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (s *Server) pauseVM(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.mgr.Pause(r.Context(), name); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "paused", "name": name})
}

func (s *Server) resumeVM(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.mgr.Resume(r.Context(), name); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "resumed", "name": name})
}

func (s *Server) suspendVM(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	// savevm can be slow for large guests
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	if err := s.mgr.Suspend(ctx, name); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "suspended", "name": name})
}

func (s *Server) restoreVM(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctx, cancel := context.WithTimeout(r.Context(), s.mgr.CreateTimeout())
	defer cancel()
	inst, err := s.mgr.Restore(ctx, name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

type forwardBody struct {
	HostPort  int `json:"host_port"`
	GuestPort int `json:"guest_port"`
}

func (s *Server) addForward(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var body forwardBody
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, errors.New("invalid JSON body"))
			return
		}
	}
	if body.GuestPort == 0 {
		writeErr(w, http.StatusBadRequest, errors.New("guest_port is required"))
		return
	}
	lf, err := s.mgr.AddForward(r.Context(), name, body.HostPort, body.GuestPort)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, lf)
}

func (s *Server) removeForward(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	hostPort, err := strconv.Atoi(r.PathValue("hostPort"))
	if err != nil || hostPort <= 0 {
		writeErr(w, http.StatusBadRequest, errors.New("invalid hostPort"))
		return
	}
	if err := s.mgr.RemoveForward(r.Context(), name, hostPort); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message":   "forward removed",
		"name":      name,
		"host_port": hostPort,
	})
}

// agentClient returns a host-side agent client for the VM, or an HTTP status + error.
// Prefers virtio-vsock when AgentCID > 0, else TCP hostfwd (see agent.Dial).
func (s *Server) agentClient(name string) (*agent.Client, int, error) {
	inst, err := s.mgr.Get(name)
	if err != nil {
		return nil, http.StatusNotFound, err
	}
	target := agent.Target{CID: inst.AgentCID, Port: inst.AgentPort}
	if !target.HasEndpoint() {
		return nil, http.StatusServiceUnavailable, errors.New("agent not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ac, err := agent.Dial(ctx, target)
	if err != nil {
		return nil, http.StatusServiceUnavailable, err
	}
	return ac, 0, nil
}

// execVM proxies command execution to the guest grain-agent.
// buffered=true (default): JSON ExecResult. Non-zero remote exit still HTTP 200.
// buffered=false: proxies NDJSON ExecFrame stream (application/x-ndjson).
// Agent connection / transport failures return 502.
func (s *Server) execVM(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ac, code, err := s.agentClient(name)
	if err != nil {
		writeErr(w, code, err)
		return
	}

	q := r.URL.Query()
	cmdName := q.Get("cmd")
	if cmdName == "" {
		writeErr(w, http.StatusBadRequest, errors.New("cmd is required"))
		return
	}
	args := q["args"]

	if q.Get("buffered") == "false" {
		s.execVMStream(w, r, ac, cmdName, args)
		return
	}

	opts := agent.ExecOpts{Cmd: cmdName, Args: args}
	if v := q.Get("uid"); v != "" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			writeErr(w, http.StatusBadRequest, errors.New("invalid uid"))
			return
		}
		u := uint32(n)
		opts.UID = &u
	}
	if v := q.Get("gid"); v != "" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			writeErr(w, http.StatusBadRequest, errors.New("invalid gid"))
			return
		}
		g := uint32(n)
		opts.GID = &g
	}
	opts.Cwd = q.Get("cwd")

	result, err := ac.ExecBufferedOpts(r.Context(), opts)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// execVMStream proxies POST /exec?buffered=false NDJSON from the guest agent.
func (s *Server) execVMStream(w http.ResponseWriter, r *http.Request, ac *agent.Client, cmd string, args []string) {
	u, err := url.Parse(strings.TrimRight(ac.BaseURL, "/") + "/exec")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	q := u.Query()
	q.Set("cmd", cmd)
	for _, a := range args {
		q.Add("args", a)
	}
	q.Set("buffered", "false")
	orig := r.URL.Query()
	for _, k := range []string{"uid", "gid", "cwd"} {
		if v := orig.Get(k); v != "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, u.String(), nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	httpClient := agentLongHTTP(ac)
	res, err := httpClient.Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		writeErr(w, http.StatusBadGateway, fmt.Errorf("agent exec stream: status %d: %s", res.StatusCode, strings.TrimSpace(string(body))))
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	if flusher, ok := w.(http.Flusher); ok {
		buf := make([]byte, 32*1024)
		for {
			n, err := res.Body.Read(buf)
			if n > 0 {
				if _, werr := w.Write(buf[:n]); werr != nil {
					return
				}
				flusher.Flush()
			}
			if err != nil {
				return
			}
		}
	}
	_, _ = io.Copy(w, res.Body)
}

// agentLongHTTP returns an HTTP client with no overall Timeout so streaming
// and large copies are governed by the request context.
func agentLongHTTP(ac *agent.Client) *http.Client {
	if ac.HTTP != nil {
		if ac.HTTP.Timeout == 0 {
			return ac.HTTP
		}
		clone := *ac.HTTP
		clone.Timeout = 0
		return &clone
	}
	return &http.Client{}
}

// shellVM reverse-proxies GET /vms/{name}/shell (WebSocket) to the guest agent /shell.
// Remote CLI clients use this instead of dialing the hostfwd agent port on loopback.
func (s *Server) shellVM(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ac, code, err := s.agentClient(name)
	if err != nil {
		writeErr(w, code, err)
		return
	}
	target, err := url.Parse(strings.TrimRight(ac.BaseURL, "/"))
	if err != nil || target.Scheme == "" || target.Host == "" {
		writeErr(w, http.StatusBadGateway, fmt.Errorf("agent base URL: %w", err))
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	// Use the agent transport (TCP hostfwd or vsock) with no overall timeout.
	proxy.Transport = agentLongHTTP(ac).Transport
	if proxy.Transport == nil {
		proxy.Transport = agentLongHTTP(ac).Transport
	}
	// Ensure the dialer from agent.Client is used when HTTP has a custom Transport.
	if ac.HTTP != nil && ac.HTTP.Transport != nil {
		proxy.Transport = ac.HTTP.Transport
	}
	origDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		origDirector(req)
		req.URL.Path = "/shell"
		req.URL.RawPath = "/shell"
		req.URL.RawQuery = r.URL.RawQuery
		req.Host = target.Host
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, e error) {
		s.log.Error("shell proxy", "vm", name, "err", e)
		if !headersSent(rw) {
			writeErr(rw, http.StatusBadGateway, fmt.Errorf("shell proxy: %w", e))
		}
	}
	proxy.ServeHTTP(w, r)
}

// headersSent is a best-effort check; ReverseProxy may have already written.
func headersSent(w http.ResponseWriter) bool {
	// http.ResponseWriter does not expose this; ErrorHandler is only called
	// before a successful upgrade in practice. Always try writeErr carefully.
	return false
}

// putCP proxies file/tar upload to the guest agent (agent POST /cp).
// Query: path (required), mode=binary|tar, uid, gid, permissions.
func (s *Server) putCP(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ac, code, err := s.agentClient(name)
	if err != nil {
		writeErr(w, code, err)
		return
	}
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
	}

	q := r.URL.Query()
	path := q.Get("path")
	if path == "" {
		writeErr(w, http.StatusBadRequest, errors.New("path is required"))
		return
	}
	mode := q.Get("mode")
	if mode == "" {
		mode = "binary"
	}

	switch mode {
	case "binary":
		opts := agent.CPOpts{Mode: q.Get("permissions")}
		if v := q.Get("uid"); v != "" {
			n, err := strconv.ParseUint(v, 10, 32)
			if err != nil {
				writeErr(w, http.StatusBadRequest, errors.New("invalid uid"))
				return
			}
			u := uint32(n)
			opts.UID = &u
		}
		if v := q.Get("gid"); v != "" {
			n, err := strconv.ParseUint(v, 10, 32)
			if err != nil {
				writeErr(w, http.StatusBadRequest, errors.New("invalid gid"))
				return
			}
			g := uint32(n)
			opts.GID = &g
		}
		size := r.ContentLength
		if err := ac.PutFile(r.Context(), path, r.Body, size, opts); err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
	case "tar":
		if err := ac.PutTar(r.Context(), path, r.Body); err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, errors.New("mode must be binary or tar"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// getCP streams a file or tar from the guest agent (agent GET /cp).
// Query: path (required), mode=binary|tar.
func (s *Server) getCP(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ac, code, err := s.agentClient(name)
	if err != nil {
		writeErr(w, code, err)
		return
	}

	q := r.URL.Query()
	path := q.Get("path")
	if path == "" {
		writeErr(w, http.StatusBadRequest, errors.New("path is required"))
		return
	}
	mode := q.Get("mode")
	if mode == "" {
		mode = "binary"
	}
	if mode != "binary" && mode != "tar" {
		writeErr(w, http.StatusBadRequest, errors.New("mode must be binary or tar"))
		return
	}

	// Proxy raw response so large files stream without buffering.
	u, err := url.Parse(strings.TrimRight(ac.BaseURL, "/") + "/cp")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	aq := u.Query()
	aq.Set("path", path)
	aq.Set("mode", mode)
	u.RawQuery = aq.Encode()

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u.String(), nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	res, err := agentLongHTTP(ac).Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode == http.StatusNotFound {
		writeErr(w, http.StatusNotFound, errors.New("not found"))
		return
	}
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		writeErr(w, http.StatusBadGateway, fmt.Errorf("agent cp: status %d: %s", res.StatusCode, strings.TrimSpace(string(body))))
		return
	}

	if ct := res.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else if mode == "tar" {
		w.Header().Set("Content-Type", "application/x-tar")
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	if cl := res.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, res.Body)
}

// fsReadDir proxies GET /fs/readdir from the guest agent.
func (s *Server) fsReadDir(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ac, code, err := s.agentClient(name)
	if err != nil {
		writeErr(w, code, err)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeErr(w, http.StatusBadRequest, errors.New("path is required"))
		return
	}
	entries, err := ac.ReadDir(r.Context(), path)
	if err != nil {
		writeAgentFSErr(w, err)
		return
	}
	if entries == nil {
		entries = []agent.FSInfo{}
	}
	writeJSON(w, http.StatusOK, entries)
}

// fsStat proxies GET /fs/stat from the guest agent.
func (s *Server) fsStat(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ac, code, err := s.agentClient(name)
	if err != nil {
		writeErr(w, code, err)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeErr(w, http.StatusBadRequest, errors.New("path is required"))
		return
	}
	info, err := ac.Stat(r.Context(), path)
	if err != nil {
		writeAgentFSErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// fsMkdir proxies POST /fs/mkdir JSON body to the guest agent.
func (s *Server) fsMkdir(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ac, code, err := s.agentClient(name)
	if err != nil {
		writeErr(w, code, err)
		return
	}
	var body agent.MkdirRequest
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, errors.New("invalid JSON body"))
			return
		}
	}
	if body.Path == "" {
		writeErr(w, http.StatusBadRequest, errors.New("path is required"))
		return
	}
	if err := ac.Mkdir(r.Context(), body.Path, body.Recursive, body.Mode); err != nil {
		writeAgentFSErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// fsRemove proxies DELETE /fs/remove from the guest agent.
// Query: path (required), recursive=true|false.
func (s *Server) fsRemove(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ac, code, err := s.agentClient(name)
	if err != nil {
		writeErr(w, code, err)
		return
	}
	q := r.URL.Query()
	path := q.Get("path")
	if path == "" {
		writeErr(w, http.StatusBadRequest, errors.New("path is required"))
		return
	}
	recursive := q.Get("recursive") == "true" || q.Get("recursive") == "1"
	if err := ac.Remove(r.Context(), path, recursive); err != nil {
		writeAgentFSErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeAgentFSErr maps agent client FS errors to HTTP status codes.
func writeAgentFSErr(w http.ResponseWriter, err error) {
	msg := err.Error()
	if strings.Contains(msg, "not found") {
		writeErr(w, http.StatusNotFound, errors.New("not found"))
		return
	}
	writeErr(w, http.StatusBadGateway, err)
}

// agentHealth proxies GET /health from the guest grain-agent.
func (s *Server) agentHealth(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ac, code, err := s.agentClient(name)
	if err != nil {
		writeErr(w, code, err)
		return
	}
	h, err := ac.Health(r.Context())
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

// vmStats proxies GET /stats from the guest grain-agent.
func (s *Server) vmStats(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ac, code, err := s.agentClient(name)
	if err != nil {
		writeErr(w, code, err)
		return
	}
	st, err := ac.Stats(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// --- host secrets ----------------------------------------------------------

func (s *Server) requireSecrets(w http.ResponseWriter) bool {
	if s.Secrets == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("secrets store not configured"))
		return false
	}
	return true
}

func (s *Server) listSecrets(w http.ResponseWriter, _ *http.Request) {
	if !s.requireSecrets(w) {
		return
	}
	list, err := s.Secrets.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if list == nil {
		list = []secrets.Meta{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) createSecret(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecrets(w) {
		return
	}
	var body secrets.PutRequest
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
		if err := json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, errors.New("invalid JSON body"))
			return
		}
	}
	m, err := s.Secrets.Put(body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) getSecret(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecrets(w) {
		return
	}
	name := r.PathValue("name")
	includeData := r.URL.Query().Get("data") == "1" || r.URL.Query().Get("data") == "true"
	sec, err := s.Secrets.Get(name, includeData)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, sec)
}

func (s *Server) patchSecret(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecrets(w) {
		return
	}
	name := r.PathValue("name")
	var body secrets.PutRequest
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
		if err := json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, errors.New("invalid JSON body"))
			return
		}
	}
	m, err := s.Secrets.Patch(name, body)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) deleteSecret(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecrets(w) {
		return
	}
	name := r.PathValue("name")
	if err := s.Secrets.Delete(name); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted", "name": name})
}

// injectSecret materializes a host secret into a running VM via the guest agent.
// Optional JSON body: { "path": "/run/grain/secrets/NAME" } overrides guest path.
func (s *Server) injectSecret(w http.ResponseWriter, r *http.Request) {
	if !s.requireSecrets(w) {
		return
	}
	vmName := r.PathValue("name")
	secretName := r.PathValue("secretName")
	sec, err := s.Secrets.Get(secretName, true)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	}
	ac, code, err := s.agentClient(vmName)
	if err != nil {
		writeErr(w, code, err)
		return
	}
	req := agent.MaterializeSecretRequest{
		Name:       sec.Name,
		DataBase64: sec.DataBase64,
		Path:       body.Path,
		Mode:       sec.Mode,
		UID:        sec.UID,
		GID:        sec.GID,
	}
	out, err := ac.MaterializeSecret(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func loggingMiddleware(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		// skip metrics noise at debug
		if r.URL.Path != "/metrics" && r.URL.Path != "/healthz" {
			log.Debug("http",
				"method", r.Method,
				"path", r.URL.Path,
				"dur_ms", time.Since(start).Milliseconds(),
			)
		}
	})
}

// authMiddleware requires Authorization: Bearer <token> when token is non-empty.
// GET /healthz is always public so local probes and process managers stay simple.
func authMiddleware(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if !bearerAuthorized(r.Header.Get("Authorization"), token) {
			writeErr(w, http.StatusUnauthorized, errors.New("unauthorized"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// bearerAuthorized checks Authorization: Bearer <token> in constant time when lengths match.
func bearerAuthorized(header, token string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got := header[len(prefix):]
	if len(got) != len(token) {
		// Dummy compare to keep timing closer when lengths differ.
		subtle.ConstantTimeCompare([]byte(token), []byte(token))
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

// Client talks to a running daemon over HTTP (unix socket via custom Transport).
type Client struct {
	Base  string // e.g. http://grain (with unix dialer)
	HTTP  *http.Client
	Token string // optional Bearer token (Authorization header)
}

func (c *Client) http() *http.Client {
	base := c.HTTP
	if base == nil {
		base = http.DefaultClient
	}
	if c.Token == "" {
		return base
	}
	// Clone so we do not mutate a shared *http.Client (e.g. httptest.Client()).
	clone := *base
	rt := base.Transport
	if rt == nil {
		rt = http.DefaultTransport
	}
	clone.Transport = &bearerRoundTripper{base: rt, token: c.Token}
	return &clone
}

// bearerRoundTripper injects Authorization: Bearer on every request.
type bearerRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (b *bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(req2)
}

func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/healthz", nil)
	if err != nil {
		return err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != 200 {
		return errors.New("unhealthy")
	}
	return nil
}

func (c *Client) List(ctx context.Context) ([]*vm.Instance, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/vms", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var list []*vm.Instance
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		return nil, err
	}
	return list, nil
}

func (c *Client) Delete(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.Base+"/vms/"+name, nil)
	if err != nil {
		return err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(res.Body).Decode(&e)
		if e.Error == "" {
			return fmt.Errorf("status %d", res.StatusCode)
		}
		return errors.New(e.Error)
	}
	return nil
}
