package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"market-lens/server/internal/api"
	"market-lens/server/internal/auth"
	"market-lens/server/internal/config"
	"market-lens/server/internal/credentials"
	"market-lens/server/internal/db"
	clientevents "market-lens/server/internal/events"
	"market-lens/server/internal/identity"
	"market-lens/server/internal/instruments"
	"market-lens/server/internal/mail"
	"market-lens/server/internal/marketdata"
	"market-lens/server/internal/marketdata/eodhd"
	"market-lens/server/internal/scheduler"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/term"
)

var version = "dev"

// The identity service is what serves owner integration administration. Stating it here means
// a signature drift is a build failure rather than a nil dependency discovered in a browser.
var _ api.IntegrationAdministration = (*identity.Service)(nil)

type marketDataCommand struct {
	Kind     marketdata.ImportKind
	Universe string
	From     marketdata.SessionDate
	To       marketdata.SessionDate
	RunID    instruments.UUID
}

type marketDataImporter interface {
	Import(context.Context, marketdata.ImportRequest) (marketdata.ImportRun, error)
}

type marketDataRetrier interface {
	Retry(context.Context, marketdata.RetryRequest) (marketdata.ImportRun, error)
}

type setupCapabilityIssuer interface {
	IssueSetupCapability(context.Context) (identity.SetupCapability, error)
}

type ownerPasswordResetter interface {
	ResetOwnerPassword(context.Context, string) error
}

type credentialKeyRotator interface {
	Rotate(context.Context, *credentials.Cipher, *credentials.Cipher, time.Time) error
}

type passwordTerminal interface {
	IsTerminal() bool
	ReadPassword(string) ([]byte, error)
	// ReadLine echoes what is typed. A confirmation word is not a secret, and hiding it
	// would leave the operator unable to see what they are agreeing to.
	ReadLine(string) (string, error)
}

type osPasswordTerminal struct {
	input  *os.File
	output io.Writer
}

func (terminal osPasswordTerminal) IsTerminal() bool {
	return terminal.input != nil && term.IsTerminal(int(terminal.input.Fd()))
}

