//go:build full_plugins && !no_wps_agentspace

package main

import (
	_ "github.com/chenhg5/cc-connect/platform/wps-agentspace"
)
