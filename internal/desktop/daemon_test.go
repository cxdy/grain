package desktop

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type fakeRunner struct {
	path     string
	lookErr  error
	startErr error
	started  atomic.Int32
	lastName string
	lastArgs []string
}

func (f *fakeRunner) LookPath(file string) (string, error) {
	if f.lookErr != nil {
		return "", f.lookErr
	}
	if f.path != "" {
		return f.path, nil
	}
	return "/usr/bin/" + file, nil
}

func (f *fakeRunner) StartBackground(ctx context.Context, name string, args ...string) error {
	f.started.Add(1)
	f.lastName = name
	f.lastArgs = append([]string(nil), args...)
	return f.startErr
}

func TestShouldStartLocalDaemon(t *testing.T) {
	t.Parallel()
	local := Connection{Name: "local"}
	remote := Connection{Name: "lab", API: "http://x"}
	cases := []struct {
		conn    Connection
		healthy bool
		pref    bool
		want    bool
	}{
		{local, true, true, false},
		{local, false, true, true},
		{local, false, false, false},
		{remote, false, true, false},
		{remote, true, true, false},
	}
	for _, tc := range cases {
		got := ShouldStartLocalDaemon(tc.conn, tc.healthy, tc.pref)
		if got != tc.want {
			t.Fatalf("conn=%+v healthy=%v pref=%v got %v want %v", tc.conn, tc.healthy, tc.pref, got, tc.want)
		}
	}
}

func TestEnsureLocalDaemonAlreadyHealthy(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{}
	res, err := EnsureLocalDaemon(context.Background(), Connection{Name: "local"}, true,
		func(ctx context.Context) error { return nil }, r, func(time.Duration) {}, time.Second, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if !res.AlreadyHealthy || res.Started || r.started.Load() != 0 {
		t.Fatalf("%+v started=%d", res, r.started.Load())
	}
}

func TestEnsureLocalDaemonRemoteUnhealthy(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{}
	_, err := EnsureLocalDaemon(context.Background(), Connection{Name: "lab", API: "http://x"}, true,
		func(ctx context.Context) error { return errors.New("down") }, r, func(time.Duration) {}, time.Second, time.Millisecond)
	if err == nil {
		t.Fatal("want error")
	}
	if r.started.Load() != 0 {
		t.Fatal("must not start grain for remote")
	}
}

func TestEnsureLocalDaemonStartsAndWaits(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{path: "/bin/grain"}
	var n atomic.Int32
	res, err := EnsureLocalDaemon(context.Background(), Connection{Name: "local"}, true,
		func(ctx context.Context) error {
			if n.Add(1) < 3 {
				return errors.New("not yet")
			}
			return nil
		}, r, func(time.Duration) {}, 5*time.Second, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Started || res.GrainPath != "/bin/grain" {
		t.Fatalf("%+v", res)
	}
	if r.started.Load() != 1 || r.lastName != "/bin/grain" || len(r.lastArgs) != 1 || r.lastArgs[0] != "up" {
		t.Fatalf("runner: name=%q args=%v count=%d", r.lastName, r.lastArgs, r.started.Load())
	}
}

func TestEnsureLocalDaemonNoBinary(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{lookErr: errors.New("not found")}
	_, err := EnsureLocalDaemon(context.Background(), Connection{Name: "local"}, true,
		func(ctx context.Context) error { return errors.New("down") }, r, func(time.Duration) {}, time.Second, time.Millisecond)
	if err == nil {
		t.Fatal("want lookpath error")
	}
}

func TestEnsureLocalDaemonPrefOff(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{}
	_, err := EnsureLocalDaemon(context.Background(), Connection{Name: "local"}, false,
		func(ctx context.Context) error { return errors.New("down") }, r, func(time.Duration) {}, time.Second, time.Millisecond)
	if err == nil {
		t.Fatal("want error")
	}
	if r.started.Load() != 0 {
		t.Fatal("should not start")
	}
}

func TestEnsureLocalDaemonTimeout(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{path: "/bin/grain"}
	_, err := EnsureLocalDaemon(context.Background(), Connection{Name: "local"}, true,
		func(ctx context.Context) error { return errors.New("down") }, r, func(time.Duration) {}, 5*time.Millisecond, time.Millisecond)
	if err == nil {
		t.Fatal("want timeout")
	}
}

func TestEnsureLocalDaemonStartFails(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{path: "/bin/grain", startErr: errors.New("spawn")}
	_, err := EnsureLocalDaemon(context.Background(), Connection{Name: "local"}, true,
		func(ctx context.Context) error { return errors.New("down") }, r, func(time.Duration) {}, time.Second, time.Millisecond)
	if err == nil {
		t.Fatal("want start error")
	}
}

func TestEnsureLocalDaemonNilHealth(t *testing.T) {
	t.Parallel()
	_, err := EnsureLocalDaemon(context.Background(), Connection{Name: "local"}, true, nil, &fakeRunner{}, nil, 0, 0)
	if err == nil {
		t.Fatal("want error")
	}
}

func TestExecRunnerStartBackground(t *testing.T) {
	// Start a short-lived process via the production runner.
	var r ExecRunner
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// `true` exits immediately; StartBackground still succeeds.
	if err := r.StartBackground(ctx, "true"); err != nil {
		// some systems use /usr/bin/true
		path, lerr := r.LookPath("true")
		if lerr != nil {
			t.Skip("no true binary")
		}
		if err := r.StartBackground(ctx, path); err != nil {
			t.Fatal(err)
		}
	}
}
