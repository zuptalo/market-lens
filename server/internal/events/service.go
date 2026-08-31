package events

import "context"

type AuthorizedReader interface {
	Head(context.Context) (int64, error)
	Audience(context.Context, string) (Audience, error)
	ListAuthorized(context.Context, Audience, int64, int) ([]Event, error)
}

type Service struct {
	reader AuthorizedReader
}

func NewService(reader AuthorizedReader) *Service {
	return &Service{reader: reader}
}

// Audience re-reads who a caller is from durable state, so a stream never keeps an authority
// that the account no longer has.
func (service *Service) Audience(ctx context.Context, userID string) (Audience, error) {
	return service.reader.Audience(ctx, userID)
}

// Head is the identifier of the most recent event, which is where a new subscriber starts.
func (service *Service) Head(ctx context.Context) (int64, error) {
	return service.reader.Head(ctx)
}

func (service *Service) ListAuthorized(ctx context.Context, audience Audience, after int64, limit int) ([]Event, error) {
	return service.reader.ListAuthorized(ctx, audience, after, limit)
}
