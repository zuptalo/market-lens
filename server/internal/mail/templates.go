package mail

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func OwnerRecoveryMessage(recipient, recoveryURL string) (Message, error) {
	if err := validateActionURL(recoveryURL); err != nil {
		return Message{}, err
	}
	return newTemplateMessage(recipient, "Recover your Market Lens owner account",
		"A recovery was requested for your Market Lens owner account.\n\nOpen this one-time link to continue:\n"+recoveryURL+"\n\nIf you did not request this, ignore this message.")
}

func MemberCodeMessage(recipient, code string) (Message, error) {
	if len(code) != 6 {
		return Message{}, errors.New("member code must contain six digits")
	}
	for _, character := range code {
		if character < '0' || character > '9' {
			return Message{}, errors.New("member code must contain six digits")
		}
	}
	return newTemplateMessage(recipient, "Your Market Lens sign-in code",
		fmt.Sprintf("Your requested Market Lens sign-in code is:\n\n%s\n\nIt expires in 10 minutes and can be used once. If you did not request it, ignore this message.", code))
}

func InvitationMessage(recipient, invitationURL string) (Message, error) {
	if err := validateActionURL(invitationURL); err != nil {
		return Message{}, err
	}
	return newTemplateMessage(recipient, "Your Market Lens invitation",
		"You have been invited to Market Lens.\n\nOpen this one-time link to accept:\n"+invitationURL+"\n\nIf you were not expecting this, ignore this message.")
}

func newTemplateMessage(recipient, subject, text string) (Message, error) {
	message := Message{To: recipient, Subject: subject, Text: text}
	if err := validateMessage(message); err != nil {
		return Message{}, errors.New("account email input is invalid")
	}
	return message, nil
}

func validateActionURL(value string) error {
	if len(value) == 0 || len(value) > 2048 || strings.ContainsAny(value, "\r\n") {
		return errors.New("account action URL is invalid")
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.Fragment == "" {
		return errors.New("account action URL is invalid")
	}
	return nil
}
