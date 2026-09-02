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
	"sort"
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
	"market-lens/server/internal/features"
	"market-lens/server/internal/identity"
	"market-lens/server/internal/instruments"
	"market-lens/server/internal/mail"
	"market-lens/server/internal/marketdata"
	"market-lens/server/internal/marketdata/eodhd"
	"market-lens/server/internal/scheduler"
	"market-lens/server/internal/strategies"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/term"
)

var version = "dev"

// The identity service is what serves owner integration administration. Stating it here means
// a signature drift is a build failure rather than a nil dependency discovered in a browser.
var _ api.IntegrationAdministration = (*identity.Service)(nil)

// marketDataResolve is not an import kind: resolve reads the provider's catalog and reports,
// changing nothing. It shares this struct because it takes the same universe flag, and it is
// given a value no import kind uses so the two can never be confused.
const marketDataResolve marketdata.ImportKind = "resolve"

type marketDataCommand struct {
	Kind     marketdata.ImportKind
	Universe string
	From     marketdata.SessionDate
	To       marketdata.SessionDate
	RunID    instruments.UUID
	// Search asks resolve to print the provider's own catalog rows matching a term instead of
	// auditing what is stored. It is how a suspected replacement symbol is confirmed before a
	// migration is written against it.
	Search string
}

type marketDataImporter interface {
	Import(context.Context, marketdata.ImportRequest) (marketdata.ImportRun, error)
}

// featurePass recomputes the features an import run's bars take part in. The scheduler and
// the marketdata commands share it, so a manual backfill leaves the engine as current as the
// nightly update does.
type featurePass struct {
	service    *features.Service
	universe   string
	appVersion string
	workers    int
}

func (p featurePass) ComputeSinceRun(ctx context.Context, runID features.UUID) error {
	_, err := p.service.Compute(ctx, features.ComputeRequest{
		Kind: features.RunKindIncremental, Universe: p.universe, SinceRun: runID,
		Workers: p.workers, AppVersion: p.appVersion,
	})
	return err
}

// featureTriggeringImporter runs the pass after each successful import. A pass that fails is
// logged and swallowed: the bars are stored and the next pass picks the work up, so a feature
// defect must never turn a good import into a failed command.
type featureTriggeringImporter struct {
	importer marketDataImporter
	pass     scheduler.FeatureComputer
}

func (i featureTriggeringImporter) Import(ctx context.Context, request marketdata.ImportRequest) (marketdata.ImportRun, error) {
	run, err := i.importer.Import(ctx, request)
	if err != nil {
		return run, err
	}
	if err := i.pass.ComputeSinceRun(ctx, run.ID); err != nil {
		slog.Default().Error("feature computation after import failed", "import_run_id", run.ID, "error", err)
	}
	return run, nil
}

// featuresCommand mirrors marketDataCommand: one run of the feature engine over a universe.
type featuresCommand struct {
	Kind     features.RunKind
	Universe string
	// SinceRun names the import run an incremental pass follows; Definition names the one
	// definition a definition pass recomputes. At most one of them is set.
	SinceRun   features.UUID
	Definition string
}

type featureComputer interface {
	Compute(context.Context, features.ComputeRequest) (features.Run, error)
}

const featuresUsage = "expected features compute [--universe CODE] [--since-run IMPORT_RUN_ID | --definition NAME]"

