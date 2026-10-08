package control

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/msoldin/porty/internal/alert"
	"github.com/msoldin/porty/internal/compose"
	"github.com/msoldin/porty/internal/ondemand"
	op "github.com/msoldin/porty/internal/operation"
	"github.com/msoldin/porty/internal/stack"
)

type OnDemandStore interface {
	ondemand.ControllerStore
	SaveGroup(context.Context, stack.StackID, string, ondemand.PolicyUpdate, ondemand.Evidence, ondemand.Phase) (ondemand.Group, error)
	DeleteGroup(context.Context, stack.StackID, string, int64) error
	HoldGroup(context.Context, stack.StackID, string, int64, string) error
	ResumeGroup(context.Context, stack.StackID, string, int64, ondemand.Evidence, ondemand.Phase) (ondemand.Group, error)
	BeginTransition(context.Context, ondemand.Group, ondemand.Action, string) error
	RecoverInterrupted(context.Context) error
}
type OnDemandRuntime interface {
	CheckOnDemandHost(context.Context, string) error
	SnapshotOnDemand(context.Context, compose.Request, []string) (compose.OnDemandSnapshot, error)
	OnDemandCounters(context.Context, []string) (map[string]compose.OnDemandCounters, time.Time, error)
	StartOnDemand(context.Context, compose.OnDemandSnapshot, time.Duration) error
	StopOnDemand(context.Context, compose.OnDemandSnapshot, int) error
}
type OnDemandService struct {
	control     *ControlPlane
	store       OnDemandStore
	controller  *ondemand.Controller
	hostMu      sync.Mutex
	hostChecked map[stack.StackID]time.Time
}

func NewOnDemandService(control *ControlPlane, store OnDemandStore) *OnDemandService {
	s := &OnDemandService{control: control, store: store, hostChecked: map[stack.StackID]time.Time{}}
	s.controller = ondemand.NewController(store, s)
	control.onDemand = s
	return s
}

