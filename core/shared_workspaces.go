package core

import "sync"

// Reservations belong to requests. Resolving an older exited request must never
// release another request's live executor in the same directory. Recovery may
// retain several uncertain owners, all of which must settle before a new claim.
var sharedWorkspaces = struct {
	sync.Mutex
	held map[string]map[string]bool
}{held: map[string]map[string]bool{}}

func claimSharedWorkspace(dir, id string) bool {
	sharedWorkspaces.Lock()
	defer sharedWorkspaces.Unlock()
	if len(sharedWorkspaces.held[dir]) != 0 {
		return false
	}
	sharedWorkspaces.held[dir] = map[string]bool{id: true}
	return true
}

func retainSharedWorkspace(dir, id string) {
	sharedWorkspaces.Lock()
	defer sharedWorkspaces.Unlock()
	if sharedWorkspaces.held[dir] == nil {
		sharedWorkspaces.held[dir] = map[string]bool{}
	}
	sharedWorkspaces.held[dir][id] = true
}

func releaseSharedWorkspace(dir, id string) {
	sharedWorkspaces.Lock()
	defer sharedWorkspaces.Unlock()
	delete(sharedWorkspaces.held[dir], id)
	if len(sharedWorkspaces.held[dir]) == 0 {
		delete(sharedWorkspaces.held, dir)
	}
}
