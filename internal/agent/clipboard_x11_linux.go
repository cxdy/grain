//go:build linux

package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// Default virtual display for headless guests so TUIs using native X11
// clipboard (arboard, etc.) can paste host images via the grain bridge.
const grainClipboardDisplay = ":7"

var (
	x11ClipOnce sync.Once
	x11ClipDisp string // set when X11 clipboard path is live
)

// clipboardDisplayEnv returns DISPLAY=… for shell sessions when the X11
// clipboard owner is running (empty if unavailable).
func clipboardDisplayEnv() string {
	x11ClipOnce.Do(func() {}) // ensure started only via ensureClipboardX11
	if x11ClipDisp == "" {
		return ""
	}
	return "DISPLAY=" + x11ClipDisp
}

// ensureClipboardX11 best-effort starts Xvfb + an X11 CLIPBOARD owner that
// serves GET /clipboard (host paste) on SelectionRequest. Safe to call often.
func ensureClipboardX11(log *slog.Logger, fetch func(context.Context) ([]byte, error)) {
	x11ClipOnce.Do(func() {
		if log == nil {
			log = slog.Default()
		}
		// Respect an existing display (user-provided desktop).
		if d := strings.TrimSpace(os.Getenv("DISPLAY")); d != "" && d != grainClipboardDisplay {
			log.Debug("clipboard x11: using existing DISPLAY", "display", d)
			// Still try to own CLIPBOARD on that display.
			if err := startX11ClipboardOwner(d, fetch, log); err != nil {
				log.Debug("clipboard x11 owner failed", "display", d, "err", err)
				return
			}
			x11ClipDisp = d
			return
		}
		if _, err := exec.LookPath("Xvfb"); err != nil {
			log.Debug("clipboard x11: Xvfb not installed; native X11 paste unavailable (CLI pbpaste/xclip shims still work)")
			return
		}
		if err := startXvfb(grainClipboardDisplay, log); err != nil {
			log.Debug("clipboard x11: Xvfb start failed", "err", err)
			return
		}
		// Xvfb needs a moment to accept connections.
		deadline := time.Now().Add(3 * time.Second)
		var last error
		for time.Now().Before(deadline) {
			if err := startX11ClipboardOwner(grainClipboardDisplay, fetch, log); err == nil {
				x11ClipDisp = grainClipboardDisplay
				log.Info("clipboard x11 ready", "display", grainClipboardDisplay)
				return
			} else {
				last = err
			}
			time.Sleep(100 * time.Millisecond)
		}
		log.Debug("clipboard x11 owner failed", "err", last)
	})
}

func startXvfb(display string, log *slog.Logger) error {
	// Already running?
	if xDisplayAlive(display) {
		return nil
	}
	// display ":7" → lock file /tmp/.X7-lock
	num := strings.TrimPrefix(display, ":")
	logDir := "/var/lib/grain"
	_ = os.MkdirAll(logDir, 0o755)
	logPath := filepath.Join(logDir, "xvfb"+num+".log")
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		logf = nil
	}
	cmd := exec.Command("Xvfb", display, "-screen", "0", "1280x720x24", "-ac", "+extension", "GLX", "-nolisten", "tcp")
	if logf != nil {
		cmd.Stdout = logf
		cmd.Stderr = logf
	}
	// Detach from agent lifecycle somewhat: agent death should kill Xvfb via process group.
	cmd.SysProcAttr = x11SysProcAttr()
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	log.Debug("xvfb started", "display", display, "pid", cmd.Process.Pid)
	return nil
}

