package vmsync

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// InventoryHost walks hostRoot with Lstat (no follow of dir symlinks).
// Keys are slash-separated relative paths from root (no leading ./).
// Ignored directories are not descended.
func InventoryHost(hostRoot string, ign *syncIgnore) (map[string]*syncInvEntry, error) {
	hostRoot = filepath.Clean(hostRoot)
	out := make(map[string]*syncInvEntry)
	err := filepath.WalkDir(hostRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(hostRoot, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil // root itself is not an inventory entry
		}
		relSlash := filepath.ToSlash(rel)
		if d.IsDir() {
			if ign != nil && ign.MatchDir(relSlash) {
				return filepath.SkipDir
			}
		} else if ign != nil && ign.Match(relSlash) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		// Do not follow directory symlinks.
		if info.Mode()&os.ModeSymlink != 0 {
			out[relSlash] = &syncInvEntry{
				Type:  "symlink",
				Size:  info.Size(),
				Mtime: info.ModTime().Unix(),
				Mode:  formatMode(info.Mode()),
			}
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		entry := &syncInvEntry{
			Size:  info.Size(),
			Mtime: info.ModTime().Unix(),
			Mode:  formatMode(info.Mode()),
		}
		if info.IsDir() {
			entry.Type = "directory"
			entry.Size = 0
		} else {
			entry.Type = "file"
		}
		out[relSlash] = entry
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("host inventory %s: %w", hostRoot, err)
	}
	return out, nil
}

func formatMode(m os.FileMode) string {
	return fmt.Sprintf("%04o", m.Perm())
}

// InventoryGuest BFS-walks guestRoot via ReadDir.
// guestRoot must be an absolute guest path. Missing root returns empty map + nil
// (caller decides whether that is an error for source vs dest).
func InventoryGuest(ctx context.Context, gfs FS, guestRoot string, ign *syncIgnore) (map[string]*syncInvEntry, error) {
	guestRoot = path.Clean("/" + strings.TrimPrefix(filepath.ToSlash(guestRoot), "/"))
	out := make(map[string]*syncInvEntry)

	st, err := gfs.Stat(ctx, guestRoot)
	if err != nil {
		// Treat not-found as empty inventory (dest may be created).
		return out, nil
	}
	if st.Type != "directory" {
		return nil, fmt.Errorf("sync requires directory roots: guest %s is %s", guestRoot, st.Type)
	}

	type node struct {
		abs, rel string
	}
	queue := []node{{abs: guestRoot, rel: ""}}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n := queue[0]
		queue = queue[1:]
		entries, err := gfs.ReadDir(ctx, n.abs)
		if err != nil {
			return nil, fmt.Errorf("guest readdir %s: %w", n.abs, err)
		}
		for _, e := range entries {
			name := e.Name
			if name == "" || name == "." || name == ".." {
				continue
			}
			childRel := name
			if n.rel != "" {
				childRel = n.rel + "/" + name
			}
			childAbs := path.Join(n.abs, name)

			if e.Type == "directory" {
				if ign != nil && ign.MatchDir(childRel) {
					continue
				}
			} else if ign != nil && ign.Match(childRel) {
				continue
			}

			entry := &syncInvEntry{
				Type:  e.Type,
				Size:  e.Size,
				Mtime: e.Mtime,
				Mode:  e.Mode,
			}
			if entry.Type == "directory" {
				entry.Size = 0
			}
			if entry.Type == "" {
				entry.Type = "file"
			}
			out[childRel] = entry
			if e.Type == "directory" {
				queue = append(queue, node{abs: childAbs, rel: childRel})
			}
		}
	}
	return out, nil
}
