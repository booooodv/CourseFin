//go:build linux || darwin

package updater

import (
	"os"
	"syscall"
)

// SupportsHotExec reports whether this platform supports replacing the running
// process image in place via syscall.Exec.
func SupportsHotExec() bool {
	return true
}

// ExecReplace replaces the current process image with binPath.
func ExecReplace(binPath string, args, env []string) error {
	if len(args) == 0 {
		args = []string{binPath}
	}
	if env == nil {
		env = os.Environ()
	}
	return syscall.Exec(binPath, args, env)
}
