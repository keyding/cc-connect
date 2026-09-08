//go:build !unix

package core

import "context"

func sharedExecutorExited(group int) bool { return false }

func waitSharedExecutorGroup(ctx context.Context, group int) error { return nil }
