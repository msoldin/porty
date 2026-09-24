package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	portyop "github.com/msoldin/porty/internal/operation"
	"github.com/msoldin/porty/internal/sqlite/generated"
	portystack "github.com/msoldin/porty/internal/stack"
)

type DeploymentStore struct {
	db      *sql.DB
	queries *generated.Queries
}

func NewDeploymentStore(db *sql.DB) *DeploymentStore {
	return &DeploymentStore{db: db, queries: generated.New(db)}
}

func (s *DeploymentStore) SaveDeployment(ctx context.Context, deployment portyop.Deployment) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO deployments(id,stack_id,operation_id,git_commit,dirty,diff_digest,compose_digest,status,started_at,completed_at,duration_ms,error_code) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		deployment.ID, deployment.StackID, deployment.OperationID, nullableString(deployment.GitCommit), deployment.Dirty, nullableString(deployment.DiffDigest), nullableString(deployment.ComposeDigest), deployment.Status,
		encodeTime(deployment.StartedAt), nullableTime(deployment.CompletedAt), deployment.Duration.Milliseconds(), nullableString(deployment.ErrorCode))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *DeploymentStore) LatestDeployment(ctx context.Context, stackID portystack.StackID) (portyop.Deployment, error) {
	row, err := s.queries.GetLatestDeployment(ctx, string(stackID))
	if err != nil {
		return portyop.Deployment{}, err
	}
	return deploymentFromRow(row), nil
}

func (s *DeploymentStore) LatestDeploymentTimes(ctx context.Context) (map[portystack.StackID]time.Time, error) {
	rows, err := s.queries.ListLatestDeploymentTimes(ctx)
	if err != nil {
		return nil, err
	}
	result := make(map[portystack.StackID]time.Time, len(rows))
	for _, row := range rows {
		at, err := time.Parse(time.RFC3339Nano, row.StartedAt)
		if err != nil {
			return nil, fmt.Errorf("parse deployment time for stack %s: %w", row.StackID, err)
		}
		result[portystack.StackID(row.StackID)] = at
	}
	return result, nil
}

func (s *DeploymentStore) HasSuccessfulDeployment(ctx context.Context, stackID portystack.StackID) (bool, error) {
	return s.queries.HasSuccessfulDeployment(ctx, string(stackID))
}

func (s *DeploymentStore) Deployments(ctx context.Context, stackID portystack.StackID, limit int) ([]portyop.Deployment, error) {
	return s.DeploymentsPage(ctx, stackID, limit, 0)
}

func (s *DeploymentStore) DeploymentsPage(ctx context.Context, stackID portystack.StackID, limit, offset int) ([]portyop.Deployment, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.queries.ListDeployments(ctx, generated.ListDeploymentsParams{StackID: string(stackID), Limit: int64(limit), Offset: int64(offset)})
	if err != nil {
		return nil, err
	}
	result := make([]portyop.Deployment, 0, len(rows))
	for _, row := range rows {
		result = append(result, deploymentFromRow(row))
	}
	return result, nil
}

func deploymentFromRow(row generated.Deployment) portyop.Deployment {
	deployment := portyop.Deployment{
		ID: row.ID, StackID: portystack.StackID(row.StackID), OperationID: row.OperationID,
		GitCommit: row.GitCommit.String, Dirty: row.Dirty != 0,
		DiffDigest: row.DiffDigest.String, ComposeDigest: row.ComposeDigest.String,
		Status: portyop.DeploymentStatus(row.Status), ErrorCode: row.ErrorCode.String,
	}
	deployment.StartedAt, _ = time.Parse(time.RFC3339Nano, row.StartedAt)
	if row.CompletedAt.Valid {
		deployment.CompletedAt, _ = time.Parse(time.RFC3339Nano, row.CompletedAt.String)
	}
	if row.DurationMs.Valid {
		deployment.Duration = time.Duration(row.DurationMs.Int64) * time.Millisecond
	}
	return deployment
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
