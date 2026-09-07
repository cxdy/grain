package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSessionFwdEnv(t *testing.T) {
	t.Parallel()
	wrapDir, err := os.MkdirTemp("", "gf-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(wrapDir) })
	if err := writeSessionSSHWrapper(wrapDir, "/usr/local/bin/grain-agent"); err != nil {
		t.Fatal(err)
	}
	env := SessionFwdEnv("/run/grain/fwd/agent.sock", "127.0.0.1:9050", "/usr/local/bin/grain-agent", wrapDir)
	got := map[string]string{}
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			t.Fatalf("bad kv %q", kv)
		}
		got[k] = v
	}
	if got["SSH_AUTH_SOCK"] != "/run/grain/fwd/agent.sock" {
		t.Fatalf("SSH_AUTH_SOCK=%q", got["SSH_AUTH_SOCK"])
	}
	if got["HTTP_PROXY"] != "http://127.0.0.1:9050" || got["HTTPS_PROXY"] != "http://127.0.0.1:9050" {
		t.Fatalf("http proxy env: %v", got)
	}
	if got["ALL_PROXY"] != "socks5h://127.0.0.1:9050" {
		t.Fatalf("ALL_PROXY=%q", got["ALL_PROXY"])
	}
	wrap := filepath.Join(wrapDir, "ssh")
	if got["GIT_SSH_COMMAND"] != wrap {
		t.Fatalf("GIT_SSH_COMMAND=%q want wrapper %q", got["GIT_SSH_COMMAND"], wrap)
	}
	if !strings.HasPrefix(got["PATH"], wrapDir+string(os.PathListSeparator)) && got["PATH"] != wrapDir {
		t.Fatalf("PATH should start with wrapper dir, got %q", got["PATH"])
	}
	body, err := os.ReadFile(wrap)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("socks-connect")) || !bytes.Contains(body, []byte("ProxyCommand")) {
		t.Fatalf("wrapper missing ProxyCommand/socks-connect: %s", body)
	}
	st, err := os.Stat(wrap)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode()&0o111 == 0 {
		t.Fatalf("wrapper not executable: %s", st.Mode())
	}
}

func TestExtraEnvForShellFwdQuery(t *testing.T) {
	t.Parallel()
	base := extraEnvForShell(map[string]string{"term": "xterm"}, nil)
	for _, kv := range base {
		if strings.HasPrefix(kv, "SSH_AUTH_SOCK=") {
			t.Fatalf("fwd disabled leaked SSH_AUTH_SOCK: %v", base)
		}
	}
	fwd := SessionFwdEnv("/tmp/a.sock", "127.0.0.1:1", "/bin/grain-agent", "")
	with := extraEnvForShell(map[string]string{"term": "xterm"}, fwd)
	var sawSock, sawAll, sawGit bool
	for _, kv := range with {
		if strings.HasPrefix(kv, "SSH_AUTH_SOCK=") {
			sawSock = true
		}
		if strings.HasPrefix(kv, "ALL_PROXY=") {
			sawAll = true
		}
		if strings.HasPrefix(kv, "GIT_SSH_COMMAND=") {
			sawGit = true
		}
	}
	if !sawSock || !sawAll || !sawGit {
		t.Fatalf("fwd env missing: %v", with)
	}
}

func TestProxyAgentConnSSHAddL(t *testing.T) {
	clientSock, _ := startTestSSHAgent(t)
	key := filepath.Join(t.TempDir(), "id_ed25519")
	out, err := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput()
	if err != nil {
		t.Skipf("ssh-keygen: %v %s", err, out)
	}
	add := exec.Command("ssh-add", key)
	add.Env = append(os.Environ(), "SSH_AUTH_SOCK="+clientSock)
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("ssh-add: %v %s", err, out)
	}

	gdir, err := os.MkdirTemp("", "gf-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(gdir) })
	guestPath := filepath.Join(gdir, "g")
	ln, err := net.Listen("unix", guestPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_ = ProxyAgentConn(ctx, clientSock, c)
				_ = c.Close()
			}()
		}
	}()

	list := exec.Command("ssh-add", "-l")
	list.Env = append(os.Environ(), "SSH_AUTH_SOCK="+guestPath)
	out, err = list.CombinedOutput()
	if err != nil {
		t.Fatalf("ssh-add -l via proxy: %v %s", err, out)
	}
	if !strings.Contains(string(out), "ED25519") && !strings.Contains(string(out), "SHA256:") {
		t.Fatalf("expected test key identity, got %s", out)
	}
}

