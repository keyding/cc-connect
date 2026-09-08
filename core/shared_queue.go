package core

import (
	"bytes"
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
	HistoryID, Result                                                     string
}

type sharedQueue struct {
	mu       sync.Mutex
	path     string
	requests []sharedRequest
	err      error
	running  bool
	guarded  bool
	paused   bool
}

// All engines in this process share directory exclusion. A failed/uncertain task
// retains its reservation; only a future explicit recovery operation may release it.
var sharedWorkspaces = struct {
	sync.Mutex
	held map[string]bool
}{held: map[string]bool{}}

func newSharedQueue(path string) *sharedQueue {
	q := &sharedQueue{}
	if path == "" {
		q.err = fmt.Errorf("shared requests require persistent storage")
		return q
	}
	q.path = path + ".requests.json"
	if _, err := os.Stat(q.path + ".guard"); err == nil {
		q.guarded = true
		q.paused = true
	} else if !os.IsNotExist(err) {
		q.err = err
		return q
	}
	data, err := os.ReadFile(q.path)
	if os.IsNotExist(err) {
		return q
	}
	if err == nil {
		err = json.Unmarshal(data, &q.requests)
	}
	q.err = err
	if err == nil {
		for i := range q.requests {
			r := &q.requests[i]
			if q.paused && r.Status != "completed" {
				sharedWorkspaces.Lock()
				sharedWorkspaces.held[r.WorkDir] = true
				sharedWorkspaces.Unlock()
			}
			switch r.Status {
			case "queued", "completed":
			case "running", "interrupted":
				r.Status = "interrupted"
				sharedWorkspaces.Lock()
				sharedWorkspaces.held[r.WorkDir] = true
				sharedWorkspaces.Unlock()
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
	if err := q.ensureGuard(); err != nil {
		return err
	}
	data, err := json.Marshal(requests)
	if err != nil {
		return err
	}
	if err = ensureSharedDirectory(filepath.Dir(q.path)); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(q.path), ".requests-*")
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
	if err = os.Rename(f.Name(), q.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(q.path))
	if err != nil {
		return err
	}
	err = dir.Sync()
	closeErr = dir.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func (q *sharedQueue) accept(r sharedRequest) (ahead int, paused, duplicate bool, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return 0, false, false, q.err
	}
	for _, old := range q.requests {
		if old.Platform == r.Platform && old.Scope == r.Scope && old.MessageID == r.MessageID {
			return 0, false, true, nil
		}
		if old.Session.ID == r.Session.ID && old.Status != "completed" {
			ahead++
			if old.Status == "interrupted" {
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
				if old.Status != "completed" {
					blocked = true
					break
				}
				r.HistoryID = old.HistoryID
			}
		}
		if blocked {
			continue
		}
		sharedWorkspaces.Lock()
		if sharedWorkspaces.held[r.WorkDir] {
			sharedWorkspaces.Unlock()
			continue
		}
		sharedWorkspaces.held[r.WorkDir] = true
		sharedWorkspaces.Unlock()
		next := append([]sharedRequest(nil), q.requests...)
		r.Status = "running"
		next[i] = r
		if err := q.save(next); err != nil {
			q.paused = true
			return i, r, false, err
		}
		q.requests = next
		return i, r, true, nil
	}
	return 0, sharedRequest{}, false, nil
}

func (q *sharedQueue) finish(index int, history, result string, success bool) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	next := append([]sharedRequest(nil), q.requests...)
	r := &next[index]
	r.HistoryID = history
	r.Result = result
	r.Status = "interrupted"
	if success {
		r.Status = "completed"
	}
	if err := q.save(next); err != nil {
		q.paused = true
		return err
	}
	q.requests = next
	allCompleted := true
	for _, request := range next {
		if request.Status != "completed" {
			allCompleted = false
			break
		}
	}
	if allCompleted && !q.paused {
		if err := q.clearGuard(); err != nil {
			slog.Warn("clear completed shared writer guard", "error", err)
		}
	}
	if success {
		sharedWorkspaces.Lock()
		delete(sharedWorkspaces.held, r.WorkDir)
		sharedWorkspaces.Unlock()
	}
	return nil
}

// A durable writer guard precedes every snapshot change and remains while work
// is unfinished. An unclean writer exit cannot distinguish a failed fsync from
// a committed checkpoint, so restart conservatively pauses the queue as a whole.
// Clean shutdown removes the guard only after all workers saved their outcomes.
func (q *sharedQueue) ensureGuard() error {
	if q.guarded {
		return nil
	}
	if err := ensureSharedDirectory(filepath.Dir(q.path)); err != nil {
		return err
	}
	f, err := os.OpenFile(q.path+".guard", os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("create shared writer guard: %w", err)
	}
	if err := syncAndCloseSharedFile(f); err != nil {
		return err
	}
	if err := syncSharedDirectory(filepath.Dir(q.path)); err != nil {
		return err
	}
	q.guarded = true
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
