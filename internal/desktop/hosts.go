package desktop

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cxdy/grain/client"
)

// HostProbe is reachability for one connection profile.
type HostProbe struct {
	Name      string `json:"name"`
	API       string `json:"api,omitempty"`
	Local     bool   `json:"local"`
	Reachable bool   `json:"reachable"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ProbeConnections checks each profile with a short timeout (parallel).
func ProbeConnections(ctx context.Context, cfg Config, dial DialFunc) []HostProbe {
	if dial == nil {
		dial = DialConnection
	}
	conns := cfg.ActiveConnections()
	out := make([]HostProbe, len(conns))
	var wg sync.WaitGroup
	for i, conn := range conns {
		wg.Add(1)
		go func(i int, conn Connection) {
			defer wg.Done()
			p := HostProbe{
				Name:  conn.Name,
				API:   conn.API,
				Local: conn.IsLocal(),
			}
			pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			c, err := dial(conn, cfg)
			if err != nil {
				p.Reachable = false
				p.Error = err.Error()
				out[i] = p
				return
			}
			if err := c.Health(pctx); err != nil {
				p.Reachable = false
				p.Error = err.Error()
				out[i] = p
				return
			}
			p.Reachable = true
			if info, err := c.Info(pctx); err == nil && info != nil {
				if v := strings.TrimSpace(info["version"]); v != "" {
					if !strings.HasPrefix(v, "v") {
						v = "v" + v
					}
					p.Version = v
				}
			}
			out[i] = p
		}(i, conn)
	}
	wg.Wait()
	return out
}

// TestConnection dials an explicit API URL (+ optional token) for Settings.
func TestConnection(ctx context.Context, api, token string) (HostProbe, error) {
	api = NormalizeAPIURL(strings.TrimSpace(api))
	if api == "" {
		return HostProbe{}, fmt.Errorf("API endpoint is required")
	}
	p := HostProbe{Name: "test", API: api, Local: false}
	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	c, err := client.DialHTTP(api, strings.TrimSpace(token))
	if err != nil {
		p.Error = err.Error()
		return p, err
	}
	if err := c.Health(pctx); err != nil {
		p.Reachable = false
		p.Error = err.Error()
		return p, err
	}
	p.Reachable = true
	if info, err := c.Info(pctx); err == nil && info != nil {
		if v := strings.TrimSpace(info["version"]); v != "" {
			if !strings.HasPrefix(v, "v") {
				v = "v" + v
			}
			p.Version = v
		}
	}
	return p, nil
}
