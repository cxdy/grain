package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/vm"
)

func TestParsePublishFlagsError(t *testing.T) {
	t.Parallel()
	_, err := parsePublishFlags([]string{"8080:80", "bad"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParsePublishFlagRanges(t *testing.T) {
	t.Parallel()
	if _, err := parsePublishFlag("70000:80"); err == nil {
		t.Fatal("host out of range")
	}
	if _, err := parsePublishFlag("8080:70000"); err == nil {
		t.Fatal("guest out of range")
	}
	if _, err := parsePublishFlag("tcp/18080:80"); err != nil {
		t.Fatal(err)
	}
	// invalid host/guest atoi
	if _, err := parsePublishFlag("x:80"); err == nil {
		t.Fatal("invalid host")
	}
	if _, err := parsePublishFlag("8080:y"); err == nil {
		t.Fatal("invalid guest")
	}
}

func TestCmdFwdTunnelJSONAndText(t *testing.T) {
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.Method == http.MethodGet && r.URL.Path == "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{
				Name: "web",
				Forwards: []vm.PortForward{
					{HostPort: 3000, GuestPort: 3000, Proto: "tcp"},
				},
				LiveForwards: []vm.LiveForward{
					{HostPort: 8080, GuestPort: 80},
				},
			}})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/vms/"):
			_ = json.NewEncoder(w).Encode(&vm.Instance{
				Name: "web",
				Forwards: []vm.PortForward{
					{HostPort: 3000, GuestPort: 3000},
				},
			})
		default:
			http.NotFound(w, r)
		}
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""

	// text lines for all VMs
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	cmd := cmdFwdTunnel(&cfg)
	cmd.SetArgs([]string{"--host", "sandbox.example.com", "--user", "alice"})
	err = cmd.Execute()
	_ = w.Close()
	os.Stdout = old
	outB, _ := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatal(err)
	}
	out := string(outB)
	if !strings.Contains(out, "ssh -N -L 3000:127.0.0.1:3000 alice@sandbox.example.com") {
		t.Fatalf("tunnel out: %q", out)
	}

	// named VM + JSON
	r2, w2, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w2
	cmd = cmdFwdTunnel(&cfg)
	cmd.SetArgs([]string{"web", "--json", "--host", "h"})
	err = cmd.Execute()
	_ = w2.Close()
	os.Stdout = old
	outB, _ = io.ReadAll(r2)
	_ = r2.Close()
	if err != nil {
		t.Fatal(err)
	}
	var lines []tunnelLine
	if err := json.Unmarshal(outB, &lines); err != nil {
		t.Fatalf("json: %v body=%s", err, outB)
	}
	if len(lines) != 1 || lines[0].HostPort != 3000 {
		t.Fatalf("lines=%+v", lines)
	}
}

func TestCmdFwdTunnelEmptyAndNoPorts(t *testing.T) {
	// no VMs
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(200)
		case "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{})
		default:
			http.NotFound(w, r)
		}
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""
	if err := cmdFwdTunnel(&cfg).Execute(); err == nil || !strings.Contains(err.Error(), "no vms") {
		t.Fatalf("want no vms: %v", err)
	}

	// VMs but no host ports
	srv2 := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(200)
		case "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{
				Name: "empty",
				Forwards: []vm.PortForward{
					{HostPort: 0, GuestPort: 80},
					{HostPort: 5353, GuestPort: 53, Proto: "udp"},
				},
			}})
		default:
			http.NotFound(w, r)
		}
	})
	useRemoteAPI(t, srv2.URL)

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	cmd := cmdFwdTunnel(&cfg)
	err = cmd.Execute()
	_ = w.Close()
	os.Stdout = old
	outB, _ := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(outB), "no host ports") {
		t.Fatalf("out=%q", outB)
	}

	// JSON empty ports
	r2, w2, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w2
	cmd = cmdFwdTunnel(&cfg)
	cmd.SetArgs([]string{"--json"})
	err = cmd.Execute()
	_ = w2.Close()
	os.Stdout = old
	outB, _ = io.ReadAll(r2)
	_ = r2.Close()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(outB)) != "[]" {
		t.Fatalf("want [], got %q", outB)
	}
}