func (terminal osPasswordTerminal) ReadLine(prompt string) (string, error) {
	if terminal.input == nil || terminal.output == nil {
		return "", errors.New("interactive terminal is unavailable")
	}
	if _, err := io.WriteString(terminal.output, prompt); err != nil {
		return "", err
	}
	reader := bufio.NewReader(terminal.input)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func (terminal osPasswordTerminal) ReadPassword(prompt string) ([]byte, error) {
	if terminal.input == nil || terminal.output == nil {
		return nil, errors.New("interactive terminal is unavailable")
	}
	if _, err := io.WriteString(terminal.output, prompt); err != nil {
		return nil, err
	}
	password, err := term.ReadPassword(int(terminal.input.Fd()))
	_, newlineErr := io.WriteString(terminal.output, "\n")
	if err != nil {
		return nil, err
	}
	if newlineErr != nil {
		clear(password)
		return nil, newlineErr
	}
	return password, nil
}

func executeOwnerPasswordReset(ctx context.Context, resetter ownerPasswordResetter, terminal passwordTerminal,
	output io.Writer, logger *slog.Logger) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if resetter == nil || terminal == nil || output == nil || logger == nil {
		return errors.New("owner password reset dependencies are unavailable")
	}
	if !terminal.IsTerminal() {
		return errors.New("owner password reset requires an interactive terminal")
	}
	password, err := terminal.ReadPassword("New owner password: ")
	if err != nil {
		return fmt.Errorf("read new owner password: %w", err)
	}
	defer clear(password)
	confirmation, err := terminal.ReadPassword("Confirm new owner password: ")
	if err != nil {
		return fmt.Errorf("confirm new owner password: %w", err)
	}
	defer clear(confirmation)
	if utf8.RuneCount(password) < 12 || utf8.RuneCount(password) > 1024 {
		return errors.New("owner password must contain between 12 and 1024 characters")
	}
	if !bytes.Equal(password, confirmation) {
		return errors.New("owner password confirmation does not match")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := resetter.ResetOwnerPassword(ctx, string(password)); err != nil {
		return err
	}
	if _, err := io.WriteString(output, "owner_password_reset=complete sessions_revoked=all\n"); err != nil {
		return fmt.Errorf("write owner password reset result: %w", err)
	}
	logger.Info("owner password reset completed", "sessions_revoked", "all")
	return nil
}

func executeCredentialKeyRotation(ctx context.Context, rotator credentialKeyRotator,
	current config.ExternalCredentialConfig, newVersion uint32, terminal passwordTerminal,
	output io.Writer, logger *slog.Logger, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if rotator == nil || terminal == nil || output == nil || logger == nil || !current.Configured ||
		len(current.Key) != 32 || current.KeyVersion == 0 || now.IsZero() {
		return errors.New("credential-key rotation dependencies are unavailable")
	}
	if newVersion <= current.KeyVersion {
		return errors.New("new credential-key version must be greater than the current version")
	}
	if !terminal.IsTerminal() {
		return errors.New("credential-key rotation requires an interactive terminal")
	}
	encodedKey, err := terminal.ReadPassword("New external credential key (base64): ")
	if err != nil {
		return fmt.Errorf("read new external credential key: %w", err)
	}
	defer clear(encodedKey)
	confirmation, err := terminal.ReadPassword("Repeat new external credential key (base64): ")
	if err != nil {
		return fmt.Errorf("confirm new external credential key: %w", err)
	}
	defer clear(confirmation)
	if !bytes.Equal(encodedKey, confirmation) {
		return errors.New("external credential key confirmation does not match")
	}
	newKey, err := base64.StdEncoding.Strict().DecodeString(string(encodedKey))
	if err != nil || len(newKey) != 32 {
		clear(newKey)
		return errors.New("new external credential key must be base64 encoding of exactly 32 bytes")
	}
	defer clear(newKey)
	oldCipher, err := credentials.NewCipher(current.Key, current.KeyVersion)
	if err != nil {
		return err
	}
	newCipher, err := credentials.NewCipher(newKey, newVersion)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := rotator.Rotate(ctx, oldCipher, newCipher, now.UTC()); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "credential_key_rotation=complete key_version=%d\n", newVersion); err != nil {
		return fmt.Errorf("write credential-key rotation result: %w", err)
	}
	logger.Info("external credential key rotation completed", "key_version", newVersion)
	return nil
}

type signingKeyRotator interface {
	RotateSigningKey(context.Context, []byte, time.Time) (auth.SigningKeyRecord, error)
}

// executeSigningKeyRotation replaces the instance signing key from the host. Host access is
// the authorization boundary here: an owner session is itself derived from the key being
// replaced, so authorizing this with one would be circular.
func executeSigningKeyRotation(ctx context.Context, rotator signingKeyRotator, terminal passwordTerminal,
	output io.Writer, logger *slog.Logger, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if rotator == nil || terminal == nil || output == nil || logger == nil || now.IsZero() {
		return errors.New("signing-key rotation dependencies are unavailable")
	}
	if !terminal.IsTerminal() {
		return errors.New("signing-key rotation requires an interactive terminal")
	}
	confirmation, err := terminal.ReadLine(
		"This ends every active session and invalidates outstanding invitations, owner setup\n" +
			"links, and login codes. Type ROTATE to continue: ")
	if err != nil {
		return fmt.Errorf("read signing-key rotation confirmation: %w", err)
	}
	if confirmation != "ROTATE" {
		return errors.New("signing-key rotation was not confirmed")
	}
	newKey, err := auth.GenerateSigningKey(rand.Reader)
	if err != nil {
		return err
	}
	defer clear(newKey)
	if err := ctx.Err(); err != nil {
		return err
	}
	record, err := rotator.RotateSigningKey(ctx, newKey, now.UTC())
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "signing_key_rotation=complete generation=%d sessions_revoked=all\n",
		record.Generation); err != nil {
		return fmt.Errorf("write signing-key rotation result: %w", err)
	}
	// The generation is an ordinal, not a secret, and is the only key identifier that may
	// ever be logged.
	logger.Info("instance signing key rotation completed",
		"generation", record.Generation, "sessions_revoked", "all")
	return nil
}

