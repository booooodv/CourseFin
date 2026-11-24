//go:build !linux && !darwin

package updater

import (
	"fmt"
	"runtime"
)

// SupportsHotExec reports whether this platform supports replacing the running
// process image in place.
func SupportsHotExec() bool {
	return false
}

// ExecReplace returns ErrCannotApply on platforms without syscall.Exec support.
func ExecReplace(binPath string, args, env []string) error {
	return fmt.Errorf("%w: hot exec is not supported on %s", ErrCannotApply, runtime.GOOS)
}
