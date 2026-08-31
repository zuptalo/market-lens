package auth_test

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	appmail "market-lens/server/internal/mail"
)

func TestMemberCodeGeneratorPreservesSixDigitsAndLeadingZerosUnderConcurrency(t *testing.T) {
	generator := auth.NewMemberCodeGenerator(bytes.NewReader(make([]byte, 4096)))
	const workers = 32
	codes := make(chan string, workers)
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			code, err := generator.Generate()
			if err != nil {
				errors <- err
				return
			}
			codes <- code
		}()
	}
	group.Wait()
	close(codes)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`^[0-9]{6}$`)
	count := 0
	for code := range codes {
		count++
		if !pattern.MatchString(code) || code != "000000" {
			t.Fatalf("generated code = %q, want leading-zero six-digit value", code)
		}
	}
	if count != workers {
		t.Fatalf("generated code count = %d, want %d", count, workers)
	}
}

func TestMemberChallengeStoresOnlyKeyedDigestExpiresInTenMinutesAndUsesOnce(t *testing.T) {
	secrets, err := auth.NewSecrets(bytes.Repeat([]byte{0x42}, 32), bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	challenge, err := auth.NewMemberChallenge(
		"40000000-0000-4000-8000-000000000101",
		"10000000-0000-4000-8000-000000000101",
		"012345", secrets, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(challenge.CodeDigest) != 32 || bytes.Equal(challenge.CodeDigest, []byte("012345")) ||
		strings.Contains(fmt.Sprintf("%#v", challenge), "012345") {
		t.Fatalf("challenge retained plaintext code: %#v", challenge)
	}
	if challenge.State != auth.MemberChallengeActive || !challenge.ExpiresAt.Equal(now.Add(10*time.Minute)) ||
		!challenge.ActiveAt(now) || !challenge.ActiveAt(challenge.ExpiresAt.Add(-time.Nanosecond)) ||
		challenge.ActiveAt(challenge.ExpiresAt) {
		t.Fatalf("challenge lifetime/state = %#v", challenge)
	}
	if err := challenge.Consume("012345", secrets, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if challenge.State != auth.MemberChallengeUsed || challenge.UsedAt == nil {
		t.Fatalf("consumed challenge = %#v", challenge)
	}
	if err := challenge.Consume("012345", secrets, now.Add(2*time.Minute)); err == nil {
		t.Fatal("member code replay unexpectedly succeeded")
	}
}

func TestNewMemberChallengeSupersedesEarlierActiveChallenge(t *testing.T) {
	secrets, err := auth.NewSecrets(bytes.Repeat([]byte{0x43}, 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	previous, err := auth.NewMemberChallenge(
		"40000000-0000-4000-8000-000000000102", "10000000-0000-4000-8000-000000000101",
		"123456", secrets, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := previous.Supersede(now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if previous.State != auth.MemberChallengeSuperseded || previous.InvalidatedAt == nil ||
		previous.ActiveAt(now.Add(2*time.Minute)) {
		t.Fatalf("superseded challenge = %#v", previous)
	}
	if err := previous.Consume("123456", secrets, now.Add(2*time.Minute)); err == nil {
		t.Fatal("superseded member code unexpectedly succeeded")
	}
}

func TestMemberCodeMailContainsOnlySafeOneTimeInstructions(t *testing.T) {
	message, err := appmail.MemberCodeMessage("member@example.test", "012345")
	if err != nil {
		t.Fatal(err)
	}
	if message.To != "member@example.test" || !strings.Contains(message.Text, "012345") ||
		!strings.Contains(message.Text, "10 minutes") || !strings.Contains(message.Text, "used once") ||
		strings.Contains(strings.ToLower(message.Text), "password") || strings.Contains(message.Text, "owner@example") {
		t.Fatalf("unsafe member code message = %#v", message)
	}
	for _, code := range []string{"12345", "1234567", "12a456", "12345\n"} {
		if _, err := appmail.MemberCodeMessage("member@example.test", code); err == nil {
			t.Fatalf("invalid member code %q accepted for mail", code)
		}
	}
}
