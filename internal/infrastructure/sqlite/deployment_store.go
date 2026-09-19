package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/msoldin/porty/internal/domain"
)

type DeploymentStore struct{ db *sql.DB }

func NewDeploymentStore(db *sql.DB) *DeploymentStore { return &DeploymentStore{db: db} }

func (s *DeploymentStore) SaveDeployment(ctx context.Context, deployment domain.Deployment) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	operationStatus := "succeeded"
	if deployment.Status != domain.DeploymentSucceeded {
		operationStatus = "failed"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO operations(id,kind,scope_type,scope_id,status,started_at,completed_at,error_code,output_truncated) VALUES(?,?,?,?,?,?,?,?,0)`,
		deployment.OperationID, "deploy", "stack", deployment.StackID, operationStatus, encodeTime(deployment.StartedAt), encodeTime(deployment.CompletedAt), nullableString(deployment.ErrorCode)); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO deployments(id,stack_id,operation_id,git_commit,dirty,diff_digest,compose_digest,status,started_at,completed_at,duration_ms,error_code) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		deployment.ID, deployment.StackID, deployment.OperationID, nullableString(deployment.GitCommit), deployment.Dirty, nullableString(deployment.DiffDigest), nullableString(deployment.ComposeDigest), deployment.Status,
		encodeTime(deployment.StartedAt), encodeTime(deployment.CompletedAt), deployment.Duration.Milliseconds(), nullableString(deployment.ErrorCode))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *DeploymentStore) LatestDeployment(ctx context.Context, stackID domain.StackID) (domain.Deployment, error) {
	return scanDeployment(s.db.QueryRowContext(ctx, deploymentSelect+` WHERE stack_id=? ORDER BY started_at DESC LIMIT 1`, stackID))
}

func (s *DeploymentStore) Deployments(ctx context.Context, stackID domain.StackID, limit int) ([]domain.Deployment, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, deploymentSelect+` WHERE stack_id=? ORDER BY started_at DESC LIMIT ?`, stackID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.Deployment, 0)
	for rows.Next() {
		deployment, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, deployment)
	}
	return result, rows.Err()
}

const deploymentSelect = `SELECT id,stack_id,operation_id,git_commit,dirty,diff_digest,compose_digest,status,started_at,completed_at,duration_ms,error_code FROM deployments`

func scanDeployment(row rowScanner) (domain.Deployment, error) {
	var deployment domain.Deployment
	var gitCommit, diffDigest, composeDigest, completedAt, errorCode sql.NullString
	var startedAt string
	var durationMS sql.NullInt64
	if err := row.Scan(&deployment.ID, &deployment.StackID, &deployment.OperationID, &gitCommit, &deployment.Dirty, &diffDigest, &composeDigest, &deployment.Status, &startedAt, &completedAt, &durationMS, &errorCode); err != nil {
		return domain.Deployment{}, err
	}
	deployment.GitCommit = gitCommit.String
	deployment.DiffDigest = diffDigest.String
	deployment.ComposeDigest = composeDigest.String
	deployment.ErrorCode = errorCode.String
	deployment.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAt)
	if completedAt.Valid {
		deployment.CompletedAt, _ = time.Parse(time.RFC3339Nano, completedAt.String)
	}
	if durationMS.Valid {
		deployment.Duration = time.Duration(durationMS.Int64) * time.Millisecond
	}
	return deployment, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
