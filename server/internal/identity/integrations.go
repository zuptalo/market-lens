package identity

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"market-lens/server/internal/credentials"
	"market-lens/server/internal/marketdata"
)

// SMTPUpdate is a submitted mail configuration. Password is a pointer so that "not supplied"
// and "deliberately empty" stay distinguishable: an owner changing a port must not have to
// retype a password they cannot read back, while an owner clearing authentication must be
// able to say so.
type SMTPUpdate struct {
	Host     string
	Port     int
	From     string
	Username string
	Password *string
}

// EODHDUpdate is a replacement provider key. The current key can never be read back, so a
// change is always a full replacement.
type EODHDUpdate struct {
	APIKey string
}

// IntegrationUpdate carries whichever integrations the owner is changing. Each is independent:
// requiring the provider key in order to change a mail port would be impossible to satisfy,
// because the key is write-only.
type IntegrationUpdate struct {
	SMTP  *SMTPUpdate
	EODHD *EODHDUpdate
}

// Changed reports whether the submission asks for anything at all.
func (update IntegrationUpdate) Changed() bool { return update.SMTP != nil || update.EODHD != nil }

// SMTPIntegrationSettings is the non-secret view of the stored mail configuration. Host, port,
// sender and username are configuration and are returned so the owner can edit them.
// PasswordConfigured stands in for the password, which is never returned.
type SMTPIntegrationSettings struct {
	Host               string
	Port               int
	From               string
	Username           string
	PasswordConfigured bool
}

// IntegrationSettings is what the owner is shown. It deliberately holds no secret.
type IntegrationSettings struct {
	EODHDConfigured  bool
	EODHDValidatedAt *string
	SMTPConfigured   bool
	SMTP             SMTPIntegrationSettings
}

// ErrNoIntegrationChange rejects a submission that asks for nothing, rather than reporting a
// success that changed nothing.
var ErrNoIntegrationChange = errors.New("no integration change was requested")

// IntegrationSettings returns what the owner may see: configuration, never secrets. The mail
// host, port, sender and username are returned because an owner cannot edit what they cannot
// read; the password and the provider key never are.
func (service *Service) IntegrationSettings(ctx context.Context, actor Actor) (IntegrationSettings, error) {
	if err := service.requireOwner(ctx, actor); err != nil {
		return IntegrationSettings{}, err
	}
	if service.credentials == nil {
		return IntegrationSettings{}, errors.New("integration settings are unavailable")
	}
	statuses, err := service.credentials.Statuses(ctx)
	if err != nil {
		return IntegrationSettings{}, err
	}
	settings := IntegrationSettings{}
	for _, status := range statuses {
		switch status.Kind {
		case credentials.KindEODHDAPI:
			settings.EODHDConfigured = status.Configured
			if status.ValidatedAt != nil {
				validated := status.ValidatedAt.UTC().Format(time.RFC3339)
				settings.EODHDValidatedAt = &validated
			}
		case credentials.KindSMTP:
			settings.SMTPConfigured = status.Configured
		}
	}
	if !settings.SMTPConfigured || service.credentialCipher == nil {
		return settings, nil
	}
	stored, err := service.credentials.SMTPSettings(ctx, service.credentialCipher)
	if err != nil {
		return IntegrationSettings{}, err
	}
	settings.SMTP = SMTPIntegrationSettings{
		Host: stored.Host, Port: stored.Port, From: stored.From, Username: stored.Username,
		PasswordConfigured: stored.Password != "",
	}
	return settings, nil
}

// VerifyIntegrations proves a submitted configuration works and stores nothing.
func (service *Service) VerifyIntegrations(ctx context.Context, actor Actor,
	update IntegrationUpdate) (IntegrationOutcomes, error) {
	_, outcomes, err := service.checkIntegrations(ctx, actor, update)
	return outcomes, err
}

// UpdateIntegrations stores a configuration only after proving it works. Nothing is written
// unless every submitted integration verifies, so a change can never leave the installation
// half-configured with something that does not work.
func (service *Service) UpdateIntegrations(ctx context.Context, actor Actor,
	update IntegrationUpdate) (IntegrationOutcomes, error) {
	replacements, outcomes, err := service.checkIntegrations(ctx, actor, update)
	if err != nil {
		return outcomes, err
	}
	if service.credentialCipher == nil {
		validation := &SetupValidationError{}
		validation.add("eodhd_api_key", "unavailable",
			"EXTERNAL_CREDENTIAL_KEY is not configured, so credentials cannot be encrypted and stored.")
		return outcomes, validation
	}
	return outcomes, service.credentials.Replace(ctx, service.credentialCipher, actor.UserID,
		service.now().UTC(), replacements)
}

