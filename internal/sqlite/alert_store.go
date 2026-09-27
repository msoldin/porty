package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/msoldin/porty/internal/alert"
)

type AlertStore struct{ db *sql.DB }

func NewAlertStore(db *sql.DB) *AlertStore { return &AlertStore{db: db} }

const alertColumns = `id,stack_id,problem,target,stack_name,revision,episode,occurrence_count,summary,operation_id,first_at,latest_at,acknowledged_at,acknowledged_by,resolved_at,resolved_by,resolution,can_resolve_manually`

func scanAlert(row rowScanner) (alert.Alert, error) {
	var a alert.Alert
	var first, last string
	var ack, res sql.NullString
	err := row.Scan(&a.ID, &a.Key.StackID, &a.Key.Problem, &a.Key.Target, &a.StackName, &a.Revision, &a.Episode, &a.Count, &a.Summary, &a.OperationID, &first, &last, &ack, &a.AcknowledgedBy, &res, &a.ResolvedBy, &a.Resolution, &a.CanResolveManually)
	if errors.Is(err, sql.ErrNoRows) {
		return a, alert.ErrNotFound
	}
	if err != nil {
		return a, err
	}
	a.FirstAt, err = time.Parse(time.RFC3339Nano, first)
	if err != nil {
		return a, err
	}
	a.LatestAt, err = time.Parse(time.RFC3339Nano, last)
	if err != nil {
		return a, err
	}
	if ack.Valid {
		v, e := time.Parse(time.RFC3339Nano, ack.String)
		if e != nil {
			return a, e
		}
		a.AcknowledgedAt = &v
	}
	if res.Valid {
		v, e := time.Parse(time.RFC3339Nano, res.String)
		if e != nil {
			return a, e
		}
		a.ResolvedAt = &v
	}
	return a, nil
}

func alertID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "alt_" + hex.EncodeToString(b[:]), nil
}
func optionalAlertTime(v *time.Time) any {
	if v == nil {
		return nil
	}
	return encodeTime(v.UTC())
}

