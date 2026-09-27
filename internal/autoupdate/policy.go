package autoupdate

import (
	"context"
	"database/sql"
	"errors"
	"github.com/msoldin/porty/internal/stack"
	"time"
)

type Eligibility struct {
	Available          bool              `json:"available"`
	AvailabilityReason string            `json:"availabilityReason,omitempty"`
	Eligible           bool              `json:"eligible"`
	EligibilityReason  string            `json:"eligibilityReason,omitempty"`
	Excluded           map[string]string `json:"excluded"`
}
type Status struct {
	Policy  Policy `json:"policy"`
	LastRun *Run   `json:"lastRun,omitempty"`
	Eligibility
}
type PolicyStore interface {
	Store
	LatestRun(context.Context, stack.StackID) (Run, error)
}
type PolicyValidator interface {
	InspectAutoUpdate(context.Context, stack.StackID) (Eligibility, error)
	EnableAutoUpdate(context.Context, stack.StackID) error
	ResumeAutoUpdate(context.Context, stack.StackID, int64, string) error
}
type PolicyService struct {
	store     PolicyStore
	validator PolicyValidator
	notify    func()
	now       func() time.Time
}

func NewPolicyService(store PolicyStore, validator PolicyValidator, notify func()) *PolicyService {
	return &PolicyService{store: store, validator: validator, notify: notify, now: time.Now}
}
func (s *PolicyService) Get(ctx context.Context, id stack.StackID) (Status, error) {
	policy, err := s.store.GetPolicy(ctx, id)
	if err != nil {
		return Status{}, err
	}
	eligibility, err := s.validator.InspectAutoUpdate(ctx, id)
	if err != nil {
		return Status{}, err
	}
	status := Status{Policy: policy, Eligibility: eligibility}
	last, err := s.store.LatestRun(ctx, id)
	if err == nil {
		status.LastRun = &last
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Status{}, err
	}
	return status, nil
}
func (s *PolicyService) Update(ctx context.Context, id stack.StackID, update PolicyUpdate, actor string) (Status, error) {
	if _, err := NextRun(update.Expression, s.now()); err != nil {
		return Status{}, err
	}
	if update.Enabled {
		if err := s.validator.EnableAutoUpdate(ctx, id); err != nil {
			return Status{}, err
		}
	}
	if _, err := s.store.SavePolicy(ctx, id, update, s.now()); err != nil {
		return Status{}, err
	}
	if s.notify != nil {
		s.notify()
	}
	return s.Get(ctx, id)
}
func (s *PolicyService) Resume(ctx context.Context, id stack.StackID, revision int64, actor string) (Status, error) {
	if err := s.validator.ResumeAutoUpdate(ctx, id, revision, actor); err != nil {
		return Status{}, err
	}
	if s.notify != nil {
		s.notify()
	}
	return s.Get(ctx, id)
}
