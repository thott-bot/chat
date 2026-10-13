package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type codexClient struct {
	agentProcess
	ctx     context.Context
	id      int64
	started time.Time

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan rpcMessage
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int64  `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return e.Message }

var (
	errCodexStopped = errors.New("Codex stopped")
	clientCount     atomic.Int64
)

type (
	notifyFunc  func(method string, params json.RawMessage)
	requestFunc func(c *codexClient, id json.RawMessage, method string, params json.RawMessage)
)

func startCodex(ctx context.Context, path string, onNotify notifyFunc, onRequest requestFunc) (*codexClient, error) {
	c := &codexClient{ctx: ctx, id: clientCount.Add(1), pending: map[int64]chan rpcMessage{}}
	err := c.start(path, "", []string{"app-server"}, func(line []byte) {
		var m rpcMessage
		if json.Unmarshal(line, &m) != nil {
			return
		}
		hasID := len(m.ID) > 0 && string(m.ID) != "null"
		switch {
		case m.Method != "" && hasID:
			onRequest(c, m.ID, m.Method, m.Params)
		case m.Method != "":
			onNotify(m.Method, m.Params)
		case hasID:
			c.resolve(m)
		}
	}, nil)
	if err != nil {
		return nil, err
	}
	c.started = time.Now()
	return c, nil
}

func (c *codexClient) resolve(m rpcMessage) {
	id, err := strconv.ParseInt(string(m.ID), 10, 64)
	if err != nil {
		return
	}
	c.mu.Lock()
	ch := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if ch != nil {
		ch <- m
	}
}

func (c *codexClient) call(method string, params, result any) error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan rpcMessage, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()
	if err := c.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case <-c.ctx.Done():
		return c.ctx.Err()
	case m := <-ch:
		if m.Error != nil {
			return m.Error
		}
		if result != nil {
			return json.Unmarshal(m.Result, result)
		}
		return nil
	case <-time.After(30 * time.Second):
		return fmt.Errorf("%s timed out", method)
	case <-c.done:
		return errCodexStopped
	}
}

func (c *codexClient) notify(method string) error {
	return c.send(map[string]any{"method": method})
}

func (c *codexClient) respond(id json.RawMessage, result any) error {
	return c.send(map[string]any{"id": id, "result": result})
}

func (c *codexClient) respondError(id json.RawMessage, message string) error {
	return c.send(map[string]any{"id": id, "error": rpcError{Code: -32601, Message: message}})
}
