package authtest

import (
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func TestClockIsDeterministicAndAdvanceable(t *testing.T) {
	start := time.Date(2026, time.August, 29, 9, 30, 0, 0, time.UTC)
	clock := NewClock(start)

	if got := clock.Now(); !got.Equal(start) {
		t.Fatalf("Now() = %v, want %v", got, start)
	}

	clock.Advance(15 * time.Minute)
	want := start.Add(15 * time.Minute)
	if got := clock.Now(); !got.Equal(want) {
		t.Fatalf("Now() after Advance = %v, want %v", got, want)
	}
}

func TestRandomReaderRepeatsItsDeterministicPattern(t *testing.T) {
	reader := NewRandomReader(0x00, 0xff, 0x11)
	got := make([]byte, 8)
	if _, err := io.ReadFull(reader, got); err != nil {
		t.Fatal(err)
	}

	want := []byte{0x00, 0xff, 0x11, 0x00, 0xff, 0x11, 0x00, 0xff}
	if string(got) != string(want) {
		t.Fatalf("random bytes = %v, want %v", got, want)
	}
}

func TestRandomReaderRejectsAnEmptyPattern(t *testing.T) {
	buffer := make([]byte, 1)
	if _, err := NewRandomReader().Read(buffer); err == nil {
		t.Fatal("empty deterministic random pattern unexpectedly succeeded")
	}
}

func TestAssertSecretAbsentReportsOnlyActualDisclosure(t *testing.T) {
	clean := &recordingTest{}
	AssertSecretAbsent(clean, "top-secret", "request failed", "safe response")
	if clean.failed {
		t.Fatalf("clean output failed assertion: %s", clean.message)
	}

	leaked := &recordingTest{}
	AssertSecretAbsent(leaked, "top-secret", "request failed: TOP-SECRET")
	if !leaked.failed || !strings.Contains(leaked.message, "output 1") {
		t.Fatalf("leaked output was not identified safely: failed=%v message=%q", leaked.failed, leaked.message)
	}
	if strings.Contains(strings.ToLower(leaked.message), "top-secret") {
		t.Fatalf("assertion failure disclosed the secret: %q", leaked.message)
	}
}

type recordingTest struct {
	failed  bool
	message string
}

func (*recordingTest) Helper() {}

func (t *recordingTest) Errorf(format string, args ...any) {
	t.failed = true
	t.message = fmt.Sprintf(format, args...)
}
