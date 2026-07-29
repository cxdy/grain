package mcp

import (
	"context"
	"io"

	"github.com/cxdy/grain/client"
	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/vmsync"
)

// clientSyncFS adapts client.Client to vmsync.FS (daemon-proxied agent routes).
type clientSyncFS struct {
	c      *client.Client
	vmName string
}

func newClientSyncFS(c *client.Client, vmName string) vmsync.FS {
	return &clientSyncFS{c: c, vmName: vmName}
}

func (a *clientSyncFS) Stat(ctx context.Context, path string) (*agent.FSInfo, error) {
	info, err := a.c.Stat(ctx, a.vmName, path)
	if err != nil {
		return nil, err
	}
	return clientFSInfoToAgent(info), nil
}

func (a *clientSyncFS) ReadDir(ctx context.Context, path string) ([]agent.FSInfo, error) {
	list, err := a.c.ReadDir(ctx, a.vmName, path)
	if err != nil {
		return nil, err
	}
	out := make([]agent.FSInfo, len(list))
	for i := range list {
		out[i] = *clientFSInfoToAgent(&list[i])
	}
	return out, nil
}

func (a *clientSyncFS) Mkdir(ctx context.Context, path string, recursive bool, mode string) error {
	return a.c.Mkdir(ctx, a.vmName, path, recursive, mode)
}

func (a *clientSyncFS) Remove(ctx context.Context, path string, recursive bool) error {
	return a.c.Remove(ctx, a.vmName, path, recursive)
}

func (a *clientSyncFS) PutFile(ctx context.Context, path string, r io.Reader, size int64, opts agent.CPOpts) error {
	return a.c.PutFile(ctx, a.vmName, path, r, size, client.CPOpts{
		UID:  opts.UID,
		GID:  opts.GID,
		Mode: opts.Mode,
	})
}

func (a *clientSyncFS) GetFile(ctx context.Context, path string, w io.Writer) error {
	return a.c.GetFile(ctx, a.vmName, path, w)
}

func clientFSInfoToAgent(info *client.FSInfo) *agent.FSInfo {
	if info == nil {
		return nil
	}
	return &agent.FSInfo{
		Name:  info.Name,
		Type:  info.Type,
		Size:  info.Size,
		Mtime: info.Mtime,
		Mode:  info.Mode,
	}
}
