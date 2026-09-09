package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// Shared requests are immutable after admission except for execution/result state.
// Attachments remain in the snapshot as session materials, including after completion.
type sharedRequest struct {
	ID                                                                    string
	Session                                                               sharedSession
	Platform, Scope, Entry, UserID, UserName, MessageID, Content, WorkDir string
	Reply                                                                 json.RawMessage
	Images                                                                []ImageAttachment
	Files                                                                 []FileAttachment
	Status                                                                string
	Waiting                                                               bool
	ExitConfirmed                                                         bool
	ExecutorGroup                                                         int
	ContinueWarned                                                        bool
	HistoryID, Result                                                     string
}

type sharedQueue struct {
	mu                 sync.Mutex
	path               string
	requests           []sharedRequest
	err                error
	running            bool
	guarded            bool
	paused             bool
	cancels            map[string]context.CancelFunc
	writer             sharedSnapshotWriter
	uncertainAdmission *sharedRequest
	interactions       map[string]*sharedInteraction
}

func newSharedQueue(path string) *sharedQueue {
	q := &sharedQueue{cancels: map[string]context.CancelFunc{}, writer: sharedSnapshotFiles{}}
	if path == "" {
		q.err = fmt.Errorf("shared requests require persistent storage")
		return q
	}
	q.path = path + ".requests.json"

	data, err := os.ReadFile(q.path)
	if os.IsNotExist(err) {
		return q
	}
	if err == nil {
		err = json.Unmarshal(data, &q.requests)
	}
	if err == nil {
		err = q.recoverJournal()
	}
	q.err = err
	if err == nil {
		for i := range q.requests {
			r := &q.requests[i]

			switch r.Status {
			case "queued", "completed", "cancelled", "stopped":
			case "running", "stopping", "interrupted":
				if !r.ExitConfirmed && sharedExecutorExited(r.ExecutorGroup) {
					r.ExitConfirmed = true
				}
				r.Status = "interrupted"
				if !r.ExitConfirmed {
					retainSharedWorkspace(r.WorkDir, r.ID)
				}
			default:
				q.err = fmt.Errorf("invalid shared request status")
			}
		}
	}
	return q
}

// save is an atomic, fsynced snapshot, including attachment bytes. On write
// failure the caller must not hand work to an agent or release an executor lock.
func (q *sharedQueue) save(requests []sharedRequest) error {
	if q.err != nil {
		return q.err
	}
	journal := sharedQueueJournal{Version: 1, Before: q.requests, After: requests}
	if err := q.writer.writeSnapshot(q.path+".guard", journal); err != nil {
		return err
	}
	q.guarded = true
	if err := q.writer.writeSnapshot(q.path, requests); err != nil {
		return err
	}
	journal.Committed = true
	if err := q.writer.writeSnapshot(q.path+".guard", journal); err != nil {
		q.err = &sharedCommitError{err}
		return q.err
	}
	return nil
}

func writeSharedQueueSnapshot(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err = ensureSharedDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".requests-*")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.Remove(f.Name()); err != nil && !os.IsNotExist(err) {
			slog.Warn("remove shared snapshot temporary file", "error", err)
		}
	}()
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return syncSharedDirectory(filepath.Dir(path))
}

func (q *sharedQueue) accept(r sharedRequest) (ahead int, paused, duplicate bool, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		if old := q.uncertainAdmission; old != nil && old.Platform == r.Platform && old.Scope == r.Scope && old.MessageID == r.MessageID {
			return 0, false, false, &sharedAdmissionUncertainError{ID: old.ID, Session: old.Session, err: q.err}
		}
		return 0, false, false, q.err
	}
	for _, old := range q.requests {
		if old.Platform == r.Platform && old.Scope == r.Scope && old.MessageID == r.MessageID {
			return 0, false, true, nil
		}
		if old.Session.ID == r.Session.ID && !sharedTerminal(old.Status) {
			ahead++
			if old.Status == "interrupted" || old.Status == "stopping" || old.Status == "stopped" {
				paused = true
			}
		}
	}
	r.Reply = bytes.Clone(r.Reply)
	r.Images = slices.Clone(r.Images)
	for i := range r.Images {
		r.Images[i].Data = bytes.Clone(r.Images[i].Data)
	}
	r.Files = slices.Clone(r.Files)
	for i := range r.Files {
		r.Files[i].Data = bytes.Clone(r.Files[i].Data)
	}
	r.ID = fmt.Sprintf("%s-%d", r.Session.ID, len(q.requests)+1)
	r.Status = "queued"
	next := append(append([]sharedRequest(nil), q.requests...), r)
	if err = q.save(next); err != nil {
		q.paused = true
		var commitErr *sharedCommitError
		if errors.As(err, &commitErr) {
			q.uncertainAdmission = &r
			err = &sharedAdmissionUncertainError{ID: r.ID, Session: r.Session, err: err}
		}
		return 0, false, false, err
	}
	q.requests = next
	return ahead, paused || q.paused, false, nil
}

