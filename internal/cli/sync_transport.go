package cli

import (
	"context"
	"io"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/api"
)

// syncFS is the guest-side surface required by sync.
// Paths are absolute guest paths (adapters bind the VM name).
type syncFS interface {
	Stat(ctx context.Context, path string) (*agent.FSInfo, error)
	ReadDir(ctx context.Context, path string) ([]agent.FSInfo, error)
	Mkdir(ctx context.Context, path string, recursive bool, mode string) error
	Remove(ctx context.Context, path string, recursive bool) error
	PutFile(ctx context.Context, path string, r io.Reader, size int64, opts agent.CPOpts) error
	GetFile(ctx context.Context, path string, w io.Writer) error
}

// agentSyncFS wraps *agent.Client for local dial after dialGuestAgent.
type agentSyncFS struct {
	c *agent.Client
}

func newAgentSyncFS(c *agent.Client) *agentSyncFS {
	return &agentSyncFS{c: c}
}

func (a *agentSyncFS) Stat(ctx context.Context, path string) (*agent.FSInfo, error) {
	return a.c.Stat(ctx, path)
}

func (a *agentSyncFS) ReadDir(ctx context.Context, path string) ([]agent.FSInfo, error) {
	return a.c.ReadDir(ctx, path)
}

func (a *agentSyncFS) Mkdir(ctx context.Context, path string, recursive bool, mode string) error {
	return a.c.Mkdir(ctx, path, recursive, mode)
}

func (a *agentSyncFS) Remove(ctx context.Context, path string, recursive bool) error {
	return a.c.Remove(ctx, path, recursive)
}

func (a *agentSyncFS) PutFile(ctx context.Context, path string, r io.Reader, size int64, opts agent.CPOpts) error {
	return a.c.PutFile(ctx, path, r, size, opts)
}

func (a *agentSyncFS) GetFile(ctx context.Context, path string, w io.Writer) error {
	return a.c.GetFile(ctx, path, w)
}

// apiSyncFS binds vmName into every api.Client call (remoteMode / GRAIN_API).
type apiSyncFS struct {
	c      *api.Client
	vmName string
}

func newAPISyncFS(c *api.Client, vmName string) *apiSyncFS {
	return &apiSyncFS{c: c, vmName: vmName}
}

func (a *apiSyncFS) Stat(ctx context.Context, path string) (*agent.FSInfo, error) {
	return a.c.Stat(ctx, a.vmName, path)
}

func (a *apiSyncFS) ReadDir(ctx context.Context, path string) ([]agent.FSInfo, error) {
	return a.c.ReadDir(ctx, a.vmName, path)
}

func (a *apiSyncFS) Mkdir(ctx context.Context, path string, recursive bool, mode string) error {
	return a.c.Mkdir(ctx, a.vmName, path, recursive, mode)
}

func (a *apiSyncFS) Remove(ctx context.Context, path string, recursive bool) error {
	return a.c.Remove(ctx, a.vmName, path, recursive)
}

func (a *apiSyncFS) PutFile(ctx context.Context, path string, r io.Reader, size int64, opts agent.CPOpts) error {
	return a.c.PutFile(ctx, a.vmName, path, r, size, opts)
}

func (a *apiSyncFS) GetFile(ctx context.Context, path string, w io.Writer) error {
	return a.c.GetFile(ctx, a.vmName, path, w)
}