func executeSetupLink(ctx context.Context, issuer setupCapabilityIssuer, output io.Writer, logger *slog.Logger) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	capability, err := issuer.IssueSetupCapability(ctx)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "setup_url=/setup#%s\n", capability.Token); err != nil {
		return fmt.Errorf("write owner setup link: %w", err)
	}
	logger.Info("owner setup capability issued",
		"capability_id", capability.ID,
		"expires_at", capability.ExpiresAt.UTC().Format(time.RFC3339),
	)
	return nil
}

func executeAuthCommand(ctx context.Context, args []string, issuer setupCapabilityIssuer,
	resetter ownerPasswordResetter, rotator credentialKeyRotator, signingKeys signingKeyRotator,
	externalConfig config.ExternalCredentialConfig,
	terminal passwordTerminal, output io.Writer, logger *slog.Logger) error {
	if len(args) == 2 && args[0] == "auth" && args[1] == "setup-link" {
		return executeSetupLink(ctx, issuer, output, logger)
	}
	if len(args) == 3 && args[0] == "auth" && args[1] == "owner-password" && args[2] == "reset" {
		return executeOwnerPasswordReset(ctx, resetter, terminal, output, logger)
	}
	if len(args) == 3 && args[0] == "auth" && args[1] == "signing-key" && args[2] == "rotate" {
		return executeSigningKeyRotation(ctx, signingKeys, terminal, output, logger, time.Now())
	}
	if len(args) >= 3 && args[0] == "auth" && args[1] == "credential-key" && args[2] == "rotate" {
		flags := flag.NewFlagSet("auth credential-key rotate", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		newVersion := flags.Uint64("new-version", 0, "new external credential key version")
		if err := flags.Parse(args[3:]); err != nil || flags.NArg() != 0 || *newVersion == 0 || *newVersion > math.MaxUint32 {
			return errors.New("credential-key rotation requires a positive --new-version")
		}
		return executeCredentialKeyRotation(ctx, rotator, externalConfig, uint32(*newVersion), terminal,
			output, logger, time.Now())
	}
	return errors.New("expected auth setup-link, auth owner-password reset, " +
		"auth credential-key rotate, or auth signing-key rotate")
}

func newIdentityService(authConfig config.AuthConfig, signingKey []byte,
	externalConfig config.ExternalCredentialConfig, validator identity.EODHDCredentialValidator,
	verifier identity.SMTPVerifier, pool *pgxpool.Pool, logger *slog.Logger) (*identity.Service, error) {
	passwords, err := auth.NewPasswordHasher(rand.Reader, auth.DefaultArgon2Params())
	if err != nil {
		return nil, err
	}
	secrets, err := auth.NewSecrets(signingKey, rand.Reader)
	if err != nil {
		return nil, err
	}
	credentialCipher, err := newCredentialCipher(externalConfig)
	if err != nil {
		return nil, err
	}
	return identity.NewService(identity.ServiceDependencies{
		Repository:             identity.NewRepository(pool),
		Passwords:              passwords,
		Secrets:                secrets,
		Now:                    time.Now,
		SetupTTL:               authConfig.SetupTTL,
		OwnerIdleTimeout:       authConfig.OwnerIdleTimeout,
		SessionAbsoluteTimeout: authConfig.SessionAbsoluteTimeout,
		EODHDValidator:         validator,
		SMTPVerifier:           verifier,
		CredentialCipher:       credentialCipher,
		Credentials:            credentials.NewRepository(pool),
		MemberAccess:           auth.NewRepository(pool),
		Mail:                   newStoredSMTPSender(credentials.NewRepository(pool), credentialCipher, logger),
		AppBaseURL:             authConfig.AppBaseURL,
		Logger:                 logger,
	})
}

// resolveInstanceSigningKey returns the key this process signs with. It must run after
// db.Migrate, because the key lives in the database the migration creates, and before any
// service is constructed, because every one of them derives its digests from it.
func resolveInstanceSigningKey(ctx context.Context, authConfig config.AuthConfig,
	pool *pgxpool.Pool) (auth.SigningKeyResolution, error) {
	return auth.NewRepository(pool).ResolveSigningKey(ctx, authConfig.Secret, rand.Reader, time.Now())
}

// logInstanceConfiguration reports which values this installation provisioned for itself and
// which the operator must retain to restore it elsewhere. It names no secret: the signing key
// generation is an ordinal, and the credential key is reported only as present or absent.
func logInstanceConfiguration(logger *slog.Logger, signingKey auth.SigningKeyResolution,
	externalConfig config.ExternalCredentialConfig, credentialsStored bool) {
	mustRetain := make([]string, 0, 2)
	if signingKey.Source == auth.SigningKeySupplied {
		mustRetain = append(mustRetain, "AUTH_SECRET")
	}
	if externalConfig.Configured || credentialsStored {
		mustRetain = append(mustRetain, "EXTERNAL_CREDENTIAL_KEY")
	}
	externalSource := "absent"
	if externalConfig.Configured {
		externalSource = "supplied"
	}
	logger.Info("instance configuration resolved",
		"signing_key", string(signingKey.Source),
		"signing_key_generation", signingKey.Generation,
		"external_credential_key", externalSource,
		"operator_must_retain", mustRetain,
	)
	if !externalConfig.Configured && !credentialsStored {
		logger.Warn("EXTERNAL_CREDENTIAL_KEY is not configured; provider credentials cannot be "+
			"stored until one is supplied, and it must be kept with your database backups "+
			"because it is never stored in the database",
			"external_credential_key", externalSource)
	}
}

// setupSMTPVerifier proves the mail configuration entered during setup actually works.
// It translates the mail package's classified outcomes into the identity package's, so the
// transport details stay in one place and the operator-facing wording stays in the other.
type setupSMTPVerifier struct{}

func (setupSMTPVerifier) VerifySMTP(ctx context.Context, config identity.SMTPSetupConfiguration) error {
	err := mail.VerifySMTP(ctx, mail.SMTPConfig{
		Host: config.Host, Port: config.Port, From: config.From,
		Username: config.Username, Password: config.Password,
	})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, mail.ErrVerifyAuth):
		return identity.ErrSMTPAuthRejected
	case errors.Is(err, mail.ErrVerifySender):
		return identity.ErrSMTPSenderRejected
	case errors.Is(err, mail.ErrVerifyTLS):
		return identity.ErrSMTPTLSFailed
	default:
		return identity.ErrSMTPUnreachable
	}
}