func (c *ControlPlane) releaseOnDemandHolds(ctx context.Context, request op.OperationRequest) error {
	if c.onDemand == nil || request.ScopeType != "stack" {
		return nil
	}
	groups, err := c.onDemand.store.ListGroups(ctx, stack.StackID(request.ScopeID))
	if err != nil {
		return err
	}
	for _, group := range groups {
		if group.HoldReason != "" || group.PausedReason != "" || !group.Enabled {
			c.onDemand.controller.SuspendGroup(group.ID)
		}
	}
	return nil
}
func (s *OnDemandService) Run(ctx context.Context) error {
	if err := s.store.RecoverInterrupted(ctx); err != nil {
		return err
	}
	return s.controller.Run(ctx)
}
func (s *OnDemandService) ListGroups(ctx context.Context, id stack.StackID) ([]ondemand.Group, error) {
	if _, err := s.control.lookup.ByID(ctx, id); err != nil {
		return nil, err
	}
	groups, err := s.store.ListGroups(ctx, id)
	if err != nil {
		return nil, err
	}
	for i := range groups {
		groups[i].ObservationReason = s.controller.ObservationReason(groups[i])
	}
	return groups, nil
}
func (s *OnDemandService) snapshot(ctx context.Context, g ondemand.Group, host bool) (compose.OnDemandSnapshot, error) {
	c := s.control
	runtime, ok := c.runtime.(OnDemandRuntime)
	if !ok || c.stateStore == nil {
		return compose.OnDemandSnapshot{}, ondemand.ErrUnavailable
	}
	target, err := c.lookup.ByID(ctx, g.StackID)
	if err != nil {
		return compose.OnDemandSnapshot{}, err
	}
	if target.ArchivedAt != nil {
		return compose.OnDemandSnapshot{}, ondemand.ErrUnavailable
	}
	if host {
		if err := runtime.CheckOnDemandHost(ctx, target.ComposeProjectName); err != nil {
			return compose.OnDemandSnapshot{}, err
		}
	}
	values, err := c.environment.Values(ctx, g.StackID)
	if err != nil {
		return compose.OnDemandSnapshot{}, err
	}
	snapshot, err := runtime.SnapshotOnDemand(ctx, compose.Request{StackDir: filepath.Join(c.root, target.DirectoryName), ProjectName: target.ComposeProjectName, Environment: values}, g.Members)
	if err != nil {
		return snapshot, err
	}
	baseline, err := c.stateStore.LatestDeployment(ctx, g.StackID)
	if err != nil {
		return snapshot, err
	}
	if baseline.Status != op.DeploymentSucceeded || baseline.ComposeDigest != snapshot.SourceDigest {
		return snapshot, ondemand.ErrUnavailable
	}
	groups, err := s.store.ListGroups(ctx, g.StackID)
	if err != nil {
		return snapshot, err
	}
	for _, other := range groups {
		if other.ID == g.ID {
			continue
		}
		for _, dep := range snapshot.ExternalDependencies {
			for _, member := range other.Members {
				if dep == member {
					return snapshot, ondemand.ErrUnavailable
				}
			}
		}
	}
	return snapshot, nil
}
func demandEvidence(snapshot compose.OnDemandSnapshot) ondemand.Evidence {
	e := ondemand.Evidence{SourceDigest: snapshot.SourceDigest, Bindings: snapshot.Bindings}
	for _, member := range snapshot.Containers {
		e.ContainerIDs = append(e.ContainerIDs, member.ID)
	}
	return e
}
func demandPhase(snapshot compose.OnDemandSnapshot) ondemand.Phase {
	if snapshot.Running {
		return ondemand.Running
	}
	return ondemand.Sleeping
}
func verifiedDemandEvidence(snapshot compose.OnDemandSnapshot, previous ondemand.Evidence) (ondemand.Evidence, error) {
	e := demandEvidence(snapshot)
	if e.SourceDigest != previous.SourceDigest || !reflect.DeepEqual(e.ContainerIDs, previous.ContainerIDs) {
		return e, ondemand.ErrConflict
	}
	if snapshot.Running {
		if !reflect.DeepEqual(e.Bindings, previous.Bindings) {
			return e, ondemand.ErrConflict
		}
	} else {
		e.Bindings = previous.Bindings
	}
	return e, nil
}
func (s *OnDemandService) SaveGroup(ctx context.Context, id stack.StackID, groupID string, update ondemand.PolicyUpdate) (ondemand.Group, error) {
	if err := ondemand.ValidatePolicy(update.Policy); err != nil {
		return ondemand.Group{}, err
	}
	release, err := s.control.coordinator.Try(false, string(id))
	if err != nil {
		return ondemand.Group{}, err
	}
	defer release()
	g := ondemand.Group{ID: groupID, StackID: id, Policy: update.Policy}
	var previous ondemand.Group
	if groupID != "" {
		previous, err = s.store.GetGroup(ctx, id, groupID)
		if err != nil {
			return g, err
		}
		if previous.Revision != update.ExpectedRevision {
			return g, ondemand.ErrConflict
		}
	}
	// Disabling must work even when Docker is unavailable. It releases reservations
	// and never changes container state or clears an existing manual hold.
	if groupID != "" && !update.Enabled && reflect.DeepEqual(update.Members, previous.Members) {
		phase := previous.Phase
		if phase == ondemand.Unknown {
			phase = ondemand.Running
		}
		saved, err := s.store.SaveGroup(ctx, id, groupID, update, previous.Evidence, phase)
		if err == nil {
			s.controller.SuspendGroup(groupID)
		}
		return saved, err
	}
	snapshot, err := s.snapshot(ctx, g, true)
	if err != nil {
		return g, err
	}
	evidence := demandEvidence(snapshot)
	if !snapshot.Running {
		if groupID == "" {
			return g, ondemand.ErrUnavailable
		}
		evidence, err = verifiedDemandEvidence(snapshot, previous.Evidence)
		if err != nil {
			return g, err
		}
	}
	saved, err := s.store.SaveGroup(ctx, id, groupID, update, evidence, demandPhase(snapshot))
	if err == nil {
		s.controller.Notify()
	}
	return saved, err
}
func (s *OnDemandService) DeleteGroup(ctx context.Context, id stack.StackID, groupID string, revision int64) error {
	release, err := s.control.coordinator.Try(false, string(id))
	if err != nil {
		return err
	}
	defer release()
	if err := s.store.DeleteGroup(ctx, id, groupID, revision); err != nil {
		return err
	}
	s.controller.SuspendGroup(groupID)
	return nil
}
func (s *OnDemandService) HoldGroup(ctx context.Context, id stack.StackID, groupID string, revision int64) error {
	release, err := s.control.coordinator.Try(false, string(id))
	if err != nil {
		return err
	}
	defer release()
	if err := s.store.HoldGroup(ctx, id, groupID, revision, "Manually held. Resume explicitly to enable automatic wake and sleep."); err != nil {
		return err
	}
	s.controller.SuspendGroup(groupID)
	return nil
}
func (s *OnDemandService) ResumeGroup(ctx context.Context, id stack.StackID, groupID string, revision int64) (ondemand.Group, error) {
	release, err := s.control.coordinator.Try(false, string(id))
	if err != nil {
		return ondemand.Group{}, err
	}
	defer release()
	g, err := s.store.GetGroup(ctx, id, groupID)
	if err != nil {
		return g, err
	}
	if g.Revision != revision {
		return g, ondemand.ErrConflict
	}
	snapshot, err := s.snapshot(ctx, g, true)
	if err != nil {
		return g, err
	}
	evidence := demandEvidence(snapshot)
	if !snapshot.Running {
		evidence, err = verifiedDemandEvidence(snapshot, g.Evidence)
		if err != nil {
			return g, err
		}
	}
	saved, err := s.store.ResumeGroup(ctx, id, groupID, revision, evidence, demandPhase(snapshot))
	if err == nil {
		s.controller.Notify()
	}
	return saved, err
}
func (s *OnDemandService) ObserveOnDemand(ctx context.Context, g ondemand.Group) (ondemand.Sample, error) {
	s.hostMu.Lock()
	check := time.Since(s.hostChecked[g.StackID]) >= time.Minute
	s.hostMu.Unlock()
	snapshot, err := s.snapshot(ctx, g, check)
	if err != nil {
		return ondemand.Sample{}, err
	}
	if check {
		s.hostMu.Lock()
		if len(s.hostChecked) >= ondemand.MaxGroups {
			clear(s.hostChecked)
		}
		s.hostChecked[g.StackID] = time.Now()
		s.hostMu.Unlock()
	}
	evidence, err := verifiedDemandEvidence(snapshot, g.Evidence)
	if err != nil {
		_ = s.store.PauseGroup(ctx, g, "Container identity or configuration changed. Review and resume.")
		return ondemand.Sample{}, err
	}
	sample := ondemand.Sample{Phase: demandPhase(snapshot), ObservedAt: time.Now(), Evidence: evidence}
	for _, member := range snapshot.Containers {
		sample.RuntimeEpoch += fmt.Sprintf("%s:%s:%d;", member.ID, member.StartedAt, member.RestartCount)
	}
	if snapshot.Running {
		counts, observed, err := s.control.runtime.(OnDemandRuntime).OnDemandCounters(ctx, evidence.ContainerIDs)
		if err != nil {
			return sample, err
		}
		sample.ObservedAt = observed
		sample.Counters = map[string]ondemand.Counters{}
		for id, count := range counts {
			sample.Counters[id] = ondemand.Counters{Received: count.Received, Sent: count.Sent}
		}
	}
	return sample, nil
}
func (s *OnDemandService) ExecuteOnDemand(ctx context.Context, g ondemand.Group, action ondemand.Action, observed ondemand.Sample, ports ondemand.Ports) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if action != ondemand.WakeUp && action != ondemand.Sleep {
		return ondemand.ErrInvalid
	}
	c := s.control
	release, err := c.coordinator.Try(false, string(g.StackID))
	if err != nil {
		return err
	}
	transferred := false
	defer func() {
		if !transferred {
			release()
		}
	}()
	fresh, err := s.store.GetGroup(ctx, g.StackID, g.ID)
	if err != nil {
		return err
	}
	if fresh.Revision != g.Revision || !fresh.Enabled || fresh.HoldReason != "" || fresh.PausedReason != "" || fresh.Phase != g.Phase {
		return ondemand.ErrConflict
	}
	snapshot, err := s.snapshot(ctx, fresh, true)
	if err != nil {
		return err
	}
	if _, err := verifiedDemandEvidence(snapshot, g.Evidence); err != nil {
		return err
	}
	if action == ondemand.Sleep && (!snapshot.Running || g.Phase != ondemand.Running) || action == ondemand.WakeUp && (snapshot.Running || g.Phase != ondemand.Sleeping) {
		return ondemand.ErrConflict
	}
	if action == ondemand.Sleep {
		if observed.ObservedAt.IsZero() || time.Since(observed.ObservedAt) > 10*time.Second || observed.ObservedAt.After(time.Now()) {
			return ondemand.ErrConflict
		}
		latest, err := s.ObserveOnDemand(ctx, g)
		if err != nil {
			return err
		}
		if len(latest.Counters) != len(g.Members) || !maps.Equal(latest.Counters, observed.Counters) || latest.RuntimeEpoch != observed.RuntimeEpoch {
			return ondemand.ErrConflict
		}
	}
	target, err := c.lookup.ByID(ctx, g.StackID)
	if err != nil {
		return err
	}
	values, err := c.environment.Values(ctx, g.StackID)
	if err != nil {
		return err
	}
	key := alert.Key{StackID: string(g.StackID), Problem: "on-demand", Target: "group:" + g.ID}
	request := op.OperationRequest{ID: op.NewOperationID(), Kind: "on-demand-" + string(action), ScopeType: "stack", ScopeID: string(g.StackID), Trigger: "automatic", StackName: target.DirectoryName, Secrets: mapValues(values), Timeout: time.Duration(g.StartupSeconds+g.StopGraceSeconds*int64(len(g.Members))+30) * time.Second, AlertTargets: []alert.Key{key}}
	operation, err := c.operations.StartTracked(ctx, request, func(job context.Context) op.Result {
		job, cancel := context.WithCancel(job)
		defer cancel()
		stop := context.AfterFunc(ctx, cancel)
		defer stop()
		if ctx.Err() != nil {
			return op.Result{Err: ctx.Err()}
		}
		if err := s.store.BeginTransition(job, g, action, request.ID); err != nil {
			return op.Result{Err: err}
		}
		completion := &op.OnDemandCompletion{GroupID: g.ID, Phase: string(ondemand.Unknown), PauseReason: "Transition could not be verified. Review containers and resume explicitly."}
		runtime := c.runtime.(OnDemandRuntime)
		var mutationErr error
		if action == ondemand.WakeUp {
			mutationErr = ports.Release(g)
			if mutationErr == nil {
				mutationErr = runtime.StartOnDemand(job, snapshot, time.Duration(g.StartupSeconds)*time.Second)
			}
			if mutationErr == nil {
				completion.Phase = string(ondemand.Running)
				completion.PauseReason = ""
			}
		} else {
			mutationErr = runtime.StopOnDemand(job, snapshot, int(g.StopGraceSeconds))
			if mutationErr == nil {
				mutationErr = ports.Reserve(g)
			}
			if mutationErr == nil {
				completion.Phase = string(ondemand.Sleeping)
				completion.PauseReason = ""
			}
		}
		if mutationErr != nil {
			_ = ports.Release(g)
		}
		return op.Result{Err: mutationErr, OnDemand: completion}
	}, release)
	if err != nil {
		return err
	}
	transferred = true
	completed, err := c.operations.Wait(context.Background(), operation.ID)
	if err != nil {
		_ = ports.Release(g)
		return err
	}
	if completed.Status != op.OperationSucceeded {
		return errors.New("on-demand operation failed; review operation history")
	}
	return nil
}