func (q *sharedQueue) take() (int, sharedRequest, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil || q.paused {
		return 0, sharedRequest{}, false, nil
	}
	for i, r := range q.requests {
		if r.Status != "queued" {
			continue
		}
		blocked := false
		for _, old := range q.requests[:i] {
			if old.Session.ID == r.Session.ID {
				if !sharedTerminal(old.Status) {
					blocked = true
					break
				}
				if old.HistoryID != "" {
					r.HistoryID = old.HistoryID
				}
			}
		}
		if blocked {
			continue
		}
		if !claimSharedWorkspace(r.WorkDir, r.ID) {
			continue
		}
		next := append([]sharedRequest(nil), q.requests...)
		r.Status = "running"
		next[i] = r
		if err := q.save(next); err != nil {
			q.paused = true
			q.requests[i].Status = "interrupted"
			q.requests[i].ExitConfirmed = true // No executor was launched after the failed checkpoint.
			return i, r, false, err
		}
		q.requests = next
		return i, r, true, nil
	}
	return 0, sharedRequest{}, false, nil
}

func (q *sharedQueue) finish(index int, history, result string, success bool, exited bool) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	next := append([]sharedRequest(nil), q.requests...)
	r := &next[index]
	if history != "" {
		r.HistoryID = history
	}
	r.Result = result
	r.Waiting = false
	stopped := r.Status == "stopping"
	r.ExitConfirmed = exited
	r.Status = "interrupted"
	if stopped && exited {
		r.Status = "stopped"
		success = false
	}
	if success {
		r.Status = "completed"
	}
	if err := q.save(next); err != nil {
		q.requests[index].Status = "interrupted"
		q.requests[index].ExitConfirmed = exited
		if history != "" {
			q.requests[index].HistoryID = history
		}
		q.requests[index].Result = result
		q.paused = true
		return err
	}
	q.requests = next
	allCompleted := true
	for _, request := range next {
		if !sharedTerminal(request.Status) {
			allCompleted = false
			break
		}
	}
	if allCompleted && !q.paused {
		if err := q.clearGuard(); err != nil {
			slog.Warn("clear completed shared writer guard", "error", err)
		}
	}
	if exited {
		releaseSharedWorkspace(r.WorkDir, r.ID)
	}
	return nil
}

func syncSharedDirectory(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	return syncAndCloseSharedFile(d)
}

func (q *sharedQueue) close() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.guarded || q.paused || q.err != nil {
		return nil
	}
	return q.clearGuard()
}

func (q *sharedQueue) clearGuard() error {
	if err := os.Remove(q.path + ".guard"); err != nil {
		return fmt.Errorf("remove shared writer guard: %w", err)
	}
	q.guarded = false
	return syncSharedDirectory(filepath.Dir(q.path))
}

// Sync newly created directory entries too, not just files inside those directories.
func ensureSharedDirectory(path string) error {
	var created []string
	for current := path; ; current = filepath.Dir(current) {
		if _, err := os.Stat(current); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return err
		}
		created = append(created, current)
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	for _, dir := range created {
		if err := syncSharedDirectory(filepath.Dir(dir)); err != nil {
			return err
		}
	}
	return nil
}

func syncAndCloseSharedFile(f *os.File) error {
	syncErr := f.Sync()
	closeErr := f.Close()
	if syncErr != nil {
		syncErr = fmt.Errorf("sync shared state: %w", syncErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("close shared state: %w", closeErr)
	}
	return errors.Join(syncErr, closeErr)
}

func sharedTerminal(status string) bool { return status == "completed" || status == "cancelled" }