func newSetupCredentialValidator(requestTimeout time.Duration) (*eodhd.CredentialValidator, error) {
	timeout := requestTimeout
	if timeout <= 0 || timeout > 10*time.Second {
		timeout = 10 * time.Second
	}
	return eodhd.NewCredentialValidator(eodhd.CredentialValidatorConfig{
		HTTPClient: &http.Client{Timeout: timeout}, Timeout: timeout, Now: time.Now,
	})
}

// validateExternalCredentialConfiguration reports whether provider credentials are stored and
// refuses the start when they exist but cannot be read.
// newCredentialCipher returns nil when no credential key is configured. That is a supported
// state: a deployment given only DATABASE_URL must start and serve sign-in, and the
// operations that actually need the key refuse individually, naming it.
func newCredentialCipher(externalConfig config.ExternalCredentialConfig) (*credentials.Cipher, error) {
	if !externalConfig.Configured {
		return nil, nil
	}
	return credentials.NewCipher(externalConfig.Key, externalConfig.KeyVersion)
}

func validateExternalCredentialConfiguration(ctx context.Context, externalConfig config.ExternalCredentialConfig,
	pool *pgxpool.Pool) (bool, error) {
	repository := credentials.NewRepository(pool)
	stored, err := repository.StoredCredentialsExist(ctx)
	if err != nil {
		return false, err
	}
	if !externalConfig.Configured {
		if stored {
			// Naming what is missing, and what it protects, without describing the
			// ciphertext: a restore that would lose provider credentials must fail loudly
			// rather than come up and discover it later.
			return stored, errors.New("EXTERNAL_CREDENTIAL_KEY is required because encrypted " +
				"provider credentials are stored; without it the stored provider API key and " +
				"mail password cannot be read. Keep it with your database backups")
		}
		return stored, nil
	}
	cipher, err := credentials.NewCipher(externalConfig.Key, externalConfig.KeyVersion)
	if err != nil {
		return stored, err
	}
	return stored, repository.ValidateConfiguration(ctx, cipher)
}

