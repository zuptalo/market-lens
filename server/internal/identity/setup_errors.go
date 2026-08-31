package identity

import (
	"context"
	"errors"
	"strings"
)

// SMTPVerifier proves a mail configuration actually works before setup stores it. Without
// this, a wrong host, port, or credential is accepted at setup and only discovered when the
// first invitation silently fails - and mail is the only way a member ever signs in.
type SMTPVerifier interface {
	VerifySMTP(context.Context, SMTPSetupConfiguration) error
}

// Classified verification outcomes. The raw server response is deliberately not carried: a
// failure banner can echo the credential that was just sent.
var (
	ErrSMTPUnreachable    = errors.New("mail server could not be reached")
	ErrSMTPTLSFailed      = errors.New("mail server connection could not be encrypted")
	ErrSMTPAuthRejected   = errors.New("mail server rejected the credentials")
	ErrSMTPSenderRejected = errors.New("mail server rejected the sender address")
)

// SetupFieldError names one submitted value the operator must change, why, and what to do.
// It is transport-only and never persisted.
type SetupFieldError struct {
	// Field is the wire name of the input, matching the setup request body.
	Field string
	// Code is machine-readable so the client can react without parsing prose.
	Code string
	// Message is operator-facing. It states the rule rather than implying it, and never
	// contains a submitted secret or raw text from an external server.
	Message string
}

// SetupValidationError reports every field the operator must fix in one response, so setup is
// corrected in one pass instead of one round trip per mistake.
//
// Unreachable distinguishes "we could not check this" from "this is wrong". The two need
// opposite responses from the operator — wait, or retype — and reporting them identically is
// the defect this type exists to remove.
type SetupValidationError struct {
	Fields      []SetupFieldError
	Unreachable bool
}

func (err *SetupValidationError) Error() string {
	if err == nil || len(err.Fields) == 0 {
		return "owner setup input is invalid"
	}
	names := make([]string, 0, len(err.Fields))
	for _, field := range err.Fields {
		names = append(names, field.Field)
	}
	return "owner setup input is invalid: " + strings.Join(names, ", ")
}

// add records one field the operator must fix.
func (err *SetupValidationError) add(field, code, message string) {
	err.Fields = append(err.Fields, SetupFieldError{Field: field, Code: code, Message: message})
}
