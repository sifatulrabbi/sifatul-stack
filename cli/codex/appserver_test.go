package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 11, 16, 3, 40, 0, 0, time.UTC)

type fakeServer func(t *testing.T, method string, id *int, write func(string))

func startFake(t *testing.T, handle fakeServer) AppServerStarter {
	t.Helper()
	return func(context.Context) (io.ReadWriteCloser, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			sc := bufio.NewScanner(server)
			write := func(s string) { io.WriteString(server, s+"\n") }
			for sc.Scan() {
				var req struct {
					ID     *int   `json:"id"`
					Method string `json:"method"`
				}
				if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
					t.Errorf("bad request line: %s", sc.Text())
					return
				}
				handle(t, req.Method, req.ID, write)
			}
		}()
		return client, nil
	}
}

func liveFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/appserver_ratelimits_2026-11-16.json")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func TestAppServerHandshakeThenReadsRateLimits(t *testing.T) {
	fixture := liveFixture(t)
	var methods []string
	src := AppServerSource{Now: func() time.Time { return fixedNow }, Start: startFake(t, func(t *testing.T, method string, id *int, write func(string)) {
		methods = append(methods, method)
		switch method {
		case "initialize":
			write(`{"method":"configWarning","params":{}}`)
			write(`{"id":1,"result":{"userAgent":"x"}}`)
		case "account/rateLimits/read":
			write(`{"method":"account/updated","params":{"planType":"plus"}}`)
			write(`{"id":2,"result":` + fixture + `}`)
		}
	})}

	reading, err := src.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(methods, ","); got != "initialize,initialized,account/rateLimits/read" {
		t.Fatalf("methods = %s", got)
	}
	if reading.Source != "codex app-server" || !reading.AsOf.Equal(fixedNow) {
		t.Errorf("reading meta = %+v", reading)
	}
	if reading.ResetCreditsAvailable == nil || *reading.ResetCreditsAvailable != 3 {
		t.Errorf("reset credits = %v", reading.ResetCreditsAvailable)
	}
	if len(reading.Snapshots) != 1 {
		t.Fatalf("snapshots = %+v", reading.Snapshots)
	}
	s := reading.Snapshots[0]
	if s.LimitID != "codex" || s.PlanType != "plus" || s.Secondary != nil || s.CreditsBalance != "0E-10" {
		t.Errorf("snapshot = %+v", s)
	}
	if s.Primary.UsedPercent != 30 || s.Primary.WindowMinutes != 10080 || s.Primary.ResetsAt.Unix() != 1795042855 {
		t.Errorf("primary = %+v", s.Primary)
	}
}

func TestAppServerRPCErrorSurfacesMessage(t *testing.T) {
	src := AppServerSource{Now: time.Now, Start: startFake(t, func(t *testing.T, method string, id *int, write func(string)) {
		switch method {
		case "initialize":
			write(`{"id":1,"result":{}}`)
		case "account/rateLimits/read":
			write(`{"id":2,"error":{"code":-32600,"message":"not logged in"}}`)
		}
	})}

	_, err := src.Read(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v", err)
	}
}

func TestAppServerExitingEarlyIsAnError(t *testing.T) {
	src := AppServerSource{Now: time.Now, Start: func(context.Context) (io.ReadWriteCloser, error) {
		client, server := net.Pipe()
		go func() {
			bufio.NewReader(server).ReadBytes('\n')
			server.Close()
		}()
		return client, nil
	}}

	_, err := src.Read(context.Background())
	if err == nil || !strings.Contains(err.Error(), "closed before replying") {
		t.Fatalf("err = %v", err)
	}
}

func TestAppServerHangingRespectsContextDeadline(t *testing.T) {
	src := AppServerSource{Now: time.Now, Start: startFake(t, func(*testing.T, string, *int, func(string)) {})}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := src.Read(ctx)
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("took %s", time.Since(start))
	}
}
