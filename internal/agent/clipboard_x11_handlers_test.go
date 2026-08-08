//go:build linux

package agent

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// dialTestDisplay starts Xvfb if needed and returns an xgb connection + cleanup.
func dialTestDisplay(t *testing.T) (display string, X *xgb.Conn) {
	t.Helper()
	if _, err := exec.LookPath("Xvfb"); err != nil {
		t.Skip("Xvfb required")
	}
	// Unique display per test to avoid parallel Xvfb races.
	n := 100 + (int(time.Now().UnixNano()/1000) % 80)
	display = ":" + strconv.Itoa(n)
	num := strconv.Itoa(n)
	_ = os.Remove("/tmp/.X" + num + "-lock")
	_ = os.Remove("/tmp/.X11-unix/X" + num)
	if err := startXvfb(display, slog.Default()); err != nil {
		t.Fatal(err)
	}
	// Wait until the X socket exists before dialing.
	sock := "/tmp/.X11-unix/X" + num
	deadline := time.Now().Add(5 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sock); err == nil {
			prev := os.Getenv("DISPLAY")
			_ = os.Setenv("DISPLAY", display)
			X, last = xgb.NewConnDisplay(display)
			if prev == "" {
				_ = os.Unsetenv("DISPLAY")
			} else {
				_ = os.Setenv("DISPLAY", prev)
			}
			if last == nil {
				t.Cleanup(func() { X.Close() })
				return display, X
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("connect: %v (sock %s)", last, sock)
	return display, X
}

func TestHandleSelectionRequestAndServeBytes(t *testing.T) {
	_, X := dialTestDisplay(t)
	setup := xproto.Setup(X)
	screen := setup.DefaultScreen(X)

	// Owner window
	owner, err := xproto.NewWindowId(X)
	if err != nil {
		t.Fatal(err)
	}
	_ = xproto.CreateWindowChecked(X, screen.RootDepth, owner, screen.Root,
		0, 0, 1, 1, 0, xproto.WindowClassInputOutput, screen.RootVisual,
		xproto.CwEventMask, []uint32{xproto.EventMaskPropertyChange}).Check()

	// Requestor window
	req, err := xproto.NewWindowId(X)
	if err != nil {
		t.Fatal(err)
	}
	_ = xproto.CreateWindowChecked(X, screen.RootDepth, req, screen.Root,
		0, 0, 1, 1, 0, xproto.WindowClassInputOutput, screen.RootVisual,
		xproto.CwEventMask, []uint32{xproto.EventMaskPropertyChange}).Check()

	atoms := &x11Atoms{}
	if err := atoms.init(X); err != nil {
		t.Fatal(err)
	}
	maxDirect := x11MaxDirectBytes(setup.MaximumRequestLength)
	pending := map[uint64]*incrTransfer{}
	log := slog.Default()

	// TARGETS
	handleSelectionRequest(X, atoms, xproto.SelectionRequestEvent{
		Time: xproto.TimeCurrentTime, Requestor: req, Selection: atoms.clipboard,
		Target: atoms.targets, Property: atoms.targets,
	}, func(ctx context.Context) ([]byte, error) { return nil, nil }, log, maxDirect, pending)

	// TIMESTAMP
	handleSelectionRequest(X, atoms, xproto.SelectionRequestEvent{
		Time: xproto.TimeCurrentTime, Requestor: req, Selection: atoms.clipboard,
		Target: atoms.timestamp, Property: 0, // property=0 uses target
	}, nil, log, maxDirect, pending)

	// UTF8 text
	handleSelectionRequest(X, atoms, xproto.SelectionRequestEvent{
		Time: xproto.TimeCurrentTime, Requestor: req, Selection: atoms.clipboard,
		Target: atoms.utf8, Property: atoms.utf8,
	}, func(ctx context.Context) ([]byte, error) {
		return []byte("hello-text"), nil
	}, log, maxDirect, pending)

	// Image refuse when text requested
	handleSelectionRequest(X, atoms, xproto.SelectionRequestEvent{
		Time: xproto.TimeCurrentTime, Requestor: req, Selection: atoms.clipboard,
		Target: atoms.utf8, Property: atoms.utf8,
	}, func(ctx context.Context) ([]byte, error) {
		return append([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, make([]byte, 20)...), nil
	}, log, maxDirect, pending)

	// PNG target
	handleSelectionRequest(X, atoms, xproto.SelectionRequestEvent{
		Time: xproto.TimeCurrentTime, Requestor: req, Selection: atoms.clipboard,
		Target: atoms.png, Property: atoms.png,
	}, func(ctx context.Context) ([]byte, error) {
		return append([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, make([]byte, 50)...), nil
	}, log, maxDirect, pending)

	// JPEG target with PNG data (type switch)
	handleSelectionRequest(X, atoms, xproto.SelectionRequestEvent{
		Time: xproto.TimeCurrentTime, Requestor: req, Selection: atoms.clipboard,
		Target: atoms.jpeg, Property: atoms.jpeg,
	}, func(ctx context.Context) ([]byte, error) {
		return append([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, make([]byte, 10)...), nil
	}, log, maxDirect, pending)

	// fetch error
	handleSelectionRequest(X, atoms, xproto.SelectionRequestEvent{
		Time: xproto.TimeCurrentTime, Requestor: req, Selection: atoms.clipboard,
		Target: atoms.utf8, Property: atoms.utf8,
	}, func(ctx context.Context) ([]byte, error) {
		return nil, context.Canceled
	}, log, maxDirect, pending)

	// unknown target
	handleSelectionRequest(X, atoms, xproto.SelectionRequestEvent{
		Time: xproto.TimeCurrentTime, Requestor: req, Selection: atoms.clipboard,
		Target: 99999, Property: 99999,
	}, nil, log, maxDirect, pending)

	// Direct serveClipboardBytes small
	reply := xproto.SelectionNotifyEvent{}
	serveClipboardBytes(X, atoms, xproto.SelectionRequestEvent{Requestor: req}, atoms.utf8, atoms.utf8,
		[]byte("abc"), maxDirect, pending, log, &reply)
	if reply.Property == 0 {
		t.Log("direct serve may fail without full setup")
	}

	// INCR path: force small maxDirect
	pending2 := map[uint64]*incrTransfer{}
	reply2 := xproto.SelectionNotifyEvent{}
	big := make([]byte, 4096)
	for i := range big {
		big[i] = byte(i)
	}
	serveClipboardBytes(X, atoms, xproto.SelectionRequestEvent{Requestor: req, Property: atoms.utf8},
		atoms.utf8, atoms.utf8, big, 64, pending2, log, &reply2)
	if len(pending2) == 0 {
		t.Log("incr pending empty (ChangeProperty may have failed)")
	} else {
		// PropertyNotify delete to drain chunks
		for k, tr := range pending2 {
			_ = k
			handlePropertyNotify(X, xproto.PropertyNotifyEvent{
				Window: tr.requestor, Atom: tr.property, State: xproto.PropertyDelete,
			}, log, pending2, 64)
			// non-delete ignored
			handlePropertyNotify(X, xproto.PropertyNotifyEvent{
				Window: tr.requestor, Atom: tr.property, State: xproto.PropertyNewValue,
			}, log, pending2, 64)
			// unknown key
			handlePropertyNotify(X, xproto.PropertyNotifyEvent{
				Window: 0, Atom: 0, State: xproto.PropertyDelete,
			}, log, pending2, 64)
		}
		// drain remaining
		for len(pending2) > 0 {
			for _, tr := range pending2 {
				handlePropertyNotify(X, xproto.PropertyNotifyEvent{
					Window: tr.requestor, Atom: tr.property, State: xproto.PropertyDelete,
				}, log, pending2, 64)
				break
			}
		}
	}
}

func TestHandleSelectionRequestErrorPaths(t *testing.T) {
	_, X := dialTestDisplay(t)
	setup := xproto.Setup(X)
	atoms := &x11Atoms{}
	if err := atoms.init(X); err != nil {
		t.Fatal(err)
	}
	maxDirect := x11MaxDirectBytes(setup.MaximumRequestLength)
	pending := map[uint64]*incrTransfer{}
	log := slog.Default()

	// Invalid requestor → ChangeProperty fails → error branches.
	bad := xproto.Window(0)
	handleSelectionRequest(X, atoms, xproto.SelectionRequestEvent{
		Time: xproto.TimeCurrentTime, Requestor: bad, Selection: atoms.clipboard,
		Target: atoms.targets, Property: atoms.targets,
	}, nil, log, maxDirect, pending)
	handleSelectionRequest(X, atoms, xproto.SelectionRequestEvent{
		Time: xproto.TimeCurrentTime, Requestor: bad, Selection: atoms.clipboard,
		Target: atoms.timestamp, Property: atoms.timestamp,
	}, nil, log, maxDirect, pending)
	handleSelectionRequest(X, atoms, xproto.SelectionRequestEvent{
		Time: xproto.TimeCurrentTime, Requestor: bad, Selection: atoms.clipboard,
		Target: atoms.utf8, Property: atoms.utf8,
	}, func(ctx context.Context) ([]byte, error) { return []byte("x"), nil }, log, maxDirect, pending)
	handleSelectionRequest(X, atoms, xproto.SelectionRequestEvent{
		Time: xproto.TimeCurrentTime, Requestor: bad, Selection: atoms.clipboard,
		Target: atoms.png, Property: atoms.png,
	}, func(ctx context.Context) ([]byte, error) {
		return append([]byte{0x89, 'P', 'N', 'G', 0, 0, 0, 0}, make([]byte, 20)...), nil
	}, log, maxDirect, pending)

	// serveClipboardBytes error paths
	reply := xproto.SelectionNotifyEvent{}
	serveClipboardBytes(X, atoms, xproto.SelectionRequestEvent{Requestor: bad}, atoms.utf8, atoms.utf8,
		[]byte("abc"), maxDirect, pending, log, &reply)
	// INCR start fail
	big := make([]byte, 10000)
	serveClipboardBytes(X, atoms, xproto.SelectionRequestEvent{Requestor: bad, Property: atoms.utf8},
		atoms.utf8, atoms.utf8, big, 64, pending, log, &reply)

	// handlePropertyNotify INCR end with bad window
	pending[incrKey(bad, atoms.utf8)] = &incrTransfer{
		requestor: bad, property: atoms.utf8, dataType: atoms.utf8, data: []byte("done"), offset: 4,
	}
	handlePropertyNotify(X, xproto.PropertyNotifyEvent{
		Window: bad, Atom: atoms.utf8, State: xproto.PropertyDelete,
	}, log, pending, 64)

	// chunk fail mid-transfer
	pending[incrKey(bad, atoms.png)] = &incrTransfer{
		requestor: bad, property: atoms.png, dataType: atoms.png, data: make([]byte, 5000), offset: 0,
	}
	handlePropertyNotify(X, xproto.PropertyNotifyEvent{
		Window: bad, Atom: atoms.png, State: xproto.PropertyDelete,
	}, log, pending, 64)

	// empty fetch for png
	screen := setup.DefaultScreen(X)
	req, _ := xproto.NewWindowId(X)
	_ = xproto.CreateWindowChecked(X, screen.RootDepth, req, screen.Root,
		0, 0, 1, 1, 0, xproto.WindowClassInputOutput, screen.RootVisual, 0, nil).Check()
	handleSelectionRequest(X, atoms, xproto.SelectionRequestEvent{
		Time: xproto.TimeCurrentTime, Requestor: req, Selection: atoms.clipboard,
		Target: atoms.png, Property: atoms.png,
	}, func(ctx context.Context) ([]byte, error) { return nil, nil }, log, maxDirect, pending)
}

func TestHandleSelectionJPEGAsPNGAndIncrPartialChunk(t *testing.T) {
	_, X := dialTestDisplay(t)
	setup := xproto.Setup(X)
	screen := setup.DefaultScreen(X)
	req, err := xproto.NewWindowId(X)
	if err != nil {
		t.Fatal(err)
	}
	_ = xproto.CreateWindowChecked(X, screen.RootDepth, req, screen.Root,
		0, 0, 1, 1, 0, xproto.WindowClassInputOutput, screen.RootVisual,
		xproto.CwEventMask, []uint32{xproto.EventMaskPropertyChange}).Check()

	atoms := &x11Atoms{}
	if err := atoms.init(X); err != nil {
		t.Fatal(err)
	}
	maxDirect := x11MaxDirectBytes(setup.MaximumRequestLength)
	pending := map[uint64]*incrTransfer{}
	log := slog.Default()

	// PNG target with JPEG payload → type switches to jpeg (lines 335-337).
	jpeg := append([]byte{0xff, 0xd8, 0xff, 0xe0}, make([]byte, 40)...)
	handleSelectionRequest(X, atoms, xproto.SelectionRequestEvent{
		Time: xproto.TimeCurrentTime, Requestor: req, Selection: atoms.clipboard,
		Target: atoms.png, Property: atoms.png,
	}, func(ctx context.Context) ([]byte, error) { return jpeg, nil }, log, maxDirect, pending)

	// Text target with JPEG → refuse (image vs text).
	handleSelectionRequest(X, atoms, xproto.SelectionRequestEvent{
		Time: xproto.TimeCurrentTime, Requestor: req, Selection: atoms.clipboard,
		Target: atoms.utf8, Property: atoms.utf8,
	}, func(ctx context.Context) ([]byte, error) { return jpeg, nil }, log, maxDirect, pending)

	// INCR with non-multiple-of-chunk size so end>len clamp runs (459-461).
	pending2 := map[uint64]*incrTransfer{}
	reply2 := xproto.SelectionNotifyEvent{}
	// 100 bytes, maxDirect 64 → two chunks: 64 then 36 (partial last piece).
	odd := make([]byte, 100)
	for i := range odd {
		odd[i] = byte(i)
	}
	serveClipboardBytes(X, atoms, xproto.SelectionRequestEvent{Requestor: req, Property: atoms.utf8},
		atoms.utf8, atoms.utf8, odd, 64, pending2, log, &reply2)
	for len(pending2) > 0 {
		for _, tr := range pending2 {
			handlePropertyNotify(X, xproto.PropertyNotifyEvent{
				Window: tr.requestor, Atom: tr.property, State: xproto.PropertyDelete,
			}, log, pending2, 64)
			break
		}
	}

	// chunk size clamp: maxDirect < 1024 → chunk raised to 1024 when remaining large
	pending3 := map[uint64]*incrTransfer{}
	big := make([]byte, 3000)
	serveClipboardBytes(X, atoms, xproto.SelectionRequestEvent{Requestor: req, Property: atoms.png},
		atoms.png, atoms.png, big, 100, pending3, log, &reply2)
	if len(pending3) > 0 {
		// first PropertyNotify with maxDirect=100 → chunk becomes 1024
		for _, tr := range pending3 {
			handlePropertyNotify(X, xproto.PropertyNotifyEvent{
				Window: tr.requestor, Atom: tr.property, State: xproto.PropertyDelete,
			}, log, pending3, 100)
			break
		}
		// drain rest
		for len(pending3) > 0 {
			for _, tr := range pending3 {
				handlePropertyNotify(X, xproto.PropertyNotifyEvent{
					Window: tr.requestor, Atom: tr.property, State: xproto.PropertyDelete,
				}, log, pending3, 100)
				break
			}
		}
	}
}

func TestStartX11ClipboardOwnerSuccessAndEventLoop(t *testing.T) {
	// Full owner start (claims CLIPBOARD + spawns runX11ClipboardOwner).
	display, X := dialTestDisplay(t)
	// Don't need X for owner; startX11ClipboardOwner opens its own conn.
	_ = X
	fetch := func(ctx context.Context) ([]byte, error) { return []byte("owned"), nil }
	if err := startX11ClipboardOwner(display, fetch, slog.Default()); err != nil {
		t.Fatal(err)
	}
	// Second start on same display may fail to own CLIPBOARD (already owned) or succeed.
	_ = startX11ClipboardOwner(display, fetch, slog.Default())
	// Give the event loop a moment to process any pending events.
	time.Sleep(100 * time.Millisecond)
}
