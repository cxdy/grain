package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cxdy/grain/internal/manager"
	"github.com/cxdy/grain/internal/observability"
	"github.com/cxdy/grain/internal/vm"
)

// Server is the local control plane HTTP API (unix socket or TCP).
type Server struct {
	mgr *manager.Manager
	met *observability.Metrics
	log *slog.Logger
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
	mux.HandleFunc("GET /vms", s.listVMs)
	mux.HandleFunc("POST /vms", s.createVM)
	mux.HandleFunc("GET /vms/{name}", s.getVM)
	mux.HandleFunc("DELETE /vms/{name}", s.deleteVM)
	mux.HandleFunc("POST /vms/{name}/shutdown", s.shutdownVM)
	mux.HandleFunc("POST /vms/{name}/start", s.startVM)
	return loggingMiddleware(s.log, mux)
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
var Version = "0.1.0-dev"

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
	Name       string            `json:"name"`
	Persistent bool              `json:"persistent"`
	CPUs       int               `json:"cpus"`
	MemoryMB   int               `json:"memory_mb"`
	DiskGB     int               `json:"disk_gb"`
	Image      string            `json:"image"`
	Tags       map[string]string `json:"tags"`
	Userdata   string            `json:"userdata"`
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
		defer r.Body.Close()
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	}
	timeout := s.mgr.CreateTimeout()
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	opts := vm.CreateOpts{
		Name:       body.Name,
		Persistent: body.Persistent,
		CPUs:       body.CPUs,
		MemoryMB:   body.MemoryMB,
		DiskGB:     body.DiskGB,
		Image:      body.Image,
		Tags:       body.Tags,
		Userdata:   body.Userdata,
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

// Client talks to a running daemon over HTTP (unix socket via custom Transport).
type Client struct {
	Base string // e.g. http://grain (with unix dialer)
	HTTP *http.Client
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
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
	defer res.Body.Close()
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
	defer res.Body.Close()
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
	defer res.Body.Close()
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
