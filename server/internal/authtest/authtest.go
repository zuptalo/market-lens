// Package authtest provides deterministic security-focused test helpers.
package authtest

import (
	"errors"
	"io"
	"strings"
	"sync"
	"time"
)

type Clock struct {
	mu  sync.RWMutex
	now time.Time
}

func NewClock(now time.Time) *Clock {
	return &Clock{now: now}
}

func (c *Clock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

// Set moves the deterministic clock to an exact instant, so tests can jump to a durable
// deadline computed by production code instead of accumulating drift with Advance.
func (c *Clock) Set(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
}

func (c *Clock) Advance(duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(duration)
}

func NewRandomReader(pattern ...byte) io.Reader {
	if len(pattern) == 0 {
		return errorReader{}
	}
	return &repeatingReader{pattern: append([]byte(nil), pattern...)}
}

type repeatingReader struct {
	mu      sync.Mutex
	pattern []byte
	offset  int
}

func (r *repeatingReader) Read(buffer []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range buffer {
		buffer[index] = r.pattern[r.offset]
		r.offset = (r.offset + 1) % len(r.pattern)
	}
	return len(buffer), nil
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("deterministic random pattern is empty")
}

type TestReporter interface {
	Helper()
	Errorf(format string, args ...any)
}

func AssertSecretAbsent(t TestReporter, secret string, outputs ...string) {
	t.Helper()
	if secret == "" {
		t.Errorf("secret assertion requires a non-empty secret")
		return
	}

	secret = strings.ToLower(secret)
	for index, output := range outputs {
		if strings.Contains(strings.ToLower(output), secret) {
			t.Errorf("secret disclosure detected in output %d", index+1)
		}
	}
}
