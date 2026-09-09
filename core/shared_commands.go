package core

import "strings"

var sharedCommandSpecs = []struct {
	name        string
	description MsgKey
}{
	{"new", MsgSharedCmdNew},
	{"list", MsgSharedCmdList},
	{"switch", MsgSharedCmdSwitch},
	{"name", MsgSharedCmdName},
	{"current", MsgSharedCmdCurrent},
	{"history", MsgSharedCmdHistory},
	{"queue", MsgSharedCmdQueue},
	{"cancel", MsgSharedCmdCancel},
	{"stop", MsgSharedCmdStop},
	{"resume", MsgSharedCmdResume},
	{"resolve", MsgSharedCmdResolve},
	{"continue", MsgSharedCmdContinue},
	{"delete", MsgSharedCmdDelete},
	{"approve", MsgSharedCmdApprove},
	{"deny", MsgSharedCmdDeny},
	{"answer", MsgSharedCmdAnswer},
	{"model", MsgSharedCmdModel},
	{"mode", MsgSharedCmdMode},
	{"reasoning", MsgSharedCmdReasoning},
	{"usage", MsgSharedCmdUsage},
	{"lang", MsgSharedCmdLang},
	{"version", MsgSharedCmdVersion},
	{"whoami", MsgSharedCmdWhoami},
	{"help", MsgSharedCmdHelp},
}

func isSharedCommand(name string) bool {
	for _, cmd := range sharedCommandSpecs {
		if cmd.name == name {
			return true
		}
	}
	return false
}
func (e *Engine) sharedMenuCommands() []BotCommandInfo {
	e.userRolesMu.RLock()
	defer e.userRolesMu.RUnlock()
	var commands []BotCommandInfo
	for _, cmd := range sharedCommandSpecs {
		if !e.disabledCmds[cmd.name] && cmd.name != "approve" && cmd.name != "deny" && cmd.name != "answer" {
			commands = append(commands, BotCommandInfo{Command: cmd.name, Description: e.i18n.T(cmd.description)})
		}
	}
	return commands
}
func (e *Engine) sharedCommandHelp() string {
	lines := []string{e.i18n.T(MsgSharedCommands)}
	for _, cmd := range e.sharedMenuCommands() {
		lines = append(lines, "/"+cmd.Command+" — "+cmd.Description)
	}
	return strings.Join(lines, "\n")
}
