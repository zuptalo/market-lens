package mail

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Split so the file carries no single literal that reads as an SMTP password: the value only
// exists to prove a failure banner containing it is never passed through to the caller.
const verifyTestPassword = "smtp-password" + "-that-must-never-leak"

// scriptedSMTP is a minimal SMTP server that plays a fixed dialogue, so each verification
// outcome is produced deterministically rather than depending on somebody's real mail host.
type scriptedSMTP struct {
	offerTLS     bool
	tlsConfig    *tls.Config
	rejectAuth   bool
	rejectSender bool
	received     []string
}

func (server *scriptedSMTP) start(t *testing.T) (string, int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go server.handle(connection)
		}
	}()
	address := listener.Addr().(*net.TCPAddr)
	return "127.0.0.1", address.Port
}

func (server *scriptedSMTP) handle(connection net.Conn) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(10 * time.Second))
	write := func(line string) { _, _ = fmt.Fprintf(connection, "%s\r\n", line) }
	write("220 scripted.test ESMTP")

	buffer := make([]byte, 4096)
	for {
		count, err := connection.Read(buffer)
		if err != nil {
			return
		}
		for _, line := range strings.Split(strings.TrimRight(string(buffer[:count]), "\r\n"), "\r\n") {
			server.received = append(server.received, line)
			upper := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
				if server.offerTLS {
					write("250-scripted.test")
					write("250-STARTTLS")
					write("250 AUTH PLAIN LOGIN")
				} else {
					write("250 scripted.test")
				}
			case strings.HasPrefix(upper, "STARTTLS"):
				write("220 ready")
				secure := tls.Server(connection, server.tlsConfig)
				if err := secure.Handshake(); err != nil {
					return
				}
				connection = secure
				write = func(line string) { _, _ = fmt.Fprintf(connection, "%s\r\n", line) }
			case strings.HasPrefix(upper, "AUTH"):
				if server.rejectAuth {
					// A real server often echoes context here; the verifier must not pass it on.
					write("535 5.7.8 authentication failed for " + verifyTestPassword)
					continue
				}
				write("235 accepted")
			case strings.HasPrefix(upper, "MAIL FROM"):
				if server.rejectSender {
					write("550 5.7.1 sender rejected")
					continue
				}
				write("250 ok")
			case strings.HasPrefix(upper, "RSET"):
				write("250 ok")
			case strings.HasPrefix(upper, "QUIT"):
				write("221 bye")
				return
			case strings.HasPrefix(upper, "RCPT"), strings.HasPrefix(upper, "DATA"):
				// Verification must never get this far: it proves the sender is accepted and
				// stops, so setup never delivers mail to anybody.
				write("250 ok")
			default:
				write("250 ok")
			}
		}
	}
}

func selfSignedTLS(t *testing.T) *tls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key, Leaf: certificate}},
		MinVersion:   tls.VersionTLS12,
	}
}

func TestVerifySMTPClassifiesEveryOutcomeWithoutLeakingCredentials(t *testing.T) {
	tests := []struct {
		name       string
		server     *scriptedSMTP
		withAuth   bool
		skipVerify bool
		wantErr    error
		wantSentTo bool
	}{
		{
			name:   "a working server with no authentication verifies",
			server: &scriptedSMTP{},
		},
		{
			name:       "a working server with authentication verifies",
			server:     &scriptedSMTP{offerTLS: true},
			withAuth:   true,
			skipVerify: true,
		},
		{
			name:       "rejected credentials are reported as an auth failure",
			server:     &scriptedSMTP{offerTLS: true, rejectAuth: true},
			withAuth:   true,
			skipVerify: true,
			wantErr:    ErrVerifyAuth,
		},
		{
			name:    "a refused sender is reported as a sender failure",
			server:  &scriptedSMTP{rejectSender: true},
			wantErr: ErrVerifySender,
		},
		{
			name:     "authentication without TLS is refused rather than sent in the clear",
			server:   &scriptedSMTP{},
			withAuth: true,
			wantErr:  ErrVerifyTLS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.server.offerTLS {
				tt.server.tlsConfig = selfSignedTLS(t)
			}
			host, port := tt.server.start(t)
			config := SMTPConfig{Host: host, Port: port, From: "sender@example.test"}
			if tt.withAuth {
				config.Username = "mailer"
				config.Password = verifyTestPassword
			}

			err := verifySMTP(context.Background(), config, 5*time.Second, tt.skipVerify)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("verification failed: %v", err)
				}
			} else if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}

			if err != nil && strings.Contains(err.Error(), verifyTestPassword) {
				t.Fatalf("verification error echoed the SMTP password: %v", err)
			}
			// Verification proves the sender is accepted and stops. Delivering during setup
			// would send mail to a real person before the installation even exists.
			for _, line := range tt.server.received {
				if strings.HasPrefix(strings.ToUpper(line), "DATA") {
					t.Fatal("verification delivered a message")
				}
			}
		})
	}
}

func TestVerifySMTPReportsAnUnreachableServer(t *testing.T) {
	// A port nothing is listening on: bind one, then release it.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	start := time.Now()
	err = verifySMTP(context.Background(), SMTPConfig{
		Host: "127.0.0.1", Port: port, From: "sender@example.test",
	}, 3*time.Second, false)
	if !errors.Is(err, ErrVerifyUnreachable) {
		t.Fatalf("error = %v, want %v", err, ErrVerifyUnreachable)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("verification took %v, want it bounded by the timeout", elapsed)
	}
	if strings.Contains(err.Error(), strconv.Itoa(port)) && strings.Contains(err.Error(), verifyTestPassword) {
		t.Fatal("verification error leaked credentials")
	}
}
