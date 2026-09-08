//go:build unix

package core

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"
)

// A missing entire dedicated process group proves its managed executor is gone.
// Existing/reused groups and permission failures remain conservative. We never
// signal a recovered PID, nor infer exit from a missing in-memory handle.
func sharedExecutorExited(group int) bool {
	return group > 0 && errors.Is(syscall.Kill(-group, 0), syscall.ESRCH)
}

func waitSharedExecutorGroup(ctx context.Context, group int) error {
	if group <= 0 {
		return nil
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for !sharedExecutorExited(group) {
		select {
		case <-ctx.Done():
			return fmt.Errorf("shared executor process group exit not confirmed: %w", ctx.Err())
		case <-ticker.C:
		}
	}
	return nil
}
