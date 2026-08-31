package marketdata_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"market-lens/server/internal/api"
	"market-lens/server/internal/auth"
	"market-lens/server/internal/httpx"

	"github.com/jackc/pgx/v5/pgxpool"
)

func authenticatedAPIDependencies(dependencies api.Dependencies) api.Dependencies {
	dependencies.Authenticator = marketDataSessionAuthenticator(func(_ context.Context, token string) (auth.Principal, error) {
		if token != "market-data-test-session" {
			return auth.Principal{}, auth.ErrAuthenticationRequired
		}
		return auth.Principal{
			UserID: "10000000-0000-4000-8000-000000000001", Role: "owner",
			SessionID: "20000000-0000-4000-8000-000000000001", VerifyCSRF: func(string) bool { return true },
		}, nil
	})
	return dependencies
}

// seedMarketDataTestOwner persists the account the fixture session belongs to. A stream now
// resolves its audience from the durable user record, so the fixture needs one to exist.
func seedMarketDataTestOwner(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	at := time.Date(2026, time.August, 30, 8, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(context.Background(), `INSERT INTO users
		(id,email,normalized_email,display_name,role,status,email_verified_at,created_at,updated_at)
		VALUES ('10000000-0000-4000-8000-000000000001','owner@example.com','owner@example.com','Owner',
		'owner','active',$1,$1,$1) ON CONFLICT (id) DO NOTHING`, at); err != nil {
		t.Fatal(err)
	}
}

func addMarketDataTestSession(request *http.Request) {
	request.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: "market-data-test-session"})
}

type marketDataSessionAuthenticator func(context.Context, string) (auth.Principal, error)

func (function marketDataSessionAuthenticator) AuthenticateSession(ctx context.Context, token string) (auth.Principal, error) {
	return function(ctx, token)
}
