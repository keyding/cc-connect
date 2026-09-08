package core

import (
	"errors"
	"sort"
)

func pendingRouteMatches(p Platform, a, b string) bool {
	if a == b {
		return true
	}
	matcher, ok := p.(PendingSessionRouteMatcher)
	return ok && matcher.PendingSessionRouteMatches(a, b)
}

func (sm *SessionManager) pendingSessionsForRoute(p Platform, key string) []*Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	var out []*Session
	seen := map[string]bool{}
	for route, ids := range sm.userSessions {
		if !pendingRouteMatches(p, key, route) {
			continue
		}
		for _, id := range ids {
			s := sm.sessions[id]
			if s == nil || seen[id] {
				continue
			}
			seen[id] = true
			s.mu.Lock()
			eligible := s.AgentSessionID == "" && len(s.PastAgentSessionIDs) == 0 && len(s.History) == 0 && s.Name != "" && s.Name != "default" && s.Name != "session"
			s.mu.Unlock()
			if eligible {
				out = append(out, s)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (sm *SessionManager) activatePendingSession(p Platform, key, id string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	owned := false
	for route, ids := range sm.userSessions {
		if pendingRouteMatches(p, key, route) {
			for _, sid := range ids {
				if sid == id {
					owned = true
				}
			}
		}
	}
	s := sm.sessions[id]
	if !owned || s == nil {
		return errPendingSessionUnavailable
	}
	s.mu.Lock()
	valid := s.AgentSessionID == "" && len(s.PastAgentSessionIDs) == 0 && len(s.History) == 0
	s.mu.Unlock()
	if !valid {
		return errPendingSessionUnavailable
	}
	found := false
	for _, sid := range sm.userSessions[key] {
		if sid == id {
			found = true
		}
	}
	if !found {
		sm.userSessions[key] = append(sm.userSessions[key], id)
	}
	sm.activeSession[key] = id
	sm.saveLocked()
	return nil
}

// Append drafts after history so existing history numbers remain unchanged.
// Both /list and /switch must resolve against this same combined ordering.
func (e *Engine) appendPendingSessionEntries(p Platform, history []AgentSessionInfo, sm *SessionManager, key string) []AgentSessionInfo {
	entries := append([]AgentSessionInfo(nil), history...)
	for _, s := range sm.pendingSessionsForRoute(p, key) {
		label := e.i18n.T(MsgPendingSessionLabel)
		entries = append(entries, AgentSessionInfo{ID: "pending:" + s.ID, Summary: s.GetName() + label, ModifiedAt: s.CreatedAt})
	}
	return entries
}

var errPendingSessionUnavailable = errors.New("pending session unavailable")