func TestCmdFwdLsBranches(t *testing.T) {
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.Method == http.MethodGet && r.URL.Path == "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{
				{
					Name: "fc1", Status: "running",
					SSHPort: 2200, AgentPort: 7700, IP: "10.77.1.2",
					Forwards: []vm.PortForward{
						{HostPort: 8080, GuestPort: 80, Proto: "tcp"},
						{HostPort: 2200, GuestPort: 22}, // skip (ssh)
						{HostPort: 0, GuestPort: 99},
						{HostPort: 5353, GuestPort: 53, Proto: "udp"},
					},
					LiveForwards: []vm.LiveForward{
						{HostPort: 2200, GuestPort: 22, PID: 11},
						{HostPort: 7700, GuestPort: 7475, PID: 12},
						{HostPort: 9090, GuestPort: 90, PID: 13},
					},
				},
				{Name: "bare", Status: "stopped"},
			})
		default:
			http.NotFound(w, r)
		}
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	if err := cmdFwdLs(&cfg).Execute(); err != nil {
		_ = w.Close()
		os.Stdout = old
		t.Fatal(err)
	}
	_ = w.Close()
	os.Stdout = old
	outB, _ := io.ReadAll(r)
	_ = r.Close()
	out := string(outB)
	for _, want := range []string{"ssh pid=", "agent pid=", "publish", "udp", "live pid=", "(none)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestCmdFwdLsEmptyAndNone(t *testing.T) {
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(200)
		case "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{})
		default:
			http.NotFound(w, r)
		}
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	if err := cmdFwdLs(&cfg).Execute(); err != nil {
		_ = w.Close()
		os.Stdout = old
		t.Fatal(err)
	}
	_ = w.Close()
	os.Stdout = old
	outB, _ := io.ReadAll(r)
	_ = r.Close()
	if !strings.Contains(string(outB), "no vms") {
		t.Fatalf("%q", outB)
	}
}

func TestCmdFwdAddRmSingleArg(t *testing.T) {
	var addedHost, removedPort int
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.Method == http.MethodGet && r.URL.Path == "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "only", Status: "running"}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/forwards"):
			var body struct {
				HostPort  int `json:"host_port"`
				GuestPort int `json:"guest_port"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			addedHost = body.HostPort
			_ = json.NewEncoder(w).Encode(&vm.LiveForward{HostPort: body.HostPort, GuestPort: body.GuestPort, PID: 1})
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/forwards/"):
			parts := strings.Split(r.URL.Path, "/")
			_, _ = fmt.Sscanf(parts[len(parts)-1], "%d", &removedPort)
			w.WriteHeader(204)
		default:
			http.NotFound(w, r)
		}
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""

	add := cmdFwdAdd(&cfg)
	add.SetArgs([]string{"18080:80"})
	if err := add.Execute(); err != nil {
		t.Fatal(err)
	}
	if addedHost != 18080 {
		t.Fatalf("addedHost=%d", addedHost)
	}

	rm := cmdFwdRm(&cfg)
	rm.SetArgs([]string{"18080"})
	if err := rm.Execute(); err != nil {
		t.Fatal(err)
	}
	if removedPort != 18080 {
		t.Fatalf("removed=%d", removedPort)
	}

	// bad host port / zero
	rm = cmdFwdRm(&cfg)
	rm.SetArgs([]string{"only", "0"})
	if err := rm.Execute(); err == nil {
		t.Fatal("want positive host port")
	}
	rm = cmdFwdRm(&cfg)
	rm.SetArgs([]string{"notaport"})
	if err := rm.Execute(); err == nil {
		t.Fatal("want atoi error")
	}
}

func TestCmdFwdSubcommandsIncludeTunnel(t *testing.T) {
	cfg := ""
	root := cmdFwd(&cfg)
	found := false
	for _, c := range root.Commands() {
		if c.Name() == "tunnel" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing tunnel")
	}
}
