package service

import (
	"bufio"
	"context"
	"io"
	"sync"
	"time"
)

// openAISSEReadPump adds cancellation and heartbeat handling to a line scanner.
// BPS can keep a response open for a long time while the model is thinking; a
// plain bufio.Scanner would block forever and prevent keepalive comments.
type openAISSEReadPump struct {
	lines  chan string
	done   chan struct{}
	reader io.Closer
	once   sync.Once
	errMu  sync.RWMutex
	err    error
	text   string
}

func newOpenAISSEReadPump(reader io.Reader, maxLineSize int) *openAISSEReadPump {
	pump := &openAISSEReadPump{lines: make(chan string), done: make(chan struct{})}
	if closer, ok := reader.(io.Closer); ok {
		pump.reader = closer
	}
	go func() {
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64*1024), maxLineSize)
		for scanner.Scan() {
			select {
			case pump.lines <- scanner.Text():
			case <-pump.done:
				return
			}
		}
		pump.errMu.Lock()
		pump.err = scanner.Err()
		pump.errMu.Unlock()
		close(pump.lines)
	}()
	return pump
}

func (p *openAISSEReadPump) Next(ctx context.Context, _ time.Duration, heartbeat <-chan time.Time, keepalive func()) bool {
	for {
		select {
		case line, ok := <-p.lines:
			if !ok {
				return false
			}
			p.text = line
			return true
		case <-heartbeat:
			if keepalive != nil {
				keepalive()
			}
		case <-ctx.Done():
			return false
		case <-p.done:
			return false
		}
	}
}

func (p *openAISSEReadPump) Text() string { return p.text }

func (p *openAISSEReadPump) Err() error {
	p.errMu.RLock()
	defer p.errMu.RUnlock()
	return p.err
}

func (p *openAISSEReadPump) Close() {
	p.once.Do(func() {
		close(p.done)
		if p.reader != nil {
			_ = p.reader.Close()
		}
	})
}