func parseFeaturesCommand(args []string) (featuresCommand, error) {
	if len(args) < 2 || args[0] != "features" || args[1] != "compute" {
		return featuresCommand{}, errors.New(featuresUsage)
	}
	command := featuresCommand{Kind: features.RunKindFull, Universe: "nordic-liquid-v1"}
	flags := flag.NewFlagSet("features compute", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&command.Universe, "universe", command.Universe, "research universe code")
	var sinceRun, definition string
	flags.StringVar(&sinceRun, "since-run", "", "recompute only what the bars of this import run take part in")
	flags.StringVar(&definition, "definition", "", "recompute one definition over every member's history")
	if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 || strings.TrimSpace(command.Universe) == "" {
		return featuresCommand{}, errors.New(featuresUsage)
	}
	sinceRunGiven, definitionGiven := isFlagSet(flags, "since-run"), isFlagSet(flags, "definition")
	if sinceRunGiven && definitionGiven {
		return featuresCommand{}, errors.New(featuresUsage)
	}
	switch {
	case sinceRunGiven:
		parsed, err := instruments.ParseUUID(strings.TrimSpace(sinceRun))
		if err != nil {
			return featuresCommand{}, errors.New(featuresUsage)
		}
		command.Kind, command.SinceRun = features.RunKindIncremental, features.UUID(parsed)
	case definitionGiven:
		if strings.TrimSpace(definition) == "" {
			return featuresCommand{}, errors.New(featuresUsage)
		}
		command.Kind, command.Definition = features.RunKindDefinition, strings.TrimSpace(definition)
	}
	return command, nil
}

// isFlagSet reports whether the caller passed a flag, so an empty value is a usage error
// rather than a silently ignored one.
func isFlagSet(flags *flag.FlagSet, name string) bool {
	given := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == name {
			given = true
		}
	})
	return given
}

