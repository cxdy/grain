package cli

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/guest"
	"github.com/spf13/cobra"
)

// cpSpec is one side of grain cp: either a host path or a guest NAME:path.
type cpSpec struct {
	// Guest is true when Spec is NAME:path (VM guest path).
	Guest bool
	// Name is the VM name when Guest is true.
	Name string
	// Path is the host filesystem path or guest path.
	Path string
	// Raw is the original argument.
	Raw string
}

// parseCPSpec parses "NAME:path" (guest) or a bare host path.
// Guest form: first ":" is the separator and the name part has no "/".
// Examples: "sbox-1:/tmp/x", "./file", "/home/a:b" (host — slash before colon).
func parseCPSpec(s string) cpSpec {
	s = strings.TrimSpace(s)
	out := cpSpec{Raw: s, Path: s}
	if i := strings.Index(s, ":"); i > 0 && !strings.Contains(s[:i], "/") {
		out.Guest = true
		out.Name = s[:i]
		out.Path = s[i+1:]
	}
	return out
}

func cmdCp(cfgPath *string) *cobra.Command {
	var forceSSH, forceAgent bool
	cmd := &cobra.Command{
		Use:   "cp [src] [dst]",
		Short: "Copy files (host path or NAME:path; prefers guest agent)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if forceSSH && forceAgent {
				return fmt.Errorf("cannot use --ssh and --agent together")
			}
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c := clientFrom(cfg)
			src := parseCPSpec(args[0])
			dst := parseCPSpec(args[1])

			// Prefer agent when one side is guest and agent is available.
			if !forceSSH && (src.Guest || dst.Guest) {
				err := cpViaAgent(c, src, dst, forceAgent)
				if err == nil {
					return nil
				}
				if forceAgent {
					return err
				}
				// Fall back to scp only when agent is missing/unhealthy.
				// Real copy errors and guest↔guest policy errors are returned as-is.
				if !isAgentUnavailable(err) {
					return err
				}
			}

			return cpViaSCP(cfg, c, src, dst)
		},
	}
	cmd.Flags().BoolVar(&forceSSH, "ssh", false, "force scp (skip guest agent)")
	cmd.Flags().BoolVar(&forceAgent, "agent", false, "force guest agent only (error if unavailable)")
	return cmd
}

func isAgentUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if err == errAgentSkip {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "agent not available") || strings.Contains(msg, "agent skip")
}

func cpViaAgent(c *api.Client, src, dst cpSpec, force bool) error {
	if src.Guest && dst.Guest {
		if src.Name == dst.Name {
			return fmt.Errorf("guest-to-guest copy on the same VM is not supported via agent; use a two-step host path or grain x")
		}
		return fmt.Errorf("guest-to-guest copy is not supported in one step; pull then push via a host temp path")
	}
	if !src.Guest && !dst.Guest {
		// Pure host copy — not an agent path.
		return errAgentSkip
	}

	var guestName string
	if src.Guest {
		guestName = src.Name
	} else {
		guestName = dst.Name
	}

	ac, err := dialGuestAgent(c, guestName, force)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if !src.Guest && dst.Guest {
		return agentPut(ctx, ac, src.Path, dst.Path)
	}
	// src.Guest && !dst.Guest
	return agentGet(ctx, ac, src.Path, dst.Path)
}

func agentPut(ctx context.Context, ac *agent.Client, hostPath, guestPath string) error {
	fi, err := os.Lstat(hostPath)
	if err != nil {
		return fmt.Errorf("src: %w", err)
	}
	// If guest path ends with /, place basename under it.
	if strings.HasSuffix(guestPath, "/") || guestPath == "" {
		guestPath = strings.TrimRight(guestPath, "/")
		if guestPath == "" {
			guestPath = "/"
		}
		guestPath = filepath.ToSlash(filepath.Join(guestPath, filepath.Base(hostPath)))
	}

	if fi.IsDir() {
		pr, pw := io.Pipe()
		go func() {
			err := writeLocalTar(pw, hostPath)
			_ = pw.CloseWithError(err)
		}()
		return ac.PutTar(ctx, guestPath, pr)
	}

	f, err := os.Open(hostPath)
	if err != nil {
		return err
	}
	defer f.Close()
	mode := fmt.Sprintf("%04o", fi.Mode().Perm())
	return ac.PutFile(ctx, guestPath, f, fi.Size(), agent.CPOpts{Mode: mode})
}

