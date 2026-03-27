package httpapi

import (
	"context"
	"net"
	nethttp "net/http"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/service"
)

func TestServerServeAndShutdown(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	client := &nethttp.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + ln.Addr().String() + "/healthz")
	if err != nil {
		t.Fatalf("get healthz: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("healthz status=%d want=%d", resp.StatusCode, nethttp.StatusOK)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("serve returned error: %v", err)
	}
}

func TestServerListenAndServeDefaultAddr(t *testing.T) {
	_, repo, _ := testHTTPEnv(t)
	svc := service.New(repo, "", nil, nil)
	srv := NewServer(svc, Config{})
	if srv.addr != ":8080" {
		t.Fatalf("default addr=%q want %q", srv.addr, ":8080")
	}
}
