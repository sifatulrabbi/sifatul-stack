package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"sync"
	"time"
)

type AppServerStarter func(ctx context.Context) (io.ReadWriteCloser, error)

type AppServerSource struct {
	Start AppServerStarter
	Now   func() time.Time
}

func (a AppServerSource) Read(ctx context.Context) (Reading, error) {
	conn, err := a.Start(ctx)
	if err != nil {
		return Reading{}, fmt.Errorf("codex: start app-server: %w", err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	rpc := newRPC(conn)
	init := map[string]any{
		"clientInfo":   map[string]string{"name": "usage-shortly", "version": "0.1.0"},
		"capabilities": map[string]any{"experimentalApi": true},
	}
	if err := rpc.call(1, "initialize", init, nil); err != nil {
		return Reading{}, wrapCtx(ctx, err)
	}
	if err := rpc.notify("initialized"); err != nil {
		return Reading{}, wrapCtx(ctx, err)
	}
	var result rateLimitsResult
	if err := rpc.call(2, "account/rateLimits/read", nil, &result); err != nil {
		return Reading{}, wrapCtx(ctx, err)
	}
	return result.reading(a.Now()), nil
}

func wrapCtx(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("codex: app-server: %w", ctx.Err())
	}
	return fmt.Errorf("codex: app-server: %w", err)
}

type apiWindow struct {
	UsedPercent        float64 `json:"usedPercent"`
	WindowDurationMins int     `json:"windowDurationMins"`
	ResetsAt           *int64  `json:"resetsAt"`
}

func (w *apiWindow) toWindow() *Window {
	if w == nil {
		return nil
	}
	return &Window{UsedPercent: w.UsedPercent, WindowMinutes: w.WindowDurationMins, ResetsAt: unixPtr(w.ResetsAt)}
}

type apiSnapshot struct {
	LimitID   string     `json:"limitId"`
	PlanType  string     `json:"planType"`
	Primary   *apiWindow `json:"primary"`
	Secondary *apiWindow `json:"secondary"`
	Credits   *struct {
		Unlimited bool    `json:"unlimited"`
		Balance   *string `json:"balance"`
	} `json:"credits"`
}

func (s apiSnapshot) toSnapshot() Snapshot {
	out := Snapshot{LimitID: s.LimitID, PlanType: s.PlanType, Primary: s.Primary.toWindow(), Secondary: s.Secondary.toWindow()}
	if out.LimitID == "" {
		out.LimitID = "codex"
	}
	if s.Credits != nil {
		out.CreditsUnlimited = s.Credits.Unlimited
		if s.Credits.Balance != nil {
			out.CreditsBalance = *s.Credits.Balance
		}
	}
	return out
}

type rateLimitsResult struct {
	RateLimits            *apiSnapshot           `json:"rateLimits"`
	RateLimitsByLimitID   map[string]apiSnapshot `json:"rateLimitsByLimitId"`
	RateLimitResetCredits *struct {
		AvailableCount int `json:"availableCount"`
	} `json:"rateLimitResetCredits"`
}

func (r rateLimitsResult) reading(now time.Time) Reading {
	out := Reading{Source: "codex app-server", AsOf: now}
	if len(r.RateLimitsByLimitID) > 0 {
		ids := make([]string, 0, len(r.RateLimitsByLimitID))
		for id := range r.RateLimitsByLimitID {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			s := r.RateLimitsByLimitID[id]
			if s.LimitID == "" {
				s.LimitID = id
			}
			out.Snapshots = append(out.Snapshots, s.toSnapshot())
		}
	} else if r.RateLimits != nil {
		out.Snapshots = []Snapshot{r.RateLimits.toSnapshot()}
	}
	if r.RateLimitResetCredits != nil {
		n := r.RateLimitResetCredits.AvailableCount
		out.ResetCreditsAvailable = &n
	}
	return out
}

type rpc struct {
	w   io.Writer
	r   *bufio.Reader
	enc *json.Encoder
}

func newRPC(conn io.ReadWriter) *rpc {
	return &rpc{w: conn, r: bufio.NewReaderSize(conn, 1<<20), enc: json.NewEncoder(conn)}
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcMessage struct {
	ID     *int            `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

func (c *rpc) notify(method string) error {
	return c.enc.Encode(map[string]any{"method": method})
}

func (c *rpc) call(id int, method string, params any, out any) error {
	req := map[string]any{"id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	if err := c.enc.Encode(req); err != nil {
		return err
	}
	for {
		line, err := c.r.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			if errors.Is(err, io.EOF) {
				return fmt.Errorf("%s: app-server closed before replying", method)
			}
			return err
		}
		var msg rpcMessage
		if json.Unmarshal(line, &msg) != nil || msg.ID == nil || *msg.ID != id {
			continue
		}
		if msg.Error != nil {
			return fmt.Errorf("%s: %s (code %d)", method, msg.Error.Message, msg.Error.Code)
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(msg.Result, out)
	}
}

type processConn struct {
	io.Reader
	io.WriteCloser
	cmd  *exec.Cmd
	once sync.Once
}

func (p *processConn) Close() error {
	p.once.Do(func() {
		p.WriteCloser.Close()
		p.cmd.Process.Kill()
		p.cmd.Wait()
	})
	return nil
}

func ExecAppServer(binary string) AppServerStarter {
	return func(ctx context.Context) (io.ReadWriteCloser, error) {
		cmd := exec.CommandContext(ctx, binary, "app-server", "--listen", "stdio://")
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return &processConn{Reader: stdout, WriteCloser: stdin, cmd: cmd}, nil
	}
}
