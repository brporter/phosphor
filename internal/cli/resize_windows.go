//go:build windows

package cli

import "context"

// watchTerminalResize is a no-op on Windows. ConPTY handles resize internally.
func watchTerminalResize(ctx context.Context, ptyProc PTYProcess) {}
