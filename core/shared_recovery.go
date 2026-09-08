package core

// Recovery controls always bind the interrupted request, never a mutable default.
// Resolving acknowledges abandonment, not successful execution or rollback.
func (e *Engine) sharedRecoveryLocked(q *sharedQueue, scope sharedScope, msg *Message, command string, args []string) (string, error) {
	if len(args) == 0 {
		target := ""
		for _, r := range q.requests {
			if r.Session.ID == scope.Selections[msg.SessionKey] && r.Status == "interrupted" {
				if target != "" {
					return e.i18n.T(MsgRecoveryChoose), nil
				}
				target = r.ID
			}
		}
		if target == "" {
			return e.i18n.T(MsgRecoveryChoose), nil
		}
		args = []string{target}
	}
	for i, r := range q.requests {
		if r.ID != args[0] || r.Platform != msg.Platform || r.Scope != msg.SharedScope {
			continue
		}
		visible := false
		for _, s := range scope.Sessions {
			if s.ID == r.Session.ID {
				visible = true
			}
		}
		if !visible || r.Status != "interrupted" {
			return e.i18n.T(MsgQueueStale), nil
		}
		next := append([]sharedRequest(nil), q.requests...)
		if command == "continue" {
			if r.UserID != msg.UserID {
				return e.i18n.T(MsgRecoveryOwner), nil
			}
			if len(args) == 2 {
				if !r.ContinueWarned {
					return e.i18n.T(MsgRecoveryChoose), nil
				}
				return e.i18n.Tf(MsgRecoveryUnavailable, r.ID), nil
			}
			next[i].ContinueWarned = true
			if err := q.save(next); err != nil {
				q.paused = true
				return "", err
			}
			q.requests = next
			return e.i18n.Tf(MsgRecoveryWarning, r.Session.Name, r.UserID, r.ID, r.ID), nil
		}
		exited := r.ExitConfirmed || sharedExecutorExited(r.ExecutorGroup)
		if !exited {
			return e.i18n.T(MsgQueueBlocked), nil
		}
		next[i].ExitConfirmed = true
		next[i].Status = "stopped"
		if err := q.save(next); err != nil {
			q.paused = true
			return "", err
		}
		q.requests = next
		q.paused = false
		releaseSharedWorkspace(r.WorkDir, r.ID)
		return e.i18n.Tf(MsgRecoveryResolved, r.Session.Name, r.UserID, r.ID, r.ID), nil
	}
	return e.i18n.T(MsgQueueStale), nil
}
