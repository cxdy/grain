package agent

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// RunSocksConnect is grain-agent socks-connect: dial host:port via
// GRAIN_FWD_SOCKS (SOCKS5h) and splice stdin/stdout. Used as ssh ProxyCommand.
func RunSocksConnect(ctx context.Context, args []string, env func(string) string, stdin io.Reader, stdout io.Writer) error {
	if len(args) != 2 {
		return fmt.Errorf("socks-connect: need host port")
	}
	proxy := strings.TrimSpace(env("GRAIN_FWD_SOCKS"))
	if proxy == "" {
		return fmt.Errorf("socks-connect: GRAIN_FWD_SOCKS is not set")
	}
	host := args[0]
	port, err := strconv.Atoi(args[1])
	if err != nil || port <= 0 || port > 65535 {
		return fmt.Errorf("socks-connect: bad port %q", args[1])
	}
	c, err := Socks5hDial(ctx, proxy, host, port)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	errCh := make(chan error, 2)
	go func() {
		_, e := io.Copy(c, stdin)
		errCh <- e
		_ = closeWrite(c)
	}()
	go func() {
		_, e := io.Copy(stdout, c)
		errCh <- e
	}()
	e1 := <-errCh
	e2 := <-errCh
	if e1 != nil && e1 != io.EOF {
		return e1
	}
	if e2 != nil && e2 != io.EOF {
		return e2
	}
	return nil
}