func xDisplayAlive(display string) bool {
	// Xvfb creates /tmp/.X<n>-lock
	num := strings.TrimPrefix(display, ":")
	if _, err := os.Stat("/tmp/.X" + num + "-lock"); err == nil {
		// Also try a quick connect.
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		cmd := exec.CommandContext(ctx, "xdpyinfo", "-display", display)
		return cmd.Run() == nil || fileExists("/tmp/.X11-unix/X"+num)
	}
	return fileExists("/tmp/.X11-unix/X" + num)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// startX11ClipboardOwner claims CLIPBOARD and serves host paste data.
func startX11ClipboardOwner(display string, fetch func(context.Context) ([]byte, error), log *slog.Logger) error {
	// XGB uses DISPLAY env.
	prev := os.Getenv("DISPLAY")
	_ = os.Setenv("DISPLAY", display)
	defer func() {
		if prev == "" {
			_ = os.Unsetenv("DISPLAY")
		} else {
			_ = os.Setenv("DISPLAY", prev)
		}
	}()

	X, err := xgb.NewConnDisplay(display)
	if err != nil {
		return fmt.Errorf("x11 connect %s: %w", display, err)
	}

	setup := xproto.Setup(X)
	screen := setup.DefaultScreen(X)
	wid, err := xproto.NewWindowId(X)
	if err != nil {
		X.Close()
		return err
	}
	_ = xproto.CreateWindowChecked(X, screen.RootDepth, wid, screen.Root,
		0, 0, 1, 1, 0, xproto.WindowClassInputOutput, screen.RootVisual,
		xproto.CwEventMask, []uint32{xproto.EventMaskPropertyChange}).Check()

	atoms := &x11Atoms{}
	if err := atoms.init(X); err != nil {
		X.Close()
		return err
	}

	// Claim CLIPBOARD.
	_ = xproto.SetSelectionOwnerChecked(X, wid, atoms.clipboard, xproto.TimeCurrentTime).Check()
	owner, err := xproto.GetSelectionOwner(X, atoms.clipboard).Reply()
	if err != nil || owner.Owner != wid {
		X.Close()
		return fmt.Errorf("failed to own CLIPBOARD (owner=%v err=%v)", owner, err)
	}

	maxDirect := x11MaxDirectBytes(setup.MaximumRequestLength)
	go runX11ClipboardOwner(X, wid, atoms, fetch, log, maxDirect)
	return nil
}

type x11Atoms struct {
	clipboard xproto.Atom
	targets   xproto.Atom
	multiple  xproto.Atom
	timestamp xproto.Atom
	utf8      xproto.Atom
	text      xproto.Atom
	png       xproto.Atom
	jpeg      xproto.Atom
	incr      xproto.Atom
	atomPair  xproto.Atom
}

func (a *x11Atoms) init(X *xgb.Conn) error {
	var err error
	intern := func(name string) (xproto.Atom, error) {
		r, e := xproto.InternAtom(X, false, uint16(len(name)), name).Reply()
		if e != nil {
			return 0, e
		}
		return r.Atom, nil
	}
	if a.clipboard, err = intern("CLIPBOARD"); err != nil {
		return err
	}
	if a.targets, err = intern("TARGETS"); err != nil {
		return err
	}
	if a.multiple, err = intern("MULTIPLE"); err != nil {
		return err
	}
	if a.timestamp, err = intern("TIMESTAMP"); err != nil {
		return err
	}
	if a.utf8, err = intern("UTF8_STRING"); err != nil {
		return err
	}
	if a.text, err = intern("TEXT"); err != nil {
		return err
	}
	if a.png, err = intern("image/png"); err != nil {
		return err
	}
	if a.jpeg, err = intern("image/jpeg"); err != nil {
		return err
	}
	if a.incr, err = intern("INCR"); err != nil {
		return err
	}
	if a.atomPair, err = intern("ATOM_PAIR"); err != nil {
		return err
	}
	return nil
}

// incrTransfer tracks an in-flight ICCCM INCR selection transfer.
type incrTransfer struct {
	requestor xproto.Window
	property  xproto.Atom
	dataType  xproto.Atom
	data      []byte
	offset    int
}

func runX11ClipboardOwner(X *xgb.Conn, wid xproto.Window, atoms *x11Atoms, fetch func(context.Context) ([]byte, error), log *slog.Logger, maxDirect int) {
	defer X.Close()
	// Keyed by requestor<<32 | property so concurrent pastes can proceed.
	pending := make(map[uint64]*incrTransfer)

	for {
		ev, err := X.WaitForEvent()
		if err != nil {
			log.Debug("clipboard x11 event loop end", "err", err)
			return
		}
		if ev == nil {
			continue
		}
		switch e := ev.(type) {
		case xproto.SelectionRequestEvent:
			handleSelectionRequest(X, wid, atoms, e, fetch, log, maxDirect, pending)
		case xproto.PropertyNotifyEvent:
			handlePropertyNotify(X, atoms, e, log, pending, maxDirect)
		case xproto.SelectionClearEvent:
			// Re-claim if something else took CLIPBOARD.
			_ = xproto.SetSelectionOwnerChecked(X, wid, atoms.clipboard, xproto.TimeCurrentTime).Check()
		}
	}
}

func incrKey(requestor xproto.Window, property xproto.Atom) uint64 {
	return uint64(requestor)<<32 | uint64(property)
}

func handleSelectionRequest(
	X *xgb.Conn,
	wid xproto.Window,
	atoms *x11Atoms,
	e xproto.SelectionRequestEvent,
	fetch func(context.Context) ([]byte, error),
	log *slog.Logger,
	maxDirect int,
	pending map[uint64]*incrTransfer,
) {
	reply := xproto.SelectionNotifyEvent{
		Time:      e.Time,
		Requestor: e.Requestor,
		Selection: e.Selection,
		Target:    e.Target,
		Property:  0, // None = refused
	}
	prop := e.Property
	if prop == 0 {
		prop = e.Target
	}

	switch e.Target {
	case atoms.targets:
		// Advertise image + text targets + INCR.
		targets := []uint32{
			uint32(atoms.targets),
			uint32(atoms.incr),
			uint32(atoms.png),
			uint32(atoms.jpeg),
			uint32(atoms.utf8),
			uint32(atoms.text),
			uint32(xproto.AtomString),
			uint32(atoms.timestamp),
		}
		data := make([]byte, len(targets)*4)
		for i, t := range targets {
			xgb.Put32(data[i*4:], t)
		}
		if err := xproto.ChangePropertyChecked(X, xproto.PropModeReplace, e.Requestor, prop,
			xproto.AtomAtom, 32, uint32(len(targets)), data).Check(); err != nil {
			log.Debug("clipboard x11 TARGETS ChangeProperty failed", "err", err)
			break
		}
		reply.Property = prop

	case atoms.timestamp:
		buf := make([]byte, 4)
		xgb.Put32(buf, uint32(xproto.TimeCurrentTime))
		if err := xproto.ChangePropertyChecked(X, xproto.PropModeReplace, e.Requestor, prop,
			xproto.AtomInteger, 32, 1, buf).Check(); err != nil {
			log.Debug("clipboard x11 TIMESTAMP ChangeProperty failed", "err", err)
			break
		}
		reply.Property = prop

	case atoms.png, atoms.jpeg, atoms.utf8, atoms.text, xproto.AtomString:
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		raw, err := fetch(ctx)
		cancel()
		if err != nil || len(raw) == 0 {
			log.Debug("clipboard x11 fetch failed", "err", err, "len", len(raw))
			break
		}
		typ := e.Target
		refuse := false
		switch {
		case e.Target == atoms.png || e.Target == atoms.jpeg:
			if e.Target == atoms.png && isJPEG(raw) {
				typ = atoms.jpeg
			}
			if e.Target == atoms.jpeg && isPNG(raw) {
				typ = atoms.png
			}
		case e.Target == atoms.utf8 || e.Target == atoms.text || e.Target == xproto.AtomString:
			if isPNG(raw) || isJPEG(raw) {
				// Client asked for text but clipboard is image — refuse so
				// they retry with image targets (arboard does this).
				refuse = true
			}
		}
		if refuse {
			break
		}

		if len(raw) <= maxDirect {
			if err := xproto.ChangePropertyChecked(X, xproto.PropModeReplace, e.Requestor, prop,
				typ, 8, uint32(len(raw)), raw).Check(); err != nil {
				log.Debug("clipboard x11 direct ChangeProperty failed", "err", err, "bytes", len(raw))
				break
			}
			reply.Property = prop
			break
		}

		// ICCCM INCR: property type INCR, format 32, value = total size.
		// Requestor deletes property; we send chunks on PropertyNotify Delete.
		sizeBuf := make([]byte, 4)
		xgb.Put32(sizeBuf, uint32(len(raw)))
		if err := xproto.ChangePropertyChecked(X, xproto.PropModeReplace, e.Requestor, prop,
			atoms.incr, 32, 1, sizeBuf).Check(); err != nil {
			log.Debug("clipboard x11 INCR start ChangeProperty failed", "err", err, "bytes", len(raw))
			break
		}
		// Need PropertyNotify when requestor deletes the property.
		_ = xproto.ChangeWindowAttributesChecked(X, e.Requestor, xproto.CwEventMask,
			[]uint32{xproto.EventMaskPropertyChange}).Check()

		pending[incrKey(e.Requestor, prop)] = &incrTransfer{
			requestor: e.Requestor,
			property:  prop,
			dataType:  typ,
			data:      raw,
			offset:    0,
		}
		log.Debug("clipboard x11 INCR start", "bytes", len(raw), "max_direct", maxDirect)
		reply.Property = prop

	default:
		// Unknown target — refuse.
	}

	_ = xproto.SendEventChecked(X, false, e.Requestor, xproto.EventMaskNoEvent,
		string(xproto.SelectionNotifyEvent{
			Time:      reply.Time,
			Requestor: reply.Requestor,
			Selection: reply.Selection,
			Target:    reply.Target,
			Property:  reply.Property,
		}.Bytes())).Check()
}

func handlePropertyNotify(
	X *xgb.Conn,
	atoms *x11Atoms,
	e xproto.PropertyNotifyEvent,
	log *slog.Logger,
	pending map[uint64]*incrTransfer,
	maxDirect int,
) {
	// Only act on property deletes (requestor finished reading previous chunk).
	if e.State != xproto.PropertyDelete {
		return
	}
	key := incrKey(e.Window, e.Atom)
	tr, ok := pending[key]
	if !ok {
		return
	}

	chunk := x11IncrChunkSize
	if chunk > maxDirect {
		chunk = maxDirect
	}
	if chunk < 1024 {
		chunk = 1024
	}

	remaining := len(tr.data) - tr.offset
	if remaining <= 0 {
		// Zero-length property signals end of INCR transfer.
		if err := xproto.ChangePropertyChecked(X, xproto.PropModeReplace, tr.requestor, tr.property,
			tr.dataType, 8, 0, []byte{}).Check(); err != nil {
			log.Debug("clipboard x11 INCR end ChangeProperty failed", "err", err)
		}
		delete(pending, key)
		log.Debug("clipboard x11 INCR complete", "bytes", len(tr.data))
		return
	}

	end := tr.offset + chunk
	if end > len(tr.data) {
		end = len(tr.data)
	}
	piece := tr.data[tr.offset:end]
	if err := xproto.ChangePropertyChecked(X, xproto.PropModeReplace, tr.requestor, tr.property,
		tr.dataType, 8, uint32(len(piece)), piece).Check(); err != nil {
		log.Debug("clipboard x11 INCR chunk ChangeProperty failed", "err", err, "off", tr.offset, "n", len(piece))
		delete(pending, key)
		return
	}
	tr.offset = end
}

// parseDisplayNum returns the integer display number for logging.
func parseDisplayNum(display string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(display, ":"))
	return n
}