func agentGet(ctx context.Context, ac *agent.Client, guestPath, hostPath string) error {
	info, err := ac.Stat(ctx, guestPath)
	if err != nil {
		return err
	}

	// If host path is an existing directory (or ends with /), place under it.
	if strings.HasSuffix(hostPath, string(os.PathSeparator)) || strings.HasSuffix(hostPath, "/") {
		hostPath = filepath.Join(hostPath, info.Name)
	} else if st, err := os.Stat(hostPath); err == nil && st.IsDir() {
		hostPath = filepath.Join(hostPath, info.Name)
	}

	if info.Type == "directory" {
		if err := os.MkdirAll(hostPath, 0o755); err != nil {
			return err
		}
		pr, pw := io.Pipe()
		errCh := make(chan error, 1)
		go func() {
			err := ac.GetTar(ctx, guestPath, pw)
			_ = pw.CloseWithError(err)
			errCh <- err
		}()
		extErr := extractTar(pr, hostPath)
		getErr := <-errCh
		if getErr != nil {
			return getErr
		}
		return extErr
	}

	if err := os.MkdirAll(filepath.Dir(hostPath), 0o755); err != nil {
		return err
	}
	f, err := os.Create(hostPath)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := ac.GetFile(ctx, guestPath, f); err != nil {
		return err
	}
	if info.Mode != "" {
		var perm os.FileMode
		if _, err := fmt.Sscanf(info.Mode, "%o", &perm); err == nil && perm != 0 {
			_ = f.Chmod(perm)
		}
	}
	return nil
}

// writeLocalTar writes a tar of path's contents (if dir) or the single file to w.
// Directory entries are relative to the directory root (not including the dir name).
func writeLocalTar(w io.Writer, path string) error {
	tw := tar.NewWriter(w)
	defer tw.Close()

	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return addLocalToTar(tw, path, filepath.Base(path), info)
	}
	return filepath.Walk(path, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(path, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		li, err := os.Lstat(p)
		if err != nil {
			return err
		}
		return addLocalToTar(tw, p, rel, li)
	})
}

func addLocalToTar(tw *tar.Writer, path, name string, fi os.FileInfo) error {
	name = filepath.ToSlash(name)
	hdr, err := tar.FileInfoHeader(fi, "")
	if err != nil {
		return err
	}
	hdr.Name = name
	if fi.Mode()&os.ModeSymlink != 0 {
		link, err := os.Readlink(path)
		if err != nil {
			return err
		}
		hdr.Typeflag = tar.TypeSymlink
		hdr.Linkname = link
		hdr.Size = 0
	} else if fi.IsDir() {
		if !strings.HasSuffix(hdr.Name, "/") {
			hdr.Name += "/"
		}
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if fi.Mode().IsRegular() {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := io.Copy(tw, f); err != nil {
			return err
		}
	}
	return nil
}

func extractTar(r io.Reader, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target, err := safeHostTarPath(dest, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			perm := os.FileMode(hdr.Mode) & os.ModePerm
			if perm == 0 {
				perm = 0o755
			}
			if err := os.MkdirAll(target, perm); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			perm := os.FileMode(hdr.Mode) & os.ModePerm
			if perm == 0 {
				perm = 0o644
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		}
	}
	return nil
}

func safeHostTarPath(dest, name string) (string, error) {
	name = filepath.FromSlash(name)
	name = strings.TrimPrefix(name, string(filepath.Separator))
	if name == "" || name == "." {
		return dest, nil
	}
	clean := filepath.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("illegal tar path: %s", name)
	}
	target := filepath.Join(dest, clean)
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return "", err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	sep := string(filepath.Separator)
	if absTarget != absDest && !strings.HasPrefix(absTarget, absDest+sep) {
		return "", fmt.Errorf("illegal tar path: %s", name)
	}
	return target, nil
}

func cpViaSCP(cfg config.Config, c *api.Client, src, dst cpSpec) error {
	resolve := func(spec cpSpec) (scpArg string, port int, err error) {
		if !spec.Guest {
			return spec.Path, 0, nil
		}
		host, p, err := getVMSSH(c, spec.Name)
		if err != nil {
			return "", 0, err
		}
		return fmt.Sprintf("%s@%s:%s", cfg.SSHUser, host, spec.Path), p, nil
	}

	s, p1, err := resolve(src)
	if err != nil {
		return err
	}
	d, p2, err := resolve(dst)
	if err != nil {
		return err
	}
	port := p1
	if p2 > 0 {
		port = p2
	}
	id := grainSSHIdentity(cfg)
	if !fileExists(id) {
		id = ""
	}
	scpArgs := guest.SCPArgs(port, id)
	// Recursive when host source is a directory (scp needs -r).
	if !src.Guest {
		if fi, err := os.Stat(src.Path); err == nil && fi.IsDir() {
			scpArgs = append(scpArgs, "-r")
		}
	} else {
		// Guest source: allow recursive for directories.
		scpArgs = append(scpArgs, "-r")
	}
	scpArgs = append(scpArgs, s, d)
	scp := exec.Command("scp", scpArgs...)
	scp.Stdout = os.Stdout
	scp.Stderr = os.Stderr
	return scp.Run()
}
