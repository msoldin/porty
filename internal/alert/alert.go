package alert

import (
	"context"
	"strings"
	"unicode/utf8"
)

// Service publishes only after the store has committed the lifecycle change.
type Service struct {
	store     Store
	publisher Publisher
}

func NewService(store Store, publisher Publisher) *Service {
	return &Service{store: store, publisher: publisher}
}
func (s *Service) List(ctx context.Context, filter Filter) (Page, error) {
	return s.store.List(ctx, filter)
}
func (s *Service) Get(ctx context.Context, id string) (Alert, error) { return s.store.Get(ctx, id) }
func (s *Service) History(ctx context.Context, id string, limit, offset int) ([]Event, error) {
	return s.store.History(ctx, id, limit, offset)
}
func (s *Service) Acknowledge(ctx context.Context, m Mutation) (Alert, error) {
	a, err := s.store.Acknowledge(ctx, m)
	if err == nil && s.publisher != nil {
		s.publisher.PublishAlert(a)
	}
	return a, err
}
func (s *Service) Resolve(ctx context.Context, m Mutation) (Alert, error) {
	a, err := s.store.Resolve(ctx, m)
	if err == nil && s.publisher != nil {
		s.publisher.PublishAlert(a)
	}
	return a, err
}

func ValidateChange(c Change) error {
	if c.Kind != "failure" && c.Kind != "recovery" {
		return ErrInvalid
	}
	if strings.TrimSpace(c.Key.StackID) == "" || strings.TrimSpace(c.Key.Problem) == "" || strings.TrimSpace(c.Key.Target) == "" || c.OccurrenceID == "" || c.ObservedAt.IsZero() {
		return ErrInvalid
	}
	if len(c.Summary) > 4096 || !utf8.ValidString(c.Summary) || len(c.OccurrenceID) > 512 || len(c.Key.Target) > 512 || len(c.StackName) > 512 {
		return ErrInvalid
	}
	if c.Kind == "recovery" && c.ExpectedRevision < 1 {
		return ErrInvalid
	}
	return nil
}

func ValidateMutation(m Mutation) error {
	if m.ID == "" || m.ActorID == "" || m.ExpectedRevision < 1 || len(m.Note) > 1024 || !utf8.ValidString(m.Note) {
		return ErrInvalid
	}
	return nil
}

func NormalizeFilter(f Filter) (Filter, error) {
	if f.View == "" {
		f.View = "attention"
	}
	switch f.View {
	case "attention", "open", "unacknowledged", "all":
	default:
		return f, ErrInvalid
	}
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 200 {
		f.Limit = 200
	}
	if f.Offset < 0 {
		return f, ErrInvalid
	}
	return f, nil
}
