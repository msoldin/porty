package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/msoldin/porty/internal/ondemand"
	"github.com/msoldin/porty/internal/operation"
)

func completeOnDemand(ctx context.Context, tx *sql.Tx, op operation.Operation, completion *operation.OnDemandCompletion) error {
	if completion == nil {
		return nil
	}
	if completion.GroupID == "" || op.ScopeType != "stack" || len(completion.PauseReason) > 256 {
		return ondemand.ErrInvalid
	}
	phase := ondemand.Phase(completion.Phase)
	if phase != ondemand.Running && phase != ondemand.Sleeping && phase != ondemand.Unknown {
		return ondemand.ErrInvalid
	}
	if op.Status != operation.OperationSucceeded && phase != ondemand.Unknown {
		return fmt.Errorf("%w: failed transition cannot publish a healthy phase", ondemand.ErrInvalid)
	}
	if phase == ondemand.Unknown && completion.PauseReason == "" {
		return fmt.Errorf("%w: ambiguous transition must pause", ondemand.ErrInvalid)
	}
	from := ondemand.Starting
	switch op.Kind {
	case "on-demand-wake":
		if phase == ondemand.Sleeping {
			return ondemand.ErrInvalid
		}
	case "on-demand-sleep":
		from = ondemand.Stopping
		if phase == ondemand.Running {
			return ondemand.ErrInvalid
		}
	default:
		return ondemand.ErrInvalid
	}
	result, err := tx.ExecContext(ctx, `UPDATE on_demand_groups SET phase=?,paused_reason=?,operation_id=NULL,last_operation_id=? WHERE id=? AND stack_id=? AND operation_id=? AND phase=?`, phase, completion.PauseReason, op.ID, completion.GroupID, op.ScopeID, op.ID, from)
	return onDemandRowChanged(result, err)
}
