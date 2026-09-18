package notify

import (
	"context"
	"crypto/hmac"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Stopping a kind of message from a link in that message, without signing in.
//
// An unsubscribe that requires signing in is one people do not use: they mark the mail as spam
// instead, and the sending domain pays for it. So the link carries a signed token that names one
// person, one kind and one channel — and can do nothing else. It cannot read, it cannot enable, and
// it cannot touch a second preference.

// Signer is the instance's signing key, narrowed to the one operation this needs. Narrowed rather
// than passed whole so this package cannot sign anything else with it.
type Signer interface {
	Sign(value string) []byte
}

// UnsubscribeLifetime is how long a link in an email keeps working. Thirty days: long enough that
// somebody coming back to an old message can still stop it, short enough that a leaked mailbox from
// a year ago is not a live control.
const UnsubscribeLifetime = 30 * 24 * time.Hour

// unsubscribeToken builds the signed, single-purpose token for one preference.
func (r *Repository) unsubscribeToken(ctx context.Context, userID string, kind Kind,
	channel Channel) (string, error) {
	if r.signer == nil {
		return "", fmt.Errorf("no signing key is configured for unsubscribe links")
	}
	expires := time.Now().UTC().Add(UnsubscribeLifetime).Unix()
	claim := strings.Join([]string{userID, string(kind), string(channel),
		strconv.FormatInt(expires, 10)}, "|")
	signature := r.signer.Sign(claim)
	return base64.RawURLEncoding.EncodeToString([]byte(claim)) + "." +
		base64.RawURLEncoding.EncodeToString(signature), nil
}

// Unsubscribe turns off exactly one kind on one channel, for whoever the token names.
//
// It only ever disables. A token that could enable something would turn a link in an email into a
// way of signing somebody up for more mail.
func (s *Service) Unsubscribe(ctx context.Context, token string) (Kind, Channel, error) {
	if s == nil || s.repository == nil || s.repository.signer == nil {
		return "", "", Refusal{Code: RefusalInvalidToken, Message: "This link cannot be used."}
	}
	invalid := Refusal{Code: RefusalInvalidToken,
		Message: "This link is not readable, or has expired. You can turn notifications off under Account settings."}

	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return "", "", invalid
	}
	claim, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", "", invalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", invalid
	}
	// Constant time, and before anything in the claim is believed.
	if !hmac.Equal(signature, s.repository.signer.Sign(string(claim))) {
		return "", "", invalid
	}

	fields := strings.Split(string(claim), "|")
	if len(fields) != 4 {
		return "", "", invalid
	}
	userID, kind, channel := fields[0], Kind(fields[1]), Channel(fields[2])
	expires, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil || time.Now().UTC().After(time.Unix(expires, 0)) {
		return "", "", invalid
	}
	if !knownKind(kind) || (channel != ChannelEmail && channel != ChannelWebPush) {
		return "", "", invalid
	}

	// Only ever off.
	if err := s.repository.setPreference(ctx, userID, kind, channel, false); err != nil {
		return "", "", err
	}
	return kind, channel, nil
}