func newAuthenticationService(authConfig config.AuthConfig, signingKey []byte,
	externalConfig config.ExternalCredentialConfig,
	pool *pgxpool.Pool, logger *slog.Logger) (*auth.Service, error) {
	passwords, err := auth.NewPasswordHasher(rand.Reader, auth.DefaultArgon2Params())
	if err != nil {
		return nil, err
	}
	secrets, err := auth.NewSecrets(signingKey, rand.Reader)
	if err != nil {
		return nil, err
	}
	credentialCipher, err := newCredentialCipher(externalConfig)
	if err != nil {
		return nil, err
	}
	return auth.NewService(auth.ServiceDependencies{
		Repository:             auth.NewRepository(pool),
		Passwords:              passwords,
		Secrets:                secrets,
		Now:                    time.Now,
		OwnerIdleTimeout:       authConfig.OwnerIdleTimeout,
		SessionAbsoluteTimeout: authConfig.SessionAbsoluteTimeout,
		MemberCodes:            auth.NewMemberCodeGenerator(rand.Reader),
		Mail:                   newStoredSMTPSender(credentials.NewRepository(pool), credentialCipher, logger),
		Logger:                 logger,
	})
}

// storedSMTPSender resolves the encrypted SMTP configuration for every message, so credentials
// updated after start-up take effect without a restart and are never held in memory between
// deliveries.
type storedSMTPSender struct {
	repository *credentials.Repository
	cipher     *credentials.Cipher
	logger     *slog.Logger
}

func newStoredSMTPSender(repository *credentials.Repository, cipher *credentials.Cipher, logger *slog.Logger) *storedSMTPSender {
	return &storedSMTPSender{repository: repository, cipher: cipher, logger: logger}
}

func (sender *storedSMTPSender) Send(ctx context.Context, message mail.Message) error {
	if sender.cipher == nil {
		// No credential key means the stored SMTP password cannot be read. Mail is an
		// integration, so the application stays usable and delivery reports a retryable
		// failure rather than bringing the process down.
		return &mail.DeliveryError{Code: "temporary_failure", Retryable: true}
	}
	settings, err := sender.repository.SMTPSettings(ctx, sender.cipher)
	if err != nil {
		return &mail.DeliveryError{Code: "temporary_failure", Retryable: true}
	}
	transport, err := mail.NewSMTPSender(mail.SMTPConfig{
		Host: settings.Host, Port: settings.Port, From: settings.From,
		Username: settings.Username, Password: settings.Password,
	}, sender.logger)
	if err != nil {
		return &mail.DeliveryError{Code: "permanent_failure", Retryable: false}
	}
	return transport.Send(ctx, message)
}

func executeMarketDataRetry(ctx context.Context, command marketDataCommand, retrier marketDataRetrier,
	output io.Writer, appVersion string, maxRetries, workers int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	run, err := retrier.Retry(ctx, marketdata.RetryRequest{
		ParentRunID: command.RunID, AppVersion: appVersion, MaxRetries: maxRetries, Workers: workers,
	})
	if err != nil {
		return err
	}
	return writeImportTotals(output, run)
}

