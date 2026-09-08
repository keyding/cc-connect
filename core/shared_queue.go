package core

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	data, err := json.Marshal(requests)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(q.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(q.path), ".requests-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
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
		q.err = err
		return 0, false, false, err
	}
	q.requests = next
	return ahead, paused, false, nil
}

func (q *sharedQueue) take() (int, sharedRequest, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
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
			q.err = err
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
		q.err = err
		return err
	}
	q.requests = next
	if success {
		sharedWorkspaces.Lock()
		delete(sharedWorkspaces.held, r.WorkDir)
		sharedWorkspaces.Unlock()
	}
	return nil
}
