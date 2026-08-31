package events

import "context"

type AuthorizedReader interface {
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

func (service *Service) ListAuthorized(ctx context.Context, audience Audience, after int64, limit int) ([]Event, error) {
	return service.reader.ListAuthorized(ctx, audience, after, limit)
}
