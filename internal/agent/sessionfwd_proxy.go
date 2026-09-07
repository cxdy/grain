package agent

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// DialContextFunc dials a remote address on the client side of a -A session.
type DialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

// ServeMixedProxy handles one accepted guest-loopback connection: SOCKS5 (first
// byte 0x05) or HTTP CONNECT. SOCKS5 ATYP domain names are resolved via dial
// (client DNS), not the guest resolver.
func ServeMixedProxy(ctx context.Context, c net.Conn, dial DialContextFunc) error {
	if dial == nil {
		d := &net.Dialer{Timeout: 30 * time.Second}
		dial = d.DialContext
	}
	defer func() { _ = c.Close() }()
	br := bufio.NewReader(c)
	b, err := br.Peek(1)
	if err != nil {
		return err
	}
	if b[0] == 0x05 {
		return serveSOCKS5(ctx, c, br, dial)
	}
	return serveHTTPConnect(ctx, c, br, dial)
}

func serveSOCKS5(ctx context.Context, c net.Conn, br *bufio.Reader, dial DialContextFunc) error {
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(br, hdr); err != nil {
		return err
	}
	nmethods := int(hdr[1])
	if nmethods > 0 {
		if _, err := io.ReadFull(br, make([]byte, nmethods)); err != nil {
			return err
		}
	}
	if _, err := c.Write([]byte{0x05, 0x00}); err != nil {
		return err
	}
	req := make([]byte, 4)
	if _, err := io.ReadFull(br, req); err != nil {
		return err
	}
	if req[0] != 0x05 || req[1] != 0x01 {
		_, _ = c.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return fmt.Errorf("socks: only CONNECT is supported")
	}
	host, err := readSOCKS5Addr(br, req[3])
	if err != nil {
		return err
	}
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(br, portBuf); err != nil {
		return err
	}
	port := int(binary.BigEndian.Uint16(portBuf))
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	up, err := dial(ctx, "tcp", addr)
	if err != nil {
		_, _ = c.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return err
	}
	defer func() { _ = up.Close() }()
	if _, err := c.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return err
	}
	return relayConns(ctx, &prefixConn{Conn: c, r: br}, up)
}

func readSOCKS5Addr(br *bufio.Reader, atyp byte) (string, error) {
	switch atyp {
	case 0x01:
		ip := make([]byte, 4)
		if _, err := io.ReadFull(br, ip); err != nil {
			return "", err
		}
		return net.IP(ip).String(), nil
	case 0x04:
		ip := make([]byte, 16)
		if _, err := io.ReadFull(br, ip); err != nil {
			return "", err
		}
		return net.IP(ip).String(), nil
	case 0x03:
		lb := make([]byte, 1)
		if _, err := io.ReadFull(br, lb); err != nil {
			return "", err
		}
		host := make([]byte, int(lb[0]))
		if _, err := io.ReadFull(br, host); err != nil {
			return "", err
		}
		return string(host), nil
	default:
		return "", fmt.Errorf("socks: unsupported atyp %d", atyp)
	}
}

func serveHTTPConnect(ctx context.Context, c net.Conn, br *bufio.Reader, dial DialContextFunc) error {
	req, err := httpReadRequestLine(br)
	if err != nil {
		return err
	}
	if !strings.EqualFold(req.method, "CONNECT") {
		_, _ = fmt.Fprintf(c, "HTTP/1.1 405 Method Not Allowed\r\nConnection: close\r\n\r\n")
		return fmt.Errorf("http proxy: only CONNECT is supported")
	}
	addr := req.target
	if !strings.Contains(addr, ":") {
		addr += ":443"
	}
	up, err := dial(ctx, "tcp", addr)
	if err != nil {
		_, _ = fmt.Fprintf(c, "HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\n\r\n")
		return err
	}
	defer func() { _ = up.Close() }()
	if _, err := io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return err
	}
	return relayConns(ctx, &prefixConn{Conn: c, r: br}, up)
}

type httpReqLine struct {
	method, target string
}

func httpReadRequestLine(br *bufio.Reader) (httpReqLine, error) {
	line, err := br.ReadString('\n')
	if err != nil {
		return httpReqLine{}, err
	}
	parts := strings.Fields(strings.TrimSpace(line))
	if len(parts) < 2 {
		return httpReqLine{}, fmt.Errorf("http: bad request line")
	}
	for {
		h, err := br.ReadString('\n')
		if err != nil {
			return httpReqLine{}, err
		}
		if h == "\r\n" || h == "\n" {
			break
		}
	}
	return httpReqLine{method: parts[0], target: parts[1]}, nil
}

type prefixConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *prefixConn) Read(p []byte) (int, error) { return c.r.Read(p) }

func relayConns(ctx context.Context, a, b net.Conn) error {
	errCh := make(chan error, 2)
	copyFn := func(dst, src net.Conn) {
		_, err := io.Copy(dst, src)
		errCh <- err
		_ = closeWrite(dst)
	}
	go copyFn(a, b)
	go copyFn(b, a)
	select {
	case <-ctx.Done():
		_ = a.Close()
		_ = b.Close()
		return ctx.Err()
	case err := <-errCh:
		_ = a.Close()
		_ = b.Close()
		<-errCh
		if err == io.EOF {
			return nil
		}
		return err
	}
}

func closeWrite(c net.Conn) error {
	type cw interface{ CloseWrite() error }
	if x, ok := c.(cw); ok {
		return x.CloseWrite()
	}
	return nil
}

// ProxyAgentConn copies bytes between a guest unix connection and the client
// SSH_AUTH_SOCK. The guest never sees private keys.
func ProxyAgentConn(ctx context.Context, clientSock string, guest net.Conn) error {
	d := net.Dialer{Timeout: 5 * time.Second}
	up, err := d.DialContext(ctx, "unix", clientSock)
	if err != nil {
		return err
	}
	defer func() { _ = up.Close() }()
	return relayConns(ctx, guest, up)
}

// Socks5hDial dials host:port through a SOCKS5h proxy (domain sent to proxy).
func Socks5hDial(ctx context.Context, proxyAddr, host string, port int) (net.Conn, error) {
	d := net.Dialer{Timeout: 15 * time.Second}
	c, err := d.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = c.Close()
		}
	}()
	if _, err := c.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return nil, err
	}
	ack := make([]byte, 2)
	if _, err := io.ReadFull(c, ack); err != nil {
		return nil, err
	}
	if ack[0] != 0x05 || ack[1] != 0x00 {
		return nil, fmt.Errorf("socks greeting rejected")
	}
	if len(host) > 255 {
		return nil, fmt.Errorf("socks host too long")
	}
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	req = append(req, []byte(host)...)
	var pb [2]byte
	binary.BigEndian.PutUint16(pb[:], uint16(port))
	req = append(req, pb[:]...)
	if _, err := c.Write(req); err != nil {
		return nil, err
	}
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(c, hdr); err != nil {
		return nil, err
	}
	if hdr[1] != 0x00 {
		return nil, fmt.Errorf("socks connect failed status %d", hdr[1])
	}
	if err := skipSOCKS5BindAddr(c, hdr[3]); err != nil {
		return nil, err
	}
	ok = true
	return c, nil
}

func skipSOCKS5BindAddr(c net.Conn, atyp byte) error {
	switch atyp {
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
		return fmt.Errorf("socks bind atyp %d", atyp)
	}
}