func parseMarketDataCommand(args []string, now time.Time) (marketDataCommand, error) {
	if len(args) < 2 || args[0] != "marketdata" {
		return marketDataCommand{}, errors.New("expected marketdata backfill, marketdata update, or marketdata retry")
	}
	if args[1] == "retry" {
		flags := flag.NewFlagSet("marketdata retry", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		var rawRunID string
		flags.StringVar(&rawRunID, "run", "", "failed or partial parent run ID")
		if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 {
			return marketDataCommand{}, errors.New("retry run ID is invalid")
		}
		runID, err := instruments.ParseUUID(rawRunID)
		if err != nil {
			return marketDataCommand{}, errors.New("retry run ID is invalid")
		}
		return marketDataCommand{Kind: marketdata.ImportRetry, RunID: runID}, nil
	}
	command := marketDataCommand{Universe: "nordic-liquid-v1"}
	flags := flag.NewFlagSet("marketdata "+args[1], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&command.Universe, "universe", command.Universe, "research universe code")
	to, err := marketdata.ParseSessionDate(now.UTC().Format("2006-01-02"))
	if err != nil {
		return marketDataCommand{}, err
	}
	command.To = to
	switch args[1] {
	case "backfill":
		years := flags.Int("years", 10, "inclusive number of years to request")
		if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 || *years < 1 || *years > 30 {
			return marketDataCommand{}, errors.New("backfill years must be between 1 and 30")
		}
		command.Kind = marketdata.ImportBackfill
		command.From, err = marketdata.ParseSessionDate(now.UTC().AddDate(-*years, 0, 0).Format("2006-01-02"))
	case "update":
		days := flags.Int("days", 7, "inclusive number of calendar days to request")
		if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 || *days < 1 || *days > 31 {
			return marketDataCommand{}, errors.New("update days must be between 1 and 31")
		}
		command.Kind = marketdata.ImportDailyUpdate
		command.From, err = marketdata.ParseSessionDate(now.UTC().AddDate(0, 0, -(*days - 1)).Format("2006-01-02"))
	default:
		return marketDataCommand{}, errors.New("expected marketdata backfill, marketdata update, or marketdata retry")
	}
	if err != nil || command.Universe == "" {
		return marketDataCommand{}, errors.New("market-data command scope is invalid")
	}
	return command, nil
}

func executeMarketDataCommand(ctx context.Context, command marketDataCommand, targets []marketdata.ImportTarget,
	importer marketDataImporter, output io.Writer, provider, appVersion string, maxRetries, workers int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for index := range targets {
		targets[index].From = command.From
		targets[index].To = command.To
	}
	run, err := importer.Import(ctx, marketdata.ImportRequest{
		Kind: command.Kind, Provider: provider, AppVersion: appVersion, Targets: targets,
		MaxRetries: maxRetries, Workers: workers,
	})
	if err != nil {
		return err
	}
	return writeImportTotals(output, run)
}

func writeImportTotals(output io.Writer, run marketdata.ImportRun) error {
	_, err := fmt.Fprintf(output, "run_id=%s status=%s processed=%d accepted=%d rejected=%d flagged=%d\n",
		run.ID, run.Status, run.Counts.Processed, run.Counts.Accepted, run.Counts.Rejected, run.Counts.Flagged)
	return err
}

func main() {
	if err := run(); err != nil {
		slog.Error("market lens stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	configureLogging(cfg.IsProduction())
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}
	credentialsStored, err := validateExternalCredentialConfiguration(ctx, cfg.ExternalCredentials, pool)
	if err != nil {
		return err
	}
	signingKey, err := resolveInstanceSigningKey(ctx, cfg.Auth, pool)
	if err != nil {
		return err
	}
	logInstanceConfiguration(slog.Default(), signingKey, cfg.ExternalCredentials, credentialsStored)
	if len(os.Args) > 1 {
		if os.Args[1] == "auth" {
			validator, err := newSetupCredentialValidator(cfg.MarketData.RequestTimeout)
			if err != nil {
				return err
			}
			identityService, err := newIdentityService(cfg.Auth, signingKey.Key, cfg.ExternalCredentials, validator,
				setupSMTPVerifier{}, pool, slog.Default())
			if err != nil {
				return err
			}
			authenticationService, err := newAuthenticationService(cfg.Auth, signingKey.Key, cfg.ExternalCredentials, pool, slog.Default())
			if err != nil {
				return err
			}
			return executeAuthCommand(ctx, os.Args[1:], identityService, authenticationService,
				credentials.NewRepository(pool), auth.NewRepository(pool), cfg.ExternalCredentials,
				osPasswordTerminal{input: os.Stdin, output: os.Stderr}, os.Stdout, slog.Default())
		}
		if os.Args[1] != "marketdata" {
			return errors.New("unknown command")
		}
		command, err := parseMarketDataCommand(os.Args[1:], time.Now())
		if err != nil {
			return err
		}
		if cfg.MarketData.Provider != "eodhd" {
			return errors.New("configured market-data provider is not supported")
		}
		provider, err := eodhd.New(eodhd.Config{
			APIToken:   cfg.MarketData.APIToken,
			HTTPClient: &http.Client{Timeout: cfg.MarketData.RequestTimeout},
		})
		if err != nil {
			return err
		}
		repository := marketdata.NewRepository(pool)
		service := marketdata.NewImportService(repository, provider)
		if command.Kind == marketdata.ImportRetry {
			return executeMarketDataRetry(ctx, command, service, os.Stdout, version,
				cfg.MarketData.MaxRetries, cfg.MarketData.Workers)
		}
		targets, err := repository.TargetsForUniverse(ctx, cfg.MarketData.Provider, command.Universe)
		if err != nil {
			return err
		}
		return executeMarketDataCommand(ctx, command, targets, service, os.Stdout,
			cfg.MarketData.Provider, version, cfg.MarketData.MaxRetries, cfg.MarketData.Workers)
	}

	var scheduleErr <-chan error
	if cfg.MarketData.ScheduleEnabled {
		if cfg.MarketData.Provider != "eodhd" {
			return errors.New("configured market-data provider is not supported")
		}
		provider, err := eodhd.New(eodhd.Config{
			APIToken:   cfg.MarketData.APIToken,
			HTTPClient: &http.Client{Timeout: cfg.MarketData.RequestTimeout},
		})
		if err != nil {
			return err
		}
		repository := marketdata.NewRepository(pool)
		service := marketdata.NewImportService(repository, provider)
		job, err := scheduler.NewMarketData(scheduler.MarketDataConfig{
			Enabled: true, Hour: cfg.MarketData.DailyHour, Minute: cfg.MarketData.DailyMinute,
			Location: cfg.MarketData.DailyLocation, Provider: cfg.MarketData.Provider,
			Universe: "nordic-liquid-v1", AppVersion: version,
			MaxRetries: cfg.MarketData.MaxRetries, Workers: cfg.MarketData.Workers,
		}, repository, service)
		if err != nil {
			return err
		}
		jobErrors := make(chan error, 1)
		scheduleErr = jobErrors
		go func() { jobErrors <- job.Run(ctx) }()
	}

	validator, err := newSetupCredentialValidator(cfg.MarketData.RequestTimeout)
	if err != nil {
		return err
	}
	identityService, err := newIdentityService(cfg.Auth, signingKey.Key, cfg.ExternalCredentials, validator,
		setupSMTPVerifier{}, pool, slog.Default())
	if err != nil {
		return err
	}
	authenticationService, err := newAuthenticationService(cfg.Auth, signingKey.Key, cfg.ExternalCredentials, pool, slog.Default())
	if err != nil {
		return err
	}
	handler := api.NewRouter(api.Dependencies{
		Database: pool, AllowedOrigins: cfg.AllowedOrigins, StaticDir: cfg.StaticDir, Version: version,
		Authenticator: authenticationService, Identity: identityService, Authentication: authenticationService,
		Integrations:     credentials.NewRepository(pool),
		IntegrationAdmin: identityService,
		InstanceConfiguration: api.InstanceConfiguration{
			SigningKeySource:      string(signingKey.Source),
			SigningKeyGeneration:  signingKey.Generation,
			ExternalKeyConfigured: cfg.ExternalCredentials.Configured,
		},
		MemberAuth:    authenticationService,
		Members:       identityService,
		Invitations:   identityService,
		SecureCookies: cfg.Auth.SecureCookies,
		MarketData:    marketdata.NewRepository(pool),
		Instruments:   instruments.NewQueryService(instruments.NewRepository(pool), marketdata.NewRepository(pool)),
		Events:        clientevents.NewService(clientevents.NewRepository(pool)),
		// An open stream re-reads its session and account on a bound tighter than the product's
		// five-second promise, so a revocation or deactivation ends it without a reconnect.
		EventRevalidator:        authenticationService,
		EventRevalidateInterval: 2 * time.Second,
	})
	server := &http.Server{
		Addr: ":" + cfg.Port, Handler: handler,
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second,
	}
	serverErr := make(chan error, 1)
	go func() {
		slog.Info("market lens starting", "address", server.Addr, "environment", cfg.Environment, "version", version)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		slog.Info("shutting down")
		return server.Shutdown(shutdownCtx)
	case err := <-scheduleErr:
		if err == nil {
			return nil
		}
		return err
	}
}

func configureLogging(production bool) {
	options := &slog.HandlerOptions{Level: slog.LevelInfo}
	if production {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, options)))
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, options)))
	}
}
