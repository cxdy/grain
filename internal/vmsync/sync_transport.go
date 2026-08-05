package vmsync

import (
	"context"
	"io"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/api"
)

// FS is the guest-side surface required by sync.
// Paths are absolute guest paths (adapters bind the VM name).
type FS interface {
	Stat(ctx context.Context, path string) (*agent.FSInfo, error)
	ReadDir(ctx context.Context, path string) ([]agent.FSInfo, error)
	Mkdir(ctx context.Context, path string, recursive bool, mode string) error
	Remove(ctx context.Context, path string, recursive bool) error
	PutFile(ctx context.Context, path string, r io.Reader, size int64, opts agent.CPOpts) error
	GetFile(ctx context.Context, path string, w io.Writer) error
	// Symlink creates/replaces a symbolic link at path pointing to target.
	Symlink(ctx context.Context, path, target string) error
}

// syncFS is an alias for FS (legacy name used in apply).
type syncFS = FS

// agentSyncFS wraps *agent.Client for local dial after dialGuestAgent.
type agentSyncFS struct {
	c *agent.Client
}

// NewAgentFS wraps *agent.Client for local dial after dialGuestAgent.
func NewAgentFS(c *agent.Client) FS {
	return newAgentSyncFS(c)
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

func (a *agentSyncFS) Symlink(ctx context.Context, path, target string) error {
	return a.c.Symlink(ctx, path, target)
}

// apiSyncFS binds vmName into every api.Client call (remoteMode / GRAIN_API).
type apiSyncFS struct {
	c      *api.Client
	vmName string
}

// NewAPIFS binds vmName into every api.Client call (remoteMode / GRAIN_API).
func NewAPIFS(c *api.Client, vmName string) FS {
	return newAPISyncFS(c, vmName)
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

func (a *apiSyncFS) Symlink(ctx context.Context, path, target string) error {
	return a.c.Symlink(ctx, a.vmName, path, target)
}
