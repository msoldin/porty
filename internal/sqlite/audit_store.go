package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/msoldin/porty/internal/domain"
)

type AuditStore struct{ db *sql.DB }

func NewAuditStore(db *sql.DB) *AuditStore { return &AuditStore{db: db} }

func (s *AuditStore) RecordAudit(ctx context.Context, event domain.AuditEvent) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_events(id,actor_user_id,action,target_type,target_id,outcome,request_id,source_ip,occurred_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		event.ID, nullableString(event.ActorUserID), event.Action, event.TargetType, nullableString(event.TargetID), event.Outcome, event.RequestID, nullableString(event.SourceIP), encodeTime(event.OccurredAt))
	return err
}

func (s *AuditStore) AuditEvents(ctx context.Context, limit, offset int) ([]domain.AuditEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,actor_user_id,action,target_type,target_id,outcome,request_id,source_ip,occurred_at FROM audit_events ORDER BY occurred_at DESC, rowid DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.AuditEvent
	for rows.Next() {
		var event domain.AuditEvent
		var actor, target, source sql.NullString
		var occurred string
		if err := rows.Scan(&event.ID, &actor, &event.Action, &event.TargetType, &target, &event.Outcome, &event.RequestID, &source, &occurred); err != nil {
			return nil, err
		}
		event.ActorUserID, event.TargetID, event.SourceIP = actor.String, target.String, source.String
		event.OccurredAt, _ = time.Parse(time.RFC3339Nano, occurred)
		result = append(result, event)
	}
	return result, rows.Err()
}
