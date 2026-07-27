package config_test

import (
	"testing"

	"github.com/cxdy/grain/internal/config"
)

func TestNormalizeAPIURL(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":                          "",
		"  ":                        "",
		"http://127.0.0.1:7474":     "http://127.0.0.1:7474",
		"http://127.0.0.1:7474/":    "http://127.0.0.1:7474",
		"https://grain.example/":    "https://grain.example",
		"127.0.0.1:7474":            "http://127.0.0.1:7474",
		"sandbox.internal:7474":     "http://sandbox.internal:7474",
	}
	for in, want := range cases {
		if got := config.NormalizeAPIURL(in); got != want {
			t.Errorf("NormalizeAPIURL(%q)=%q want %q", in, got, want)
		}
	}
}

func TestAPIURLIsLoopback(t *testing.T) {
	t.Parallel()
	loop := []string{"", "http://127.0.0.1:7474", "http://localhost:7474", "http://[::1]:7474", "http://grain"}
	for _, u := range loop {
		if !config.APIURLIsLoopback(u) {
			t.Errorf("expected loopback: %q", u)
		}
	}
	remote := []string{"http://10.0.0.5:7474", "https://grain.example", "http://sandbox.internal:7474"}
	for _, u := range remote {
		if config.APIURLIsLoopback(u) {
			t.Errorf("expected non-loopback: %q", u)
		}
	}
}

func TestListenAddrIsLoopback(t *testing.T) {
	t.Parallel()
	if !config.ListenAddrIsLoopback("127.0.0.1:7474") {
		t.Fatal("127.0.0.1 should be loopback")
	}
	if !config.ListenAddrIsLoopback("[::1]:7474") {
		t.Fatal("::1 should be loopback")
	}
	if config.ListenAddrIsLoopback("0.0.0.0:7474") {
		t.Fatal("0.0.0.0 is not loopback")
	}
	if config.ListenAddrIsLoopback(":7474") {
		t.Fatal(":port binds all interfaces")
	}
	if config.ListenAddrIsLoopback("192.168.1.10:7474") {
		t.Fatal("LAN IP is not loopback")
	}
}

func TestIsRemoteClient(t *testing.T) {
	t.Parallel()
	c := config.Config{}
	if c.IsRemoteClient() {
		t.Fatal("empty should be local")
	}
	c.APIURL = "http://10.0.0.1:7474"
	if !c.IsRemoteClient() {
		t.Fatal("api_url should be remote")
	}
}
