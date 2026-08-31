// Package mail provides a narrow transactional-email delivery contract.
package mail

import "context"

type Message struct {
	To      string
	Subject string
	Text    string
}

type Sender interface {
	Send(context.Context, Message) error
}

type DeliveryError struct {
	Code      string
	Retryable bool
}

func (e *DeliveryError) Error() string { return "email delivery failed" }