func TestServeMixedProxySOCKS5hDomainUsesClientDial(t *testing.T) {
	t.Parallel()
	wantBody := "from-client-dial"
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = backend.Close() }()
	go func() {
		for {
			c, err := backend.Accept()
			if err != nil {
				return
			}
			_, _ = c.Write([]byte(wantBody))
			_ = c.Close()
		}
	}()
	backendHost, backendPort, _ := net.SplitHostPort(backend.Addr().String())
	_ = backendHost

	var sawDomain string
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		_ = network
		host, _, _ := net.SplitHostPort(address)
		sawDomain = host
		if host != "repo.example.invalid" {
			return nil, fmt.Errorf("unexpected dial %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", backendPort))
	}

	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = proxyLn.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		c, err := proxyLn.Accept()
		if err != nil {
			return
		}
		_ = ServeMixedProxy(ctx, c, dial)
	}()

	pc, err := net.Dial("tcp", proxyLn.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pc.Close() }()
	if err := socks5hConnect(pc, "repo.example.invalid", 22); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(wantBody))
	if _, err := io.ReadFull(pc, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != wantBody {
		t.Fatalf("got %q", buf)
	}
	if sawDomain != "repo.example.invalid" {
		t.Fatalf("client dial host %q", sawDomain)
	}
}

func TestServeMixedProxyHTTPCONNECT(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("http-ok"))
	})
	backend := httptestServerOnLoopback(t, mux)

	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = proxyLn.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		c, err := proxyLn.Accept()
		if err != nil {
			return
		}
		_ = ServeMixedProxy(ctx, c, dial)
	}()

	pc, err := net.Dial("tcp", proxyLn.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pc.Close() }()
	_, backendPort, _ := net.SplitHostPort(backend)
	req := fmt.Sprintf("CONNECT 127.0.0.1:%s HTTP/1.1\r\nHost: 127.0.0.1:%s\r\n\r\n", backendPort, backendPort)
	if _, err := pc.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(pc)
	status, err := br.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "200") {
		t.Fatalf("CONNECT status %q", status)
	}
	// Drain headers.
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "\r\n" {
			break
		}
	}
	if _, err := pc.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(br)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("http-ok")) {
		t.Fatalf("body %q", body)
	}
}

func TestSocks5hDialAndRunSocksConnect(t *testing.T) {
	t.Parallel()
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = backend.Close() }()
	go func() {
		for {
			c, err := backend.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				buf := make([]byte, 4)
				_, _ = io.ReadFull(c, buf)
				_, _ = c.Write([]byte("pong"))
				_ = c.Close()
			}(c)
		}
	}()
	_, bport, _ := net.SplitHostPort(backend.Addr().String())

	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = proxyLn.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			c, err := proxyLn.Accept()
			if err != nil {
				return
			}
			go func() { _ = ServeMixedProxy(ctx, c, nil) }()
		}
	}()

	c, err := Socks5hDial(ctx, proxyLn.Addr().String(), "127.0.0.1", atoiPort(t, bport))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	out := make([]byte, 4)
	if _, err := io.ReadFull(c, out); err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if string(out) != "pong" {
		t.Fatalf("got %q", out)
	}

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	env := func(k string) string {
		if k == "GRAIN_FWD_SOCKS" {
			return proxyLn.Addr().String()
		}
		return ""
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunSocksConnect(ctx, []string{"127.0.0.1", bport}, env, inR, outW)
		_ = outW.Close()
	}()
	if _, err := inW.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	pong := make([]byte, 4)
	if _, err := io.ReadFull(outR, pong); err != nil {
		t.Fatalf("socks-connect read: %v", err)
	}
	_ = inW.Close()
	if string(pong) != "pong" {
		t.Fatalf("socks-connect out %q", pong)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("socks-connect hung")
	}
}

func TestTwoGuestFwdSessionsIsolated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	a, err := StartGuestFwdListeners(ctx, func(string, net.Conn) {})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := StartGuestFwdListeners(ctx, func(string, net.Conn) {})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if a.AgentSock == b.AgentSock || a.ProxyAddr == b.ProxyAddr {
		t.Fatalf("sessions must not share sockets: %+v %+v", a, b)
	}
	ea := SessionFwdEnv(a.AgentSock, a.ProxyAddr, "grain-agent", "")
	eb := SessionFwdEnv(b.AgentSock, b.ProxyAddr, "grain-agent", "")
	if strings.Join(ea, ",") == strings.Join(eb, ",") {
		t.Fatal("session env must differ")
	}
}