// checkIntegrations is the shared path: authorize, validate shape, resolve what was omitted
// from what is stored, then prove each submitted integration actually works. It returns the
// sealed-ready plaintexts so a save writes exactly what was verified.
func (service *Service) checkIntegrations(ctx context.Context, actor Actor,
	update IntegrationUpdate) ([]credentials.Replacement, IntegrationOutcomes, error) {
	outcomes := newIntegrationOutcomes()
	if err := service.requireOwner(ctx, actor); err != nil {
		return nil, outcomes, err
	}
	if !update.Changed() {
		return nil, outcomes, ErrNoIntegrationChange
	}
	validation := &SetupValidationError{}
	var smtp SMTPSetupConfiguration
	if update.SMTP != nil {
		smtp = SMTPSetupConfiguration{
			Host: update.SMTP.Host, Port: update.SMTP.Port, From: update.SMTP.From,
			Username: update.SMTP.Username,
		}
		// An omitted password means "keep the one already stored", so the check runs against
		// the credentials that would actually be used rather than an empty one.
		if update.SMTP.Password != nil {
			smtp.Password = *update.SMTP.Password
		} else {
			if service.credentialCipher == nil {
				return nil, outcomes, errors.New("stored SMTP credentials are unavailable")
			}
			stored, err := service.credentials.SMTPSettings(ctx, service.credentialCipher)
			if err != nil {
				return nil, outcomes, err
			}
			smtp.Password = stored.Password
		}
		validateSMTPShape(smtp, validation)
	}
	if update.EODHD != nil {
		validateEODHDKeyShape(update.EODHD.APIKey, validation)
	}
	// A shape problem stops every network call, so nothing has been checked at this point.
	if len(validation.Fields) > 0 {
		return nil, outcomes, validation
	}

	if update.EODHD != nil {
		outcomes.EODHD = IntegrationVerified
		if err := service.eodhdValidator.ValidateCredential(ctx, update.EODHD.APIKey); err != nil {
			outcomes.EODHD = IntegrationFailed
			var providerError *marketdata.ProviderError
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
				errors.Is(err, ErrProviderUnavailable) || errors.As(err, &providerError) && providerError.Transient {
				validation.Unreachable = true
				validation.add("eodhd_api_key", "unreachable",
					"EODHD could not be reached, so the API key could not be checked. Try again shortly.")
			} else {
				validation.add("eodhd_api_key", "rejected",
					"EODHD rejected this API key. Check it in your EODHD account and paste it again.")
			}
		}
	}
	if update.SMTP != nil {
		before := len(validation.Fields)
		service.verifySetupMail(ctx, smtp, validation)
		outcomes.SMTP = IntegrationVerified
		if len(validation.Fields) > before {
			outcomes.SMTP = IntegrationFailed
		}
	}
	if len(validation.Fields) > 0 {
		return nil, outcomes, validation
	}

	replacements := make([]credentials.Replacement, 0, 2)
	if update.EODHD != nil {
		plaintext, err := json.Marshal(map[string]any{"api_key": update.EODHD.APIKey})
		if err != nil {
			return nil, outcomes, errors.New("encode the provider credential")
		}
		replacements = append(replacements, credentials.Replacement{
			Kind: credentials.KindEODHDAPI, Plaintext: plaintext,
		})
	}
	if update.SMTP != nil {
		payload := map[string]any{"host": smtp.Host, "port": smtp.Port, "from": smtp.From}
		if smtp.Username != "" {
			payload["username"] = smtp.Username
		}
		if smtp.Password != "" {
			payload["password"] = smtp.Password
		}
		plaintext, err := json.Marshal(payload)
		if err != nil {
			return nil, outcomes, errors.New("encode the mail configuration")
		}
		replacements = append(replacements, credentials.Replacement{
			Kind: credentials.KindSMTP, Plaintext: plaintext,
		})
	}
	return replacements, outcomes, nil
}

// IntegrationOutcome is what actually happened to one integration during a check. "No error
// reported" is not the same as "verified": when a submitted value is the wrong shape, no
// network call is made for either integration, so neither was checked.
type IntegrationOutcome string

const (
	IntegrationNotChecked IntegrationOutcome = "not_checked"
	IntegrationVerified   IntegrationOutcome = "verified"
	IntegrationFailed     IntegrationOutcome = "failed"
)

// IntegrationOutcomes reports each integration separately, so the owner can be told which half
// of their configuration works rather than only whether the whole submission passed.
type IntegrationOutcomes struct {
	EODHD IntegrationOutcome
	SMTP  IntegrationOutcome
}

func newIntegrationOutcomes() IntegrationOutcomes {
	return IntegrationOutcomes{EODHD: IntegrationNotChecked, SMTP: IntegrationNotChecked}
}