func saveAlert(ctx context.Context, tx *sql.Tx, a alert.Alert) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO alerts (`+alertColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(id) DO UPDATE SET stack_name=excluded.stack_name,revision=excluded.revision,episode=excluded.episode,occurrence_count=excluded.occurrence_count,summary=excluded.summary,operation_id=excluded.operation_id,first_at=excluded.first_at,latest_at=excluded.latest_at,acknowledged_at=excluded.acknowledged_at,acknowledged_by=excluded.acknowledged_by,resolved_at=excluded.resolved_at,resolved_by=excluded.resolved_by,resolution=excluded.resolution,can_resolve_manually=excluded.can_resolve_manually`,
		a.ID, a.Key.StackID, a.Key.Problem, a.Key.Target, a.StackName, a.Revision, a.Episode, a.Count, a.Summary, a.OperationID, encodeTime(a.FirstAt), encodeTime(a.LatestAt), optionalAlertTime(a.AcknowledgedAt), a.AcknowledgedBy, optionalAlertTime(a.ResolvedAt), a.ResolvedBy, a.Resolution, a.CanResolveManually)
	return err
}
func recordAlertEvent(ctx context.Context, tx *sql.Tx, a alert.Alert, kind, actor, note, operation string, at time.Time) error {
	id, err := alertID()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO alert_events(id,alert_id,episode,kind,actor_id,occurred_at,note,operation_id) VALUES(?,?,?,?,?,?,?,?)`, id, a.ID, a.Episode, kind, actor, encodeTime(at.UTC()), note, operation)
	return err
}

// applyAlertChanges uses the caller's transaction so operation completion and
// its alerts either both persist or neither does.
func applyAlertChanges(ctx context.Context, tx *sql.Tx, changes []alert.Change) ([]alert.Alert, error) {
	result := make([]alert.Alert, 0, len(changes))
	for _, c := range changes {
		if err := alert.ValidateChange(c); err != nil {
			return nil, err
		}
		a, err := scanAlert(tx.QueryRowContext(ctx, `SELECT `+alertColumns+` FROM alerts WHERE stack_id=? AND problem=? AND target=?`, c.Key.StackID, c.Key.Problem, c.Key.Target))
		fresh := errors.Is(err, alert.ErrNotFound)
		if err != nil && !fresh {
			return nil, err
		}
		if fresh && c.Kind == "recovery" {
			continue
		}
		if fresh {
			id, e := alertID()
			if e != nil {
				return nil, e
			}
			a = alert.Alert{ID: id, Key: c.Key, Episode: 1, FirstAt: c.ObservedAt.UTC()}
		}
		if !fresh {
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM alert_occurrences WHERE alert_id=? AND occurrence_id=?`, a.ID, c.OccurrenceID).Scan(&exists); err != nil {
				return nil, err
			}
			if exists > 0 {
				result = append(result, a)
				continue
			}
		}
		at := c.ObservedAt.UTC()
		if c.Kind == "recovery" {
			if a.Revision != c.ExpectedRevision {
				return nil, alert.ErrConflict
			}
			if a.ResolvedAt != nil {
				continue
			}
			a.ResolvedAt = &at
			a.ResolvedBy = ""
			a.Resolution = "Verified recovery"
		} else {
			if a.ResolvedAt != nil {
				a.Episode++
				a.Count = 0
				a.FirstAt = at
				a.AcknowledgedAt = nil
				a.AcknowledgedBy = ""
				a.ResolvedAt = nil
				a.ResolvedBy = ""
				a.Resolution = ""
			}
			a.Count++
			a.Summary = c.Summary
			a.OperationID = c.OperationID
			a.CanResolveManually = c.CanResolveManually
			if c.StackName != "" {
				a.StackName = c.StackName
			}
		}
		if at.After(a.LatestAt) {
			a.LatestAt = at
		}
		a.Revision++
		if err := saveAlert(ctx, tx, a); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO alert_occurrences(alert_id,occurrence_id) VALUES(?,?)`, a.ID, c.OccurrenceID); err != nil {
			return nil, err
		}
		if err := recordAlertEvent(ctx, tx, a, c.Kind, "", c.Summary, c.OperationID, at); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, nil
}

func (s *AlertStore) Apply(ctx context.Context, changes []alert.Change) ([]alert.Alert, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := applyAlertChanges(ctx, tx, changes)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
func (s *AlertStore) Get(ctx context.Context, id string) (alert.Alert, error) {
	return scanAlert(s.db.QueryRowContext(ctx, `SELECT `+alertColumns+` FROM alerts WHERE id=?`, id))
}

func (s *AlertStore) List(ctx context.Context, filter alert.Filter) (alert.Page, error) {
	page := alert.Page{Items: []alert.Alert{}}
	f, err := alert.NormalizeFilter(filter)
	if err != nil {
		return page, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	where := "1=1"
	args := []any{}
	if f.StackID != "" {
		where += " AND stack_id=?"
		args = append(args, f.StackID)
	}
	switch f.View {
	case "attention":
		where += " AND (resolved_at IS NULL OR acknowledged_at IS NULL)"
	case "open":
		where += " AND resolved_at IS NULL"
	case "unacknowledged":
		where += " AND acknowledged_at IS NULL"
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM alerts WHERE acknowledged_at IS NULL`).Scan(&page.UnacknowledgedCount); err != nil {
		return page, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM alerts WHERE `+where, args...).Scan(&page.Total); err != nil {
		return page, err
	}
	args = append(args, f.Limit, f.Offset)
	rows, err := tx.QueryContext(ctx, `SELECT `+alertColumns+` FROM alerts WHERE `+where+` ORDER BY latest_at DESC,id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return page, err
	}
	for rows.Next() {
		a, e := scanAlert(rows)
		if e != nil {
			rows.Close()
			return page, e
		}
		page.Items = append(page.Items, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	return page, tx.Commit()
}

func (s *AlertStore) Acknowledge(ctx context.Context, m alert.Mutation) (alert.Alert, error) {
	return s.mutate(ctx, m, false)
}
func (s *AlertStore) Resolve(ctx context.Context, m alert.Mutation) (alert.Alert, error) {
	return s.mutate(ctx, m, true)
}
func (s *AlertStore) mutate(ctx context.Context, m alert.Mutation, resolve bool) (alert.Alert, error) {
	if err := alert.ValidateMutation(m); err != nil {
		return alert.Alert{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return alert.Alert{}, err
	}
	defer tx.Rollback()
	a, err := scanAlert(tx.QueryRowContext(ctx, `SELECT `+alertColumns+` FROM alerts WHERE id=?`, m.ID))
	if err != nil {
		return a, err
	}
	if a.Revision != m.ExpectedRevision {
		return a, alert.ErrConflict
	}
	now := time.Now().UTC()
	kind := "acknowledged"
	if resolve {
		if !a.CanResolveManually {
			return a, alert.ErrManualResolutionUnavailable
		}
		if a.ResolvedAt != nil {
			return a, alert.ErrConflict
		}
		a.ResolvedAt = &now
		a.ResolvedBy = m.ActorID
		a.Resolution = m.Note
		kind = "resolved"
	} else {
		if a.AcknowledgedAt != nil {
			return a, alert.ErrConflict
		}
		a.AcknowledgedAt = &now
		a.AcknowledgedBy = m.ActorID
	}
	a.Revision++
	if err := saveAlert(ctx, tx, a); err != nil {
		return a, err
	}
	if err := recordAlertEvent(ctx, tx, a, kind, m.ActorID, m.Note, "", now); err != nil {
		return a, err
	}
	return a, tx.Commit()
}

func (s *AlertStore) History(ctx context.Context, id string, limit, offset int) ([]alert.Event, error) {
	f, err := alert.NormalizeFilter(alert.Filter{Limit: limit, Offset: offset})
	if err != nil {
		return nil, err
	}
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,alert_id,episode,kind,actor_id,occurred_at,note,operation_id FROM alert_events WHERE alert_id=? ORDER BY sequence DESC LIMIT ? OFFSET ?`, id, f.Limit, f.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []alert.Event{}
	for rows.Next() {
		var e alert.Event
		var at string
		if err := rows.Scan(&e.ID, &e.AlertID, &e.Episode, &e.Kind, &e.ActorID, &at, &e.Note, &e.OperationID); err != nil {
			return nil, err
		}
		e.At, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return nil, fmt.Errorf("alert history time: %w", err)
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
