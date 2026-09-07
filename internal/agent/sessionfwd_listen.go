package agent

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
)

// GuestFwd holds guest-loopback listeners for one grain sh -A session.
type GuestFwd struct {
	AgentSock string
	ProxyAddr string
	Dir       string

	agentLn net.Listener
	proxyLn net.Listener
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// FwdHandler is called for each accepted guest connection (chanKind is
// "agent" or "proxy"). The handler owns conn.
type FwdHandler func(chanKind string, c net.Conn)

// StartGuestFwdListeners binds a unix SSH-agent socket and a TCP mixed
// HTTP/SOCKS proxy on 127.0.0.1. Close removes the sockets.
func StartGuestFwdListeners(ctx context.Context, onAccept FwdHandler) (*GuestFwd, error) {
	dir, err := os.MkdirTemp("", "gf-*")
	if err != nil {
		return nil, err
	}
	agentSock := filepath.Join(dir, "agent.sock")
	agentLn, err := net.Listen("unix", agentSock)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("listen ssh-agent sock: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = agentLn.Close()
		_ = os.RemoveAll(dir)
		return nil, err
	}
	if err := os.Chmod(agentSock, 0o600); err != nil {
		_ = agentLn.Close()
		_ = os.RemoveAll(dir)
		return nil, err
	}
	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = agentLn.Close()
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("listen client proxy: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	g := &GuestFwd{
		AgentSock: agentSock,
		ProxyAddr: proxyLn.Addr().String(),
		Dir:       dir,
		agentLn:   agentLn,
		proxyLn:   proxyLn,
		cancel:    cancel,
	}
	g.acceptLoop(ctx, "agent", agentLn, onAccept)
	g.acceptLoop(ctx, "proxy", proxyLn, onAccept)
	return g, nil
}

func (g *GuestFwd) acceptLoop(ctx context.Context, kind string, ln net.Listener, onAccept FwdHandler) {
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			if ctx.Err() != nil {
				_ = c.Close()
				return
			}
			if onAccept != nil {
				go onAccept(kind, c)
			} else {
				_ = c.Close()
			}
		}
	}()
}

// InstallSSHWrapper writes Dir/ssh so stock ssh on PATH uses SOCKS5h.
func (g *GuestFwd) InstallSSHWrapper(agentBin string) error {
	if g == nil {
		return fmt.Errorf("nil GuestFwd")
	}
	return writeSessionSSHWrapper(g.Dir, agentBin)
}

// ChownSession sets dir 0700 and agent.sock 0600 owned by uid/gid so the
// login-shell user can connect to SSH_AUTH_SOCK (agent often starts as root).
func (g *GuestFwd) ChownSession(uid, gid int) error {
	if g == nil {
		return fmt.Errorf("nil GuestFwd")
	}
	if err := os.Chmod(g.Dir, 0o700); err != nil {
		return err
	}
	if err := os.Chown(g.Dir, uid, gid); err != nil {
		return fmt.Errorf("chown fwd dir: %w", err)
	}
	if err := os.Chmod(g.AgentSock, 0o600); err != nil {
		return err
	}
	if err := os.Chown(g.AgentSock, uid, gid); err != nil {
		return fmt.Errorf("chown fwd agent sock: %w", err)
	}
	wrap := filepath.Join(g.Dir, "ssh")
	if st, err := os.Lstat(wrap); err == nil && !st.IsDir() {
		if err := os.Chmod(wrap, 0o755); err != nil {
			return err
		}
		if err := os.Chown(wrap, uid, gid); err != nil {
			return fmt.Errorf("chown fwd ssh wrapper: %w", err)
		}
	}
	return nil
}

// Close stops listeners and deletes the session socket directory.
func (g *GuestFwd) Close() {
	if g == nil {
		return
	}
	if g.cancel != nil {
		g.cancel()
	}
	if g.agentLn != nil {
		_ = g.agentLn.Close()
	}
	if g.proxyLn != nil {
		_ = g.proxyLn.Close()
	}
	g.wg.Wait()
	if g.Dir != "" {
		_ = os.RemoveAll(g.Dir)
	}
}
