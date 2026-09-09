package core

import (
	"context"
	"sync"
)

type sharedSettingsAgent struct {
	queueTestAgent
	settingsMu          sync.Mutex
	model, mode, effort string
	started             chan string
}

func (a *sharedSettingsAgent) GetModel() string {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	return a.model
}
func (a *sharedSettingsAgent) SetModel(s string) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	a.model = s
}
func (a *sharedSettingsAgent) AvailableModels(context.Context) []ModelOption {
	return []ModelOption{{Name: "first"}, {Name: "second"}}
}
func (a *sharedSettingsAgent) GetMode() string {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	return a.mode
}
func (a *sharedSettingsAgent) SetMode(s string) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	a.mode = s
}
func (*sharedSettingsAgent) PermissionModes() []PermissionModeInfo {
	return []PermissionModeInfo{{Key: "default", Name: "Default"}, {Key: "plan", Name: "Plan"}}
}
func (a *sharedSettingsAgent) GetReasoningEffort() string {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	return a.effort
}
func (a *sharedSettingsAgent) SetReasoningEffort(s string) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	a.effort = s
}
func (*sharedSettingsAgent) AvailableReasoningEfforts() []string { return []string{"low", "high"} }
func (a *sharedSettingsAgent) StartSession(ctx context.Context, id string) (AgentSession, error) {
	a.settingsMu.Lock()
	settings := a.model + "/" + a.mode + "/" + a.effort
	a.settingsMu.Unlock()
	a.started <- settings
	return a.queueTestAgent.StartSession(ctx, id)
}