func executeFeaturesCommand(ctx context.Context, command featuresCommand, computer featureComputer,
	output io.Writer, appVersion string, workers int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	run, err := computer.Compute(ctx, features.ComputeRequest{
		Kind: command.Kind, Universe: command.Universe, Workers: workers, AppVersion: appVersion,
		SinceRun: command.SinceRun, Definition: command.Definition,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "run_id=%s status=%s instruments=%d values=%d\n",
		run.ID, run.Status, run.InstrumentCount, run.ValueCount)
	return err
}

// newSignalPass builds the strategy layer for one universe. It exists so the command line, the
// scheduler and the import path all trigger the same computation rather than three variations of
// it — the product's position is that strategy behaviour has exactly one implementation.
func newSignalPass(pool *pgxpool.Pool, universe, appVersion string, workers int) signalPass {
	return signalPass{
		service: strategies.NewService(strategies.NewRepository(pool),
			features.NewRepository(pool), slog.Default()),
		universe: universe, appVersion: appVersion, workers: workers,
	}
}

// signalPass scores the sessions a feature run wrote. It is what the feature engine asks when a
// run commits, seen from the engine as features.SignalComputer.
type signalPass struct {
	service    *strategies.Service
	universe   string
	appVersion string
	workers    int
}

func (p signalPass) ComputeSinceFeatureRun(ctx context.Context, runID features.UUID) error {
	_, err := p.service.Compute(ctx, strategies.ComputeRequest{
		Kind: strategies.RunKindIncremental, Universe: p.universe, SinceFeatureRun: strategies.UUID(runID),
		Workers: p.workers, AppVersion: p.appVersion,
	})
	return err
}

// signalsCommand is one strategy computation over a universe, asked for by an owner.
type signalsCommand struct {
	Kind     strategies.RunKind
	Universe string
	// SinceFeatureRun names the feature run an incremental pass follows; Strategy and Version
	// name the one published version a strategy pass recomputes. At most one of them is set.
	SinceFeatureRun strategies.UUID
	Strategy        string
	Version         int
}

type signalComputer interface {
	Compute(context.Context, strategies.ComputeRequest) (strategies.Run, error)
}

const signalsUsage = "expected signals compute [--universe CODE] [--since-feature-run FEATURE_RUN_ID | --strategy NAME [--version N]]"

func parseSignalsCommand(args []string) (signalsCommand, error) {
	if len(args) < 2 || args[0] != "signals" || args[1] != "compute" {
		return signalsCommand{}, errors.New(signalsUsage)
	}
	command := signalsCommand{Kind: strategies.RunKindFull, Universe: "nordic-liquid-v1"}
	flags := flag.NewFlagSet("signals compute", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&command.Universe, "universe", command.Universe, "research universe code")
	var sinceFeatureRun, strategy string
	var version int
	flags.StringVar(&sinceFeatureRun, "since-feature-run", "", "score only the sessions this feature run wrote")
	flags.StringVar(&strategy, "strategy", "", "recompute one published strategy over the whole history")
	flags.IntVar(&version, "version", 0, "a specific strategy version, with --strategy")
	if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 || strings.TrimSpace(command.Universe) == "" {
		return signalsCommand{}, errors.New(signalsUsage)
	}
	sinceGiven, strategyGiven := isFlagSet(flags, "since-feature-run"), isFlagSet(flags, "strategy")
	if sinceGiven && strategyGiven {
		return signalsCommand{}, errors.New(signalsUsage)
	}
	if isFlagSet(flags, "version") && version < 1 {
		return signalsCommand{}, errors.New(signalsUsage)
	}
	switch {
	case sinceGiven:
		parsed, err := instruments.ParseUUID(strings.TrimSpace(sinceFeatureRun))
		if err != nil {
			return signalsCommand{}, errors.New(signalsUsage)
		}
		command.Kind, command.SinceFeatureRun = strategies.RunKindIncremental, strategies.UUID(parsed)
	case strategyGiven:
		if strings.TrimSpace(strategy) == "" {
			return signalsCommand{}, errors.New(signalsUsage)
		}
		command.Kind, command.Strategy, command.Version = strategies.RunKindStrategy, strings.TrimSpace(strategy), version
	}
	return command, nil
}

func executeSignalsCommand(ctx context.Context, command signalsCommand, computer signalComputer,
	output io.Writer, appVersion string, workers int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	run, err := computer.Compute(ctx, strategies.ComputeRequest{
		Kind: command.Kind, Universe: command.Universe, Workers: workers, AppVersion: appVersion,
		SinceFeatureRun: command.SinceFeatureRun, Strategy: command.Strategy, Version: command.Version,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "run_id=%s status=%s instruments=%d signals=%d failed=%d\n",
		run.ID, run.Status, run.InstrumentCount, run.SignalCount, run.FailedCount)
	return err
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
// marketDataTokenSource resolves the EODHD API token when a request needs it.
//
// The key an owner configures and verifies through the settings screen is stored encrypted in
// the database, and until now nothing fed it to the importer: the client was built from
// EODHD_API_TOKEN, which a self-hosted deployment has no reason to set. An installation could
// therefore show its market-data credential as stored and validated and still import nothing.
//
// The environment variable remains as a fallback so a development machine can point at a key
// without a database round trip, but the stored credential wins, because that is the one the
// owner can actually see and change.
func marketDataTokenSource(pool *pgxpool.Pool, externalConfig config.ExternalCredentialConfig,
	envToken string) func(context.Context) (string, error) {
	repository := credentials.NewRepository(pool)
	return func(ctx context.Context) (string, error) {
		cipher, err := newCredentialCipher(externalConfig)
		if err == nil && cipher != nil {
			if key, keyErr := repository.EODHDAPIKey(ctx, cipher); keyErr == nil {
				return key, nil
			}
		}
		if strings.TrimSpace(envToken) != "" {
			return envToken, nil
		}
		return "", errors.New("no market-data API key is configured")
	}
}

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

// reportSymbolAudit writes what the provider's catalog says about the symbols this
// installation stores.
//
// It reports and changes nothing. Correcting a stale symbol means correcting seeded reference
// data, which is an ordered migration, and a migration must not be written from a guess — so
// the job here is to turn a guess into a fact.
//
// Correct instruments are counted rather than listed. A hundred lines saying "fine" would
// bury the two that are not.
func reportSymbolAudit(output io.Writer, universe []marketdata.UniverseEntry,
	catalog map[string][]marketdata.CatalogEntry) error {
	findings := marketdata.AuditProviderSymbols(universe, catalog)
	counts := map[marketdata.SymbolState]int{}
	for _, finding := range findings {
		counts[finding.State]++
	}

	for _, finding := range findings {
		if finding.State == marketdata.SymbolOK {
			continue
		}
		line := fmt.Sprintf("%s %s stored=%s state=%s",
			finding.Entry.MIC, finding.Entry.Ticker, finding.Entry.ProviderSymbol, finding.State)
		if finding.Suggested != "" {
			// A suggestion says where it came from. An ISIN match is near-certain and a name
			// match is only a lead, and acting on the second as though it were the first is how
			// one company's prices end up under another company's record.
			line += fmt.Sprintf(" suggested=%s matched_on=%s", finding.Suggested, finding.MatchedOn)
		}
		if finding.CatalogName != "" {
			line += fmt.Sprintf(" provider_name=%q", finding.CatalogName)
		}
		if finding.Entry.ISIN != "" {
			line += fmt.Sprintf(" stored_isin=%s", finding.Entry.ISIN)
		}
		if finding.CatalogISIN != "" && !strings.EqualFold(finding.CatalogISIN, finding.Entry.ISIN) {
			line += fmt.Sprintf(" provider_isin=%s", finding.CatalogISIN)
		}
		// How much history is stored is the evidence for leaving an uncatalogued symbol alone,
		// so it belongs on the line rather than in a separate query the reader has to think to run.
		if finding.State == marketdata.SymbolUncatalogued || finding.State == marketdata.SymbolStale {
			line += fmt.Sprintf(" stored_bars=%d last_session=%s",
				finding.Entry.StoredBars, valueOrDash(finding.Entry.LastSession))
		}
		if _, err := fmt.Fprintln(output, line); err != nil {
			return err
		}
	}

	_, err := fmt.Fprintf(output,
		"checked=%d ok=%d renamed=%d absent=%d stale=%d uncatalogued=%d "+
			"mismatched=%d unverified=%d unchecked=%d\n",
		len(findings), counts[marketdata.SymbolOK], counts[marketdata.SymbolRenamed],
		counts[marketdata.SymbolAbsent], counts[marketdata.SymbolStale],
		counts[marketdata.SymbolUncatalogued], counts[marketdata.SymbolMismatched],
		counts[marketdata.SymbolUnverified], counts[marketdata.SymbolUnchecked])
	return err
}

// reportCatalogSearch prints the provider's own rows matching a term, across every exchange
// the universe covers.
//
// The audit can only match on identifiers this installation already stores, so a company that
// changed its ticker and its name at once falls out of every lookup it makes. This is the way
// back in: search the catalog for whatever is known — a fragment of the new name, the
// unchanged ISIN — and read the provider's answer directly, rather than writing a migration
// against a guess about it.
func reportCatalogSearch(output io.Writer, term string,
	catalog map[string][]marketdata.CatalogEntry) error {
	needle := strings.ToLower(strings.TrimSpace(term))
	mics := make([]string, 0, len(catalog))
	for mic := range catalog {
		mics = append(mics, mic)
	}
	sort.Strings(mics)

	matches := 0
	for _, mic := range mics {
		for _, row := range catalog[mic] {
			if !strings.Contains(strings.ToLower(row.ProviderSymbol), needle) &&
				!strings.Contains(strings.ToLower(row.Name), needle) &&
				!strings.Contains(strings.ToLower(row.ISIN), needle) {
				continue
			}
			matches++
			if _, err := fmt.Fprintf(output, "%s %s isin=%s name=%q currency=%s\n",
				mic, row.ProviderSymbol, valueOrDash(row.ISIN), row.Name,
				valueOrDash(row.Currency)); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintf(output, "matches=%d\n", matches)
	return err
}

// valueOrDash keeps a blank provider field visible. An empty ISIN is itself the finding when a
// symbol that plainly exists cannot be matched by identifier.
func valueOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
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
		return marketDataCommand{}, errors.New(
			"expected marketdata backfill, marketdata update, marketdata retry, or marketdata resolve")
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
	case "resolve":
		search := flags.String("search", "", "print provider catalog rows matching this term")
		if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 ||
			strings.TrimSpace(command.Universe) == "" {
			return marketDataCommand{}, errors.New("resolve takes only a universe and an optional search term")
		}
		command.Search = strings.TrimSpace(*search)
		command.Kind = marketDataResolve
		return command, nil
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
		// Reach back past the requested floor when the instrument still has a finding older
		// than it. The window is relative, so it walks forward a day at a time and would
		// otherwise leave anything below it permanently unexaminable — and a finding that
		// cannot be re-examined cannot be resolved.
		targets[index].From = marketdata.WidenToUnsettled(command.From, targets[index].EarliestUnsettled)
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
		if os.Args[1] == "features" {
			command, err := parseFeaturesCommand(os.Args[1:])
			if err != nil {
				return err
			}
			service := features.NewService(features.NewRepository(pool), slog.Default())
			service.Signals = newSignalPass(pool, command.Universe, version, cfg.MarketData.Workers)
			return executeFeaturesCommand(ctx, command, service, os.Stdout, version, cfg.MarketData.Workers)
		}
		if os.Args[1] == "signals" {
			command, err := parseSignalsCommand(os.Args[1:])
			if err != nil {
				return err
			}
			service := strategies.NewService(strategies.NewRepository(pool),
				features.NewRepository(pool), slog.Default())
			return executeSignalsCommand(ctx, command, service, os.Stdout, version, cfg.MarketData.Workers)
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
			TokenSource: marketDataTokenSource(pool, cfg.ExternalCredentials, cfg.MarketData.APIToken),
			HTTPClient:  &http.Client{Timeout: cfg.MarketData.RequestTimeout},
		})
		if err != nil {
			return err
		}
		repository := marketdata.NewRepository(pool)
		if command.Kind == marketDataResolve {
			entries, err := repository.UniverseEntries(ctx, cfg.MarketData.Provider, command.Universe)
			if err != nil {
				return err
			}
			// One catalog fetch per exchange, not per instrument: the whole universe is
			// audited with four requests.
			catalog := map[string][]marketdata.CatalogEntry{}
			for _, entry := range entries {
				if _, done := catalog[entry.MIC]; done {
					continue
				}
				listed, err := provider.ListInstruments(ctx, entry.MIC)
				if err != nil {
					// An exchange that cannot be read is reported as unchecked rather than
					// silently turning every one of its instruments into a finding.
					slog.Default().Warn("market-data catalog unavailable", "mic", entry.MIC,
						"reason", marketdata.NormalizeSafeError(marketdata.SanitizeError(err.Error())).Code)
					continue
				}
				catalog[entry.MIC] = listed
			}
			if command.Search != "" {
				return reportCatalogSearch(os.Stdout, command.Search, catalog)
			}
			return reportSymbolAudit(os.Stdout, entries, catalog)
		}
		service := marketdata.NewImportService(repository, provider)
		featureService := features.NewService(features.NewRepository(pool), slog.Default())
		featureService.Signals = newSignalPass(pool, command.Universe, version, cfg.MarketData.Workers)
		pass := featurePass{service: featureService,
			universe: command.Universe, appVersion: version, workers: cfg.MarketData.Workers}
		if command.Kind == marketdata.ImportRetry {
			return executeMarketDataRetry(ctx, command, service, os.Stdout, version,
				cfg.MarketData.MaxRetries, cfg.MarketData.Workers)
		}
		targets, err := repository.TargetsForUniverse(ctx, cfg.MarketData.Provider, command.Universe)
		if err != nil {
			return err
		}
		return executeMarketDataCommand(ctx, command, targets, featureTriggeringImporter{importer: service, pass: pass},
			os.Stdout, cfg.MarketData.Provider, version, cfg.MarketData.MaxRetries, cfg.MarketData.Workers)
	}

	var scheduleErr <-chan error
	if cfg.MarketData.ScheduleEnabled {
		if cfg.MarketData.Provider != "eodhd" {
			return errors.New("configured market-data provider is not supported")
		}
		provider, err := eodhd.New(eodhd.Config{
			TokenSource: marketDataTokenSource(pool, cfg.ExternalCredentials, cfg.MarketData.APIToken),
			HTTPClient:  &http.Client{Timeout: cfg.MarketData.RequestTimeout},
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
		scheduledFeatures := features.NewService(features.NewRepository(pool), slog.Default())
		scheduledFeatures.Signals = newSignalPass(pool, "nordic-liquid-v1", version, cfg.MarketData.Workers)
		job.Features = featurePass{service: scheduledFeatures,
			universe: "nordic-liquid-v1", appVersion: version, workers: cfg.MarketData.Workers}
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
		Features:      features.NewRepository(pool),
		Signals:       strategies.NewRepository(pool),
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
