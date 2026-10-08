package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"github.com/msoldin/porty/internal/operation"
)

// A manual hold is admitted in the same transaction as its operation. Failed
// admission cannot suspend automation; accepted work stays held across restart.
func interlockOnDemand(ctx context.Context, tx *sql.Tx, o operation.Operation) error {
	if o.ScopeType != "stack" || o.Trigger != "manual" {
		return nil
	}
	action := strings.TrimPrefix(strings.TrimPrefix(o.Kind, "container_batch_"), "container_")
	switch action {
	case "stop", "start", "restart", "deploy", "recreate":
	default:
		return nil
	}
	query := `UPDATE on_demand_groups SET paused_reason='A manual runtime action requires review and explicit resume.',revision=revision+1`
	if action == "stop" {
		query += `,hold_reason='Manually stopped. Resume explicitly to allow automatic wake and sleep.'`
	}
	query += ` WHERE stack_id=? AND operation_id IS NULL`
	args := []any{o.ScopeID}
	if len(o.AffectedServices) > 0 {
		query += ` AND id IN (SELECT group_id FROM on_demand_members WHERE stack_id=? AND service IN (`
		args = append(args, o.ScopeID)
		for i, service := range o.AffectedServices {
			if i > 0 {
				query += ","
			}
			query += "?"
			args = append(args, service)
		}
		query += "))"
	}
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}
