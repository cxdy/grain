package hypervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"time"

	"github.com/cxdy/grain/internal/vm"
)

// QMPSocketName is the basename of the QEMU monitor socket under the VM dir.
const QMPSocketName = "qmp.sock"

// QMPPathFor returns the QMP unix socket path for an instance.
func QMPPathFor(inst *vm.Instance) string {
	if inst == nil {
		return ""
	}
	if inst.QMPPath != "" {
		return inst.QMPPath
	}
	if inst.DiskPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(inst.DiskPath), QMPSocketName)
}

// QMPClient is a minimal QEMU machine protocol client.
type QMPClient struct {
	conn net.Conn
	enc  *json.Encoder
	dec  *json.Decoder
}

// DialQMP connects to a QMP unix socket and negotiates capabilities.
func DialQMP(ctx context.Context, sockPath string) (*QMPClient, error) {
	if sockPath == "" {
		return nil, fmt.Errorf("qmp: empty socket path")
	}
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(ctx, "unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("qmp dial: %w", err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	} else {
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	}

	c := &QMPClient{
		conn: conn,
		enc:  json.NewEncoder(conn),
		dec:  json.NewDecoder(conn),
	}

	// QMP greeting
	var greeting json.RawMessage
	if err := c.dec.Decode(&greeting); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("qmp greeting: %w", err)
	}
	if err := c.execute(ctx, "qmp_capabilities"); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return c, nil
}

// Close closes the underlying connection.
func (c *QMPClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *QMPClient) execute(ctx context.Context, cmd string) error {
	if dl, ok := ctx.Deadline(); ok {
		_ = c.conn.SetDeadline(dl)
	}
	if err := c.enc.Encode(map[string]any{"execute": cmd}); err != nil {
		return fmt.Errorf("qmp %s write: %w", cmd, err)
	}
	var resp map[string]any
	if err := c.dec.Decode(&resp); err != nil {
		return fmt.Errorf("qmp %s read: %w", cmd, err)
	}
	if e, ok := resp["error"]; ok {
		// error is typically {"class":"...","desc":"..."}
		if m, ok := e.(map[string]any); ok {
			if desc, ok := m["desc"].(string); ok && desc != "" {
				return fmt.Errorf("qmp %s: %s", cmd, desc)
			}
		}
		return fmt.Errorf("qmp %s: %v", cmd, e)
	}
	return nil
}

// Powerdown requests ACPI powerdown (system_powerdown).
func (c *QMPClient) Powerdown(ctx context.Context) error {
	return c.execute(ctx, "system_powerdown")
}

// Stop freezes guest vCPUs.
func (c *QMPClient) Stop(ctx context.Context) error {
	return c.execute(ctx, "stop")
}

// Cont resumes guest vCPUs.
func (c *QMPClient) Cont(ctx context.Context) error {
	return c.execute(ctx, "cont")
}

// Quit asks QEMU to exit.
func (c *QMPClient) Quit(ctx context.Context) error {
	return c.execute(ctx, "quit")
}

// qmpCommand dials, runs a single execute command, and closes.
func qmpCommand(ctx context.Context, sockPath, execute string) error {
	c, err := DialQMP(ctx, sockPath)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.execute(ctx, execute)
}
