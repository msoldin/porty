package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/msoldin/porty/internal/domain"
	"github.com/msoldin/porty/internal/sqlite/generated"
)

type AuditStore struct {
	db      *sql.DB
	queries *generated.Queries
}

func NewAuditStore(db *sql.DB) *AuditStore { return &AuditStore{db: db, queries: generated.New(db)} }

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
	rows, err := s.queries.ListAuditEvents(ctx, generated.ListAuditEventsParams{Limit: int64(limit), Offset: int64(offset)})
	if err != nil {
		return nil, err
	}
	result := make([]domain.AuditEvent, 0, len(rows))
	for _, row := range rows {
		occurred, _ := time.Parse(time.RFC3339Nano, row.OccurredAt)
		result = append(result, domain.AuditEvent{
			ID: row.ID, ActorUserID: row.ActorUserID.String, Action: row.Action,
			TargetType: row.TargetType, TargetID: row.TargetID.String,
			Outcome: row.Outcome, RequestID: row.RequestID, SourceIP: row.SourceIp.String,
			OccurredAt: occurred,
		})
	}
	return result, nil
}
