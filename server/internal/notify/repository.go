package notify

import (
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository holds the owner-scoped queries. Every predicate reads the row's own user_id.
type Repository struct {
	pool *pgxpool.Pool
	// signer is the instance signing key, narrowed to one operation: signing the link that stops a
	// kind of message. Narrowed rather than passed whole so nothing here can sign anything else.
	signer Signer
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// WithSigner returns the repository able to sign unsubscribe links. Without one, email still sends
// and the link simply cannot be built — which is a failure worth reporting rather than hiding.
func (r *Repository) WithSigner(signer Signer) *Repository {
	r.signer = signer
	return r
}

func (r *Repository) ready() error {
	if r == nil || r.pool == nil {
		return errors.New("notification repository is not configured")
	}
	return nil
}
