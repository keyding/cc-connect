package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// sharedSession is a registered conversation, independent of its selection entry
// and future agent history ID. The directory slice currently creates drafts only.
type sharedSession struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AgentType string `json:"agent_type"`
}

type sharedScope struct {
	Sessions   []sharedSession   `json:"sessions"`
	Selections map[string]string `json:"selections"`
}

type sharedDirectoryState struct {
	InteractionLinks map[string]bool        `json:"interaction_links,omitempty"`
	Links            map[string]string      `json:"message_links,omitempty"`
	Version          int                    `json:"version"`
	Scopes           map[string]sharedScope `json:"scopes"`
}

// One engine owns this file. Serialize read/check/write/publication so concurrent
// names cannot both succeed and failed writes never publish in-memory changes.
type sharedDirectory struct {
	mu      sync.Mutex
	path    string
	state   sharedDirectoryState
	loadErr error
}

func newSharedDirectory(sessionPath string) *sharedDirectory {
	d := &sharedDirectory{state: sharedDirectoryState{Version: 1, Scopes: map[string]sharedScope{}}}
	if sessionPath == "" {
		return d
	}
	d.path = sessionPath + ".shared.json"
	data, err := os.ReadFile(d.path)
	if os.IsNotExist(err) {
		return d
	}
	if err == nil {
		err = json.Unmarshal(data, &d.state)
	}
	if err == nil && (d.state.Version != 1 || d.state.Scopes == nil) {
		err = fmt.Errorf("unsupported shared directory state")
	}
	d.loadErr = err
	return d
}

func (d *sharedDirectory) save(state sharedDirectoryState) error {
	if d.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err = ensureSharedDirectory(filepath.Dir(d.path)); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(d.path), ".shared-*")
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
	if err := os.Rename(f.Name(), d.path); err != nil {
		return err
	}
	return syncSharedDirectory(filepath.Dir(d.path))
}

// English case folding only: other Unicode letters remain distinct per spec.
func sharedNameKey(name string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, strings.TrimSpace(name))
}

func sharedNameTaken(scope sharedScope, name, exceptID string) bool {
	for _, s := range scope.Sessions {
		if s.ID != exceptID && sharedNameKey(s.Name) == sharedNameKey(name) {
			return true
		}
	}
	return false
}

func (d *sharedDirectory) apply(scopeKey, entry, agent, command, arg string) (sharedScope, MsgKey, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.loadErr != nil {
		return sharedScope{}, "", fmt.Errorf("load shared directory: %w", d.loadErr)
	}
	original := d.state.Scopes[scopeKey]
	scope := sharedScope{Sessions: append([]sharedSession(nil), original.Sessions...), Selections: map[string]string{}}
	for k, v := range original.Selections {
		scope.Selections[k] = v
	}
	if command != "new" && command != "switch" && command != "name" {
		return scope, "", nil
	}
	if hint, err := scope.change(entry, agent, command, arg); hint != "" || err != nil {
		return scope, hint, err
	}
	next := sharedDirectoryState{Version: 1, Scopes: map[string]sharedScope{}, Links: d.state.Links, InteractionLinks: d.state.InteractionLinks}
	for k, v := range d.state.Scopes {
		next.Scopes[k] = v
	}
	next.Scopes[scopeKey] = scope
	if err := d.save(next); err != nil {
		return original, "", fmt.Errorf("save shared directory: %w", err)
	}
	d.state = next
	return scope, "", nil
}

func (scope *sharedScope) change(entry, agent, command, arg string) (MsgKey, error) {
	arg = strings.TrimSpace(arg)
	active := -1
	for i, s := range scope.Sessions {
		if s.ID == scope.Selections[entry] {
			active = i
		}
	}
	switch command {
	case "new":
		if arg != "" && sharedNameTaken(*scope, arg, "") {
			return MsgSharedNameConflict, nil
		}
		var bytes [16]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			return "", fmt.Errorf("create shared identity: %w", err)
		}
		id := hex.EncodeToString(bytes[:])
		if arg == "" {
			for n := len(scope.Sessions) + 1; ; n++ {
				arg = fmt.Sprintf("session-%d", n)
				if !sharedNameTaken(*scope, arg, "") {
					break
				}
			}
		}
		scope.Sessions = append(scope.Sessions, sharedSession{ID: id, Name: arg, AgentType: agent})
		scope.Selections[entry] = id
	case "switch":
		target := sharedSessionIndex(scope.Sessions, arg)
		if target < 0 {
			return MsgSharedNotFound, nil
		}
		scope.Selections[entry] = scope.Sessions[target].ID
	case "name":
		if active < 0 {
			return MsgSharedChoose, nil
		}
		if arg == "" {
			return MsgSharedNameUsage, nil
		}
		if sharedNameTaken(*scope, arg, scope.Sessions[active].ID) {
			return MsgSharedNameConflict, nil
		}
		scope.Sessions[active].Name = arg
	default:
		return "", nil
	}

	return "", nil
}

// sharedSessionIndex keeps command selectors aligned with the displayed list.
func sharedSessionIndex(sessions []sharedSession, selector string) int {
	if n, err := strconv.Atoi(selector); err == nil && n > 0 && n <= len(sessions) {
		return n - 1
	}
	for i, s := range sessions {
		if s.ID == selector || sharedNameKey(s.Name) == sharedNameKey(selector) {
			return i
		}
	}
	return -1
}
