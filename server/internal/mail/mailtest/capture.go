// Package mailtest provides transactional-email test doubles.
package mailtest

import (
	"context"
	"strings"
	"sync"
)

type Capture[T any] struct {
	mu       sync.RWMutex
	messages []T
}

func NewCapture[T any]() *Capture[T] {
	return &Capture[T]{}
}

func (c *Capture[T]) Send(ctx context.Context, message T) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, message)
	return nil
}

func (c *Capture[T]) Messages() []T {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]T(nil), c.messages...)
}

type Failure[T any] struct {
	mu       sync.RWMutex
	err      error
	attempts int
}

func NewFailure[T any](err error) *Failure[T] {
	return &Failure[T]{err: err}
}

func (f *Failure[T]) Send(ctx context.Context, _ T) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	return f.err
}

func (f *Failure[T]) Attempts() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.attempts
}

type TestReporter interface {
	Helper()
	Errorf(format string, args ...any)
}

func AssertSafeText(t TestReporter, secret string, text ...string) {
	t.Helper()
	if secret == "" {
		t.Errorf("safe-text assertion requires a non-empty secret")
		return
	}

	secret = strings.ToLower(secret)
	for index, value := range text {
		if strings.Contains(strings.ToLower(value), secret) {
			t.Errorf("secret disclosure detected in text %d", index+1)
		}
	}
}