func atoiPort(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestLoginShellReappliesWrapperPATH(t *testing.T) {
	t.Parallel()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not found")
	}
	wrapDir, err := os.MkdirTemp("", "gf-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(wrapDir) })
	if err := writeSessionSSHWrapper(wrapDir, "/usr/local/bin/grain-agent"); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(wrapDir, "ssh")
	home := t.TempDir()

	fwdEnv := SessionFwdEnv("/run/a.sock", "127.0.0.1:9", "/usr/local/bin/grain-agent", wrapDir)
	args := loginShellArgv(bash, fwdEnv)
	if len(args) != 2 || args[0] != "-lc" {
		t.Fatalf("loginShellArgs: %v", args)
	}
	if !strings.Contains(args[1], wrapDir) || !strings.Contains(args[1], "export PATH") {
		t.Fatalf("script should re-prepend wrapDir after login: %q", args[1])
	}
	// Same -lc body as startLoginShell, but command -v instead of exec -i.
	script := strings.Replace(args[1], "exec "+shellSingleQuote(bash)+" -i", "command -v ssh", 1)
	if script == args[1] {
		t.Fatalf("could not substitute exec in %q", args[1])
	}
	cmd := exec.Command(bash, "-lc", script)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + home, "USER=test"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash -lc: %v %s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if got != want {
		t.Fatalf("command -v ssh after login PATH fix = %q want %q", got, want)
	}

	if loginShellArgs("/bin/sh", "")[0] != "-l" {
		t.Fatal("without wrapDir should stay a normal login shell")
	}
}

func TestChownSessionAppliesOwnerAndMode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fwd, err := StartGuestFwdListeners(ctx, func(string, net.Conn) {})
	if err != nil {
		t.Fatal(err)
	}
	defer fwd.Close()
	if err := fwd.InstallSSHWrapper("/usr/local/bin/grain-agent"); err != nil {
		t.Fatal(err)
	}
	uid, gid := os.Getuid(), os.Getgid()
	if err := fwd.ChownSession(uid, gid); err != nil {
		t.Fatal(err)
	}
	dirInfo, err := os.Stat(fwd.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %o want 0700", dirInfo.Mode().Perm())
	}
	sockInfo, err := os.Lstat(fwd.AgentSock)
	if err != nil {
		t.Fatal(err)
	}
	if sockInfo.Mode()&os.ModeSocket == 0 {
		t.Fatal("agent path is not a socket")
	}
	if sockInfo.Mode().Perm() != 0o600 {
		t.Fatalf("sock mode %o want 0600", sockInfo.Mode().Perm())
	}
	sys, ok := sockInfo.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("stat not Stat_t")
	}
	if int(sys.Uid) != uid || int(sys.Gid) != gid {
		t.Fatalf("sock owner %d:%d want %d:%d", sys.Uid, sys.Gid, uid, gid)
	}
	wrap := filepath.Join(fwd.Dir, "ssh")
	wst, err := os.Stat(wrap)
	if err != nil {
		t.Fatal(err)
	}
	if wst.Mode().Perm()&0o100 == 0 {
		t.Fatalf("wrapper not owner-executable: %s", wst.Mode())
	}
}

func TestSessionFwdListenTeardown(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fwd, err := StartGuestFwdListeners(ctx, func(chanKind string, c net.Conn) {
		_ = c.Close()
	})
	if err != nil {
		t.Fatal(err)
	}
	if fwd.AgentSock == "" || fwd.ProxyAddr == "" {
		t.Fatalf("%+v", fwd)
	}
	if _, err := os.Lstat(fwd.AgentSock); err != nil {
		t.Fatal(err)
	}
	fwd.Close()
	if _, err := os.Lstat(fwd.AgentSock); !os.IsNotExist(err) {
		t.Fatalf("agent sock still present: %v", err)
	}
	c, err := net.DialTimeout("tcp", fwd.ProxyAddr, 200*time.Millisecond)
	if err == nil {
		_ = c.Close()
		t.Fatal("proxy still listening after Close")
	}
}

func startTestSSHAgent(t *testing.T) (sock string, pid int) {
	t.Helper()
	adir, err := os.MkdirTemp("", "gf-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(adir) })
	sock = filepath.Join(adir, "a")
	cmd := exec.Command("ssh-agent", "-a", sock, "-D")
	if err := cmd.Start(); err != nil {
		t.Skipf("ssh-agent: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := ValidateClientSSHAuthSock(sock); err == nil {
			return sock, cmd.Process.Pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("ssh-agent socket never became ready")
	return "", 0
}

func socks5hConnect(c net.Conn, host string, port int) error {
	if _, err := c.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return err
	}
	ack := make([]byte, 2)
	if _, err := io.ReadFull(c, ack); err != nil {
		return err
	}
	if ack[0] != 0x05 || ack[1] != 0x00 {
		return fmt.Errorf("socks greeting %v", ack)
	}
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	req = append(req, []byte(host)...)
	var pb [2]byte
	binary.BigEndian.PutUint16(pb[:], uint16(port))
	req = append(req, pb[:]...)
	if _, err := c.Write(req); err != nil {
		return err
	}
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(c, hdr); err != nil {
		return err
	}
	if hdr[1] != 0x00 {
		return fmt.Errorf("socks reply status %d", hdr[1])
	}
	switch hdr[3] {
	case 0x01:
		_, err := io.ReadFull(c, make([]byte, 6))
		return err
	case 0x04:
		_, err := io.ReadFull(c, make([]byte, 18))
		return err
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(c, l); err != nil {
			return err
		}
		_, err := io.ReadFull(c, make([]byte, int(l[0])+2))
		return err
	default:
		return fmt.Errorf("socks atyp %d", hdr[3])
	}
}

func httptestServerOnLoopback(t *testing.T, h http.Handler) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String()
}
