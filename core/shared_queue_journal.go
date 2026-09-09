package core

import (
	"encoding/json"
	"fmt"
	"os"
)

// Before/After intent is durable before replacing the main snapshot. Dispatch
// occurs only after the committed journal checkpoint. On an interrupted write,
// retain the prior accepted requests plus any discovered executor identity;
// never treat a prospective completion as reliable or admit an uncommitted task.
type sharedQueueJournal struct {
	Version   int
	Before    []sharedRequest
	After     []sharedRequest
	Committed bool
}

func (q *sharedQueue) recoverJournal() error {
	data, err := os.ReadFile(q.path + ".guard")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	q.guarded = true
	var journal sharedQueueJournal
	if err = json.Unmarshal(data, &journal); err != nil {
		return fmt.Errorf("read shared queue journal: %w", err)
	}
	if journal.Version != 1 {
		return fmt.Errorf("unsupported shared queue journal version")
	}
	if journal.Committed {
		q.requests = journal.After
		return nil
	}
	q.requests = journal.Before
	for i, r := range q.requests {
		for _, candidate := range journal.After {
			if candidate.ID != r.ID {
				continue
			}
			// A attempted start is conservatively interrupted even when launch never happened.
			if candidate.Status == "running" || r.Status == "running" || r.Status == "stopping" {
				q.requests[i].Status = "interrupted"
				if candidate.ExitConfirmed {
					q.requests[i].ExitConfirmed = true
				}
				if candidate.ExecutorGroup != 0 {
					q.requests[i].ExecutorGroup = candidate.ExecutorGroup
				}
			}
		}
	}
	return nil
}

// A small storage boundary keeps checkpoint failure semantics testable while the
// production implementation continues to use real atomic, fsynced local files.
type sharedSnapshotWriter interface{ writeSnapshot(string, any) error }
type sharedSnapshotFiles struct{}

func (sharedSnapshotFiles) writeSnapshot(path string, value any) error {
	return writeSharedQueueSnapshot(path, value)
}

type sharedCommitError struct{ err error }

func (e *sharedCommitError) Error() string {
	return fmt.Sprintf("shared journal commit outcome uncertain: %v", e.err)
}
func (e *sharedCommitError) Unwrap() error { return e.err }

type sharedAdmissionUncertainError struct {
	Session sharedSession
	ID      string
	err     error
}

func (e *sharedAdmissionUncertainError) Error() string { return e.err.Error() }
func (e *sharedAdmissionUncertainError) Unwrap() error { return e.err }
