package api

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// nonLoopbackIPv4 returns an IPv4 address of this machine that is not
// loopback, or "" when there is none (for example inside a private network
// namespace).
func nonLoopbackIPv4(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatalf("InterfaceAddrs: %v", err)
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.IsLoopback() || ipn.IP.To4() == nil {
			continue
		}
		return ipn.IP.String()
	}
	return ""
}

func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return port
}

// SetHost("127.0.0.1") must bind loopback only. Before it existed the server
// built ":<port>", the dual-stack wildcard, so every interface answered.
func TestServer_SetHostBindsOnlyThatAddress(t *testing.T) {
	other := nonLoopbackIPv4(t)
	if other == "" {
		t.Skip("no non-loopback IPv4 address on this machine to prove the server is not reachable on")
	}

	dir := t.TempDir()
	gs, err := storage.NewGraphStorage(dir)
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })

	port := freeLoopbackPort(t)
	server, err := NewServerWithDataDir(gs, port, dir)
	if err != nil {
		t.Fatalf("NewServerWithDataDir: %v", err)
	}
	server.SetHost("127.0.0.1")

	errCh := make(chan error, 1)
	go func() { errCh <- server.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		if err := <-errCh; err != nil && err != http.ErrServerClosed {
			t.Errorf("Start returned %v", err)
		}
	})

	loopback := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(5 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", loopback, 500*time.Millisecond)
		if err == nil {
			_ = c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("positive control: the server never answered on %s: %v", loopback, err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	elsewhere := net.JoinHostPort(other, strconv.Itoa(port))
	if c, err := net.DialTimeout("tcp", elsewhere, 2*time.Second); err == nil {
		_ = c.Close()
		t.Fatalf("the server answered on %s; with SetHost(\"127.0.0.1\") it must answer on loopback only", elsewhere)
	}
}

// The guard: without SetHost the bind stays on every interface, so a
// deployment that relied on the default keeps answering on the network.
func TestServer_DefaultHostStillBindsEveryInterface(t *testing.T) {
	other := nonLoopbackIPv4(t)
	if other == "" {
		t.Skip("no non-loopback IPv4 address on this machine")
	}

	dir := t.TempDir()
	gs, err := storage.NewGraphStorage(dir)
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })

	port := freeLoopbackPort(t)
	server, err := NewServerWithDataDir(gs, port, dir)
	if err != nil {
		t.Fatalf("NewServerWithDataDir: %v", err)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- server.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		if err := <-errCh; err != nil && err != http.ErrServerClosed {
			t.Errorf("Start returned %v", err)
		}
	})

	elsewhere := net.JoinHostPort(other, strconv.Itoa(port))
	deadline := time.Now().Add(5 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", elsewhere, 500*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("with no host set the server never answered on %s: %v", elsewhere, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
