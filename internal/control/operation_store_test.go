package control_test

import (
	"context"
	"github.com/msoldin/porty/internal/alert"
	op "github.com/msoldin/porty/internal/operation"
)

func (s *countingOperationStore) CompleteOperation(ctx context.Context, o op.Operation, r op.Result) ([]alert.Alert, error) {
	return nil, s.UpdateOperation(ctx, o)
}
