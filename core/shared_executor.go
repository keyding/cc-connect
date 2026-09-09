package core

import "context"

type sharedExecutorCheckpointKey struct{}
type sharedHistoryCheckpointKey struct{}

// CheckpointSharedExecutor records a dedicated local process group after spawn.
// Adapters invoke it before returning the created executor. With ordinary
// sessions there is no checkpoint, so this is a no-op. A checkpoint error means
// the adapter must kill and reap the newly created process before returning.
func CheckpointSharedExecutor(ctx context.Context, processGroup int) error {
	if record, ok := ctx.Value(sharedExecutorCheckpointKey{}).(func(int) error); ok {
		return record(processGroup)
	}
	return nil
}

func (q *sharedQueue) executorContext(ctx context.Context, id string) context.Context {
	ctx = context.WithValue(ctx, sharedHistoryCheckpointKey{}, func(history string) error {
		q.mu.Lock()
		defer q.mu.Unlock()
		i := indexOfSharedRequest(q.requests, id)
		if q.requests[i].HistoryID == history {
			return nil
		}
		next := append([]sharedRequest(nil), q.requests...)
		next[i].HistoryID = history
		if err := q.save(next); err != nil {
			q.paused = true
			return err
		}
		q.requests = next
		return nil
	})
	return context.WithValue(ctx, sharedExecutorCheckpointKey{}, func(group int) error {
		q.mu.Lock()
		defer q.mu.Unlock()
		next := append([]sharedRequest(nil), q.requests...)
		i := indexOfSharedRequest(next, id)
		next[i].ExecutorGroup = group
		if err := q.save(next); err != nil {
			q.paused = true
			q.requests[i].ExecutorGroup = group
			return err
		}
		q.requests = next
		return nil
	})
}

func (q *sharedQueue) executorStartFailed(id string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return sharedExecutorExited(q.requests[indexOfSharedRequest(q.requests, id)].ExecutorGroup)
}

// CheckpointSharedHistory preserves a discovered conversation identity without
// promising that an interrupted turn can be resumed from a tool checkpoint.
func CheckpointSharedHistory(ctx context.Context, history string) error {
	if record, ok := ctx.Value(sharedHistoryCheckpointKey{}).(func(string) error); ok && history != "" {
		return record(history)
	}
	return nil
}

func (q *sharedQueue) executorGroup(id string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.requests[indexOfSharedRequest(q.requests, id)].ExecutorGroup
}
