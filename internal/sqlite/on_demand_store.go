package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/msoldin/porty/internal/ondemand"
	"github.com/msoldin/porty/internal/stack"
	"github.com/msoldin/porty/internal/traffic"
)

type OnDemandStore struct{ db *sql.DB }

func NewOnDemandStore(db *sql.DB) *OnDemandStore { return &OnDemandStore{db: db} }

const onDemandColumns = "g.id,g.stack_id,g.policy_json,g.revision,g.phase,g.hold_reason,g.paused_reason,g.evidence_json"

func scanOnDemand(row rowScanner) (ondemand.Group, error) {
	var g ondemand.Group
	var policy, evidence string
	if err := row.Scan(&g.ID, &g.StackID, &policy, &g.Revision, &g.Phase, &g.HoldReason, &g.PausedReason, &evidence); err != nil {
		return g, err
	}
	if err := json.Unmarshal([]byte(policy), &g.Policy); err != nil {
		return g, err
	}
	if err := json.Unmarshal([]byte(evidence), &g.Evidence); err != nil {
		return g, err
	}
	return g, nil
}
func (s *OnDemandStore) GetGroup(ctx context.Context, stackID stack.StackID, id string) (ondemand.Group, error) {
	return scanOnDemand(s.db.QueryRowContext(ctx, "SELECT "+onDemandColumns+" FROM on_demand_groups g JOIN stacks s ON s.id=g.stack_id WHERE g.id=? AND g.stack_id=? AND s.archived_at IS NULL", id, stackID))
}
func (s *OnDemandStore) ListGroups(ctx context.Context, stackID stack.StackID) ([]ondemand.Group, error) {
	query := "SELECT " + onDemandColumns + " FROM on_demand_groups g JOIN stacks s ON s.id=g.stack_id WHERE s.archived_at IS NULL"
	var args []any
	if stackID != "" {
		query += " AND g.stack_id=?"
		args = append(args, stackID)
	}
	query += " ORDER BY g.stack_id,g.id LIMIT 65"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ondemand.Group{}
	for rows.Next() {
		g, err := scanOnDemand(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, g)
	}
	if len(items) > ondemand.MaxGroups {
		return nil, fmt.Errorf("%w: group limit exceeded", ondemand.ErrUnavailable)
	}
	return items, rows.Err()
}
func validateOnDemandEvidence(p ondemand.Policy, e ondemand.Evidence, phase ondemand.Phase) error {
	if phase != ondemand.Running && phase != ondemand.Sleeping {
		return fmt.Errorf("%w: group must be uniformly running or stopped", ondemand.ErrInvalid)
	}
	if e.SourceDigest == "" || len(e.SourceDigest) > 128 || len(e.ContainerIDs) != len(p.Members) {
		return fmt.Errorf("%w: incomplete runtime identity", ondemand.ErrInvalid)
	}
	ids := map[string]bool{}
	for _, id := range e.ContainerIDs {
		if id == "" || len(id) > 128 || ids[id] {
			return fmt.Errorf("%w: invalid container identity", ondemand.ErrInvalid)
		}
		ids[id] = true
	}
	if err := traffic.Validate(traffic.Config{Generation: 1, Threshold: p.WakeThreshold, Window: time.Duration(p.WakeWindowMS) * time.Millisecond, Bindings: e.Bindings}); err != nil {
		return fmt.Errorf("%w: %v", ondemand.ErrInvalid, err)
	}
	return nil
}
func onDemandRowChanged(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ondemand.ErrConflict
	}
	return nil
}
func (s *OnDemandStore) SaveGroup(ctx context.Context, stackID stack.StackID, id string, update ondemand.PolicyUpdate, evidence ondemand.Evidence, phase ondemand.Phase) (ondemand.Group, error) {
	if err := ondemand.ValidatePolicy(update.Policy); err != nil {
		return ondemand.Group{}, err
	}
	if err := validateOnDemandEvidence(update.Policy, evidence, phase); err != nil {
		return ondemand.Group{}, err
	}
	if (id == "" && update.ExpectedRevision != 0) || (id != "" && update.ExpectedRevision < 1) {
		return ondemand.Group{}, ondemand.ErrConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ondemand.Group{}, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT 1 FROM stacks WHERE id=? AND archived_at IS NULL", stackID).Scan(&exists); err != nil {
		return ondemand.Group{}, err
	}
	policyJSON, err := json.Marshal(update.Policy)
	if err != nil {
		return ondemand.Group{}, err
	}
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		return ondemand.Group{}, err
	}
	if id == "" {
		id = "odg_" + rand.Text()
		_, err = tx.ExecContext(ctx, `INSERT INTO on_demand_groups(id,stack_id,policy_json,revision,phase,evidence_json) VALUES(?,?,?,1,?,?)`, id, stackID, string(policyJSON), phase, string(evidenceJSON))
	} else {
		result, writeErr := tx.ExecContext(ctx, `UPDATE on_demand_groups SET policy_json=?,evidence_json=?,revision=revision+1 WHERE id=? AND stack_id=? AND revision=? AND operation_id IS NULL`, string(policyJSON), string(evidenceJSON), id, stackID, update.ExpectedRevision)
		err = onDemandRowChanged(result, writeErr)
	}
	if err != nil {
		return ondemand.Group{}, err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM on_demand_members WHERE group_id=?", id); err != nil {
		return ondemand.Group{}, err
	}
	for _, member := range update.Members {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM on_demand_members WHERE stack_id=? AND service=?", stackID, member).Scan(&count); err != nil {
			return ondemand.Group{}, err
		}
		if count != 0 {
			return ondemand.Group{}, ondemand.ErrConflict
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO on_demand_members(group_id,stack_id,service) VALUES(?,?,?)", id, stackID, member); err != nil {
			return ondemand.Group{}, err
		}
	}
	if err := checkOnDemandBounds(ctx, tx); err != nil {
		return ondemand.Group{}, err
	}
	g, err := scanOnDemand(tx.QueryRowContext(ctx, "SELECT "+onDemandColumns+" FROM on_demand_groups g WHERE g.id=?", id))
	if err != nil {
		return g, err
	}
	return g, tx.Commit()
}
func checkOnDemandBounds(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT g.evidence_json FROM on_demand_groups g JOIN stacks s ON s.id=g.stack_id WHERE s.archived_at IS NULL`)
	if err != nil {
		return err
	}
	defer rows.Close()
	groups := 0
	var bindings []traffic.Binding
	for rows.Next() {
		groups++
		var encoded string
		if err := rows.Scan(&encoded); err != nil {
			return err
		}
		var e ondemand.Evidence
		if err := json.Unmarshal([]byte(encoded), &e); err != nil {
			return err
		}
		for _, b := range e.Bindings {
			a, err := netip.ParseAddrPort(b.Address)
			if err != nil {
				return err
			}
			for _, other := range bindings {
				if b.Network != other.Network {
					continue
				}
				o, err := netip.ParseAddrPort(other.Address)
				if err != nil {
					return err
				}
				if a.Port() == o.Port() && (a.Addr() == o.Addr() || a.Addr().IsUnspecified() || o.Addr().IsUnspecified()) {
					return fmt.Errorf("%w: published endpoint overlaps another group", ondemand.ErrConflict)
				}
			}
			bindings = append(bindings, b)
		}
		if groups > ondemand.MaxGroups || len(bindings) > traffic.MaxBindings {
			return fmt.Errorf("%w: at most 64 groups and 256 published endpoints", ondemand.ErrInvalid)
		}
	}
	return rows.Err()
}
func (s *OnDemandStore) DeleteGroup(ctx context.Context, stackID stack.StackID, id string, revision int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM on_demand_groups WHERE id=? AND stack_id=? AND revision=? AND operation_id IS NULL AND EXISTS(SELECT 1 FROM stacks WHERE id=? AND archived_at IS NULL)`, id, stackID, revision, stackID)
	return onDemandRowChanged(result, err)
}
func (s *OnDemandStore) HoldGroup(ctx context.Context, stackID stack.StackID, id string, revision int64, reason string) error {
	if strings.TrimSpace(reason) == "" || len(reason) > 256 {
		return ondemand.ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, `UPDATE on_demand_groups SET hold_reason=?,revision=revision+1 WHERE id=? AND stack_id=? AND revision=? AND operation_id IS NULL AND EXISTS(SELECT 1 FROM stacks WHERE id=? AND archived_at IS NULL)`, reason, id, stackID, revision, stackID)
	return onDemandRowChanged(result, err)
}
func (s *OnDemandStore) PauseGroup(ctx context.Context, g ondemand.Group, reason string) error {
	if reason == "" || len(reason) > 256 {
		return ondemand.ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, `UPDATE on_demand_groups SET paused_reason=?,revision=revision+1 WHERE id=? AND stack_id=? AND revision=? AND operation_id IS NULL`, reason, g.ID, g.StackID, g.Revision)
	return onDemandRowChanged(result, err)
}
func (s *OnDemandStore) ResumeGroup(ctx context.Context, stackID stack.StackID, id string, revision int64, evidence ondemand.Evidence, phase ondemand.Phase) (ondemand.Group, error) {
	g, err := s.GetGroup(ctx, stackID, id)
	if err != nil {
		return g, err
	}
	if g.Revision != revision {
		return g, ondemand.ErrConflict
	}
	if err := validateOnDemandEvidence(g.Policy, evidence, phase); err != nil {
		return g, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return g, err
	}
	defer tx.Rollback()
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return g, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE on_demand_groups SET hold_reason='',paused_reason='',phase=?,evidence_json=?,revision=revision+1 WHERE id=? AND stack_id=? AND revision=? AND operation_id IS NULL AND EXISTS(SELECT 1 FROM stacks WHERE id=? AND archived_at IS NULL)`, phase, string(encoded), id, stackID, revision, stackID)
	if err := onDemandRowChanged(result, err); err != nil {
		return g, err
	}
	if err := checkOnDemandBounds(ctx, tx); err != nil {
		return g, err
	}
	g, err = scanOnDemand(tx.QueryRowContext(ctx, "SELECT "+onDemandColumns+" FROM on_demand_groups g WHERE g.id=?", id))
	if err != nil {
		return g, err
	}
	return g, tx.Commit()
}
func (s *OnDemandStore) BeginTransition(ctx context.Context, g ondemand.Group, action ondemand.Action, operationID string) error {
	if operationID == "" {
		return ondemand.ErrInvalid
	}
	var from, to ondemand.Phase
	switch action {
	case ondemand.WakeUp:
		from, to = ondemand.Sleeping, ondemand.Starting
	case ondemand.Sleep:
		from, to = ondemand.Running, ondemand.Stopping
	default:
		return ondemand.ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, `UPDATE on_demand_groups SET phase=?,operation_id=? WHERE id=? AND stack_id=? AND revision=? AND phase=? AND operation_id IS NULL AND hold_reason='' AND paused_reason='' AND json_extract(policy_json,'$.enabled')=1 AND EXISTS(SELECT 1 FROM stacks WHERE id=? AND archived_at IS NULL)`, to, operationID, g.ID, g.StackID, g.Revision, from, g.StackID)
	return onDemandRowChanged(result, err)
}
func (s *OnDemandStore) RecoverInterrupted(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE on_demand_groups SET phase='unknown',paused_reason='An on-demand transition was interrupted. Review containers and resume explicitly.',operation_id=NULL,revision=revision+1 WHERE operation_id IS NOT NULL OR phase IN ('starting','stopping')`)
	return err
}
