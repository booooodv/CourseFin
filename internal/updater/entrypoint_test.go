package updater_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDockerEntrypoint(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell entrypoint test skipped on Windows")
	}

	entrypointScript := filepath.Join("..", "..", "scripts", "docker-entrypoint.sh")

	writeFakeBinary := func(t *testing.T, path, label, version string, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		script := fmt.Sprintf(`#!/bin/sh
if [ "${1:-}" = "--version" ]; then
	printf '%%s\n' %q
	exit 0
fi
printf 'RUNNING:%s:%%s\n' "${COURSEFIN_MANAGED_BIN_DIR:-}"
if [ "$#" -gt 0 ]; then
	printf 'ARGS:%%s\n' "$*"
fi
`, version, label)
		if err := os.WriteFile(path, []byte(script), mode); err != nil {
			t.Fatalf("write fake binary %s: %v", path, err)
		}
	}

	writeFailingBinary := func(t *testing.T, path, output string, exitCode int, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		script := fmt.Sprintf(`#!/bin/sh
if [ "${1:-}" = "--version" ]; then
	printf '%%s\n' %q
	exit %d
fi
printf 'RUNNING:corrupt:%%s\n' "${COURSEFIN_MANAGED_BIN_DIR:-}"
`, output, exitCode)
		if err := os.WriteFile(path, []byte(script), mode); err != nil {
			t.Fatalf("write failing binary %s: %v", path, err)
		}
	}

	runEntrypoint := func(t *testing.T, imageBin, dataDir string, args ...string) string {
		t.Helper()
		cmdArgs := append([]string{entrypointScript}, args...)
		cmd := exec.Command("sh", cmdArgs...)
		cmd.Env = []string{
			"PATH=" + os.Getenv("PATH"),
			"COURSEFIN_IMAGE_BIN=" + imageBin,
			"COURSEFIN_DATA_DIR=" + dataDir,
		}
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("entrypoint failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		}
		return strings.TrimSpace(stdout.String())
	}

	t.Run("1_fresh_data_runs_image_binary", func(t *testing.T) {
		tmp := t.TempDir()
		imageBin := filepath.Join(tmp, "image", "coursefin")
		dataDir := filepath.Join(tmp, "data")
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			t.Fatalf("mkdir dataDir: %v", err)
		}
		writeFakeBinary(t, imageBin, "image", "1.0.1", 0o755)

		out := runEntrypoint(t, imageBin, dataDir, "--listen", "8096")
		expectedBinDir := filepath.Join(dataDir, "updates", "bin")
		want := "RUNNING:image:" + expectedBinDir + "\nARGS:--listen 8096"
		if out != want {
			t.Fatalf("expected output %q, got %q", want, out)
		}
	})

	t.Run("2_active_newer_than_image_runs_active_binary", func(t *testing.T) {
		tmp := t.TempDir()
		imageBin := filepath.Join(tmp, "image", "coursefin")
		dataDir := filepath.Join(tmp, "data")
		binDir := filepath.Join(dataDir, "updates", "bin")
		activeBin := filepath.Join(binDir, "coursefin")

		writeFakeBinary(t, imageBin, "image", "1.0.1", 0o755)
		writeFakeBinary(t, activeBin, "active", "1.2.0", 0o755)

		out := runEntrypoint(t, imageBin, dataDir)
		want := "RUNNING:active:" + binDir
		if out != want {
			t.Fatalf("expected output %q, got %q", want, out)
		}
	})

	t.Run("2b_numeric_semver_comparison_1_10_0_gt_1_2_0", func(t *testing.T) {
		tmp := t.TempDir()
		imageBin := filepath.Join(tmp, "image", "coursefin")
		dataDir := filepath.Join(tmp, "data")
		binDir := filepath.Join(dataDir, "updates", "bin")
		activeBin := filepath.Join(binDir, "coursefin")

		writeFakeBinary(t, imageBin, "image", "1.2.0", 0o755)
		writeFakeBinary(t, activeBin, "active", "1.10.0", 0o755)

		out := runEntrypoint(t, imageBin, dataDir)
		want := "RUNNING:active:" + binDir
		if out != want {
			t.Fatalf("expected numeric semver 1.10.0 > 1.2.0 to run active, got %q", out)
		}
	})

	t.Run("3_newer_or_equal_image_supersedes_active_binary", func(t *testing.T) {
		cases := []struct {
			name      string
			imageVer  string
			activeVer string
		}{
			{name: "image_strictly_newer", imageVer: "1.3.0", activeVer: "1.2.0"},
			{name: "image_equal_version", imageVer: "1.2.0", activeVer: "1.2.0"},
			{name: "image_with_v_prefix", imageVer: "v1.3.0", activeVer: "1.2.0"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				tmp := t.TempDir()
				imageBin := filepath.Join(tmp, "image", "coursefin")
				dataDir := filepath.Join(tmp, "data")
				binDir := filepath.Join(dataDir, "updates", "bin")
				activeBin := filepath.Join(binDir, "coursefin")

				writeFakeBinary(t, imageBin, "image", tc.imageVer, 0o755)
				writeFakeBinary(t, activeBin, "active", tc.activeVer, 0o755)

				out := runEntrypoint(t, imageBin, dataDir)
				want := "RUNNING:image:" + binDir
				if out != want {
					t.Fatalf("expected output %q, got %q", want, out)
				}
			})
		}
	})

	t.Run("4_corrupt_active_falls_back_to_valid_backup_or_image", func(t *testing.T) {
		t.Run("falls_back_to_valid_newer_backup", func(t *testing.T) {
			tmp := t.TempDir()
			imageBin := filepath.Join(tmp, "image", "coursefin")
			dataDir := filepath.Join(tmp, "data")
			binDir := filepath.Join(dataDir, "updates", "bin")
			activeBin := filepath.Join(binDir, "coursefin")
			backupBin := filepath.Join(binDir, "coursefin.bak")

			writeFakeBinary(t, imageBin, "image", "1.0.1", 0o755)
			writeFailingBinary(t, activeBin, "corrupt", 1, 0o755)
			writeFakeBinary(t, backupBin, "backup", "1.1.0", 0o755)

			out := runEntrypoint(t, imageBin, dataDir)
			want := "RUNNING:backup:" + binDir
			if out != want {
				t.Fatalf("expected fallback to backup %q, got %q", want, out)
			}

			verOut, err := exec.Command(activeBin, "--version").CombinedOutput()
			if err != nil || strings.TrimSpace(string(verOut)) != "1.1.0" {
				t.Fatalf("expected active binary restored to 1.1.0, err=%v out=%q", err, string(verOut))
			}
		})

		t.Run("falls_back_to_image_when_backup_not_newer", func(t *testing.T) {
			tmp := t.TempDir()
			imageBin := filepath.Join(tmp, "image", "coursefin")
			dataDir := filepath.Join(tmp, "data")
			binDir := filepath.Join(dataDir, "updates", "bin")
			activeBin := filepath.Join(binDir, "coursefin")
			backupBin := filepath.Join(binDir, "coursefin.bak")

			writeFakeBinary(t, imageBin, "image", "1.0.1", 0o755)
			writeFailingBinary(t, activeBin, "not-a-semver", 0, 0o755)
			writeFakeBinary(t, backupBin, "backup", "1.0.0", 0o755)

			out := runEntrypoint(t, imageBin, dataDir)
			want := "RUNNING:image:" + binDir
			if out != want {
				t.Fatalf("expected fallback to image %q, got %q", want, out)
			}
		})

		t.Run("falls_back_to_image_when_no_backup_and_active_non_exec", func(t *testing.T) {
			tmp := t.TempDir()
			imageBin := filepath.Join(tmp, "image", "coursefin")
			dataDir := filepath.Join(tmp, "data")
			binDir := filepath.Join(dataDir, "updates", "bin")
			activeBin := filepath.Join(binDir, "coursefin")

			writeFakeBinary(t, imageBin, "image", "1.0.1", 0o755)
			writeFakeBinary(t, activeBin, "active", "1.2.0", 0o644)

			out := runEntrypoint(t, imageBin, dataDir)
			want := "RUNNING:image:" + binDir
			if out != want {
				t.Fatalf("expected fallback to image %q, got %q", want, out)
			}
		})
	})

	t.Run("5_crash_loop_guard", func(t *testing.T) {
		t.Run("tried_marker_restores_backup_and_deletes_pending", func(t *testing.T) {
			tmp := t.TempDir()
			imageBin := filepath.Join(tmp, "image", "coursefin")
			dataDir := filepath.Join(tmp, "data")
			binDir := filepath.Join(dataDir, "updates", "bin")
			activeBin := filepath.Join(binDir, "coursefin")
			backupBin := filepath.Join(binDir, "coursefin.bak")
			pendingFile := filepath.Join(binDir, ".pending_update")

			writeFakeBinary(t, imageBin, "image", "1.0.1", 0o755)
			writeFakeBinary(t, activeBin, "active", "1.2.0", 0o755)
			writeFakeBinary(t, backupBin, "backup", "1.1.0", 0o755)
			if err := os.WriteFile(pendingFile, []byte("tried\n"), 0o644); err != nil {
				t.Fatalf("write pending file: %v", err)
			}

			out := runEntrypoint(t, imageBin, dataDir)
			want := "RUNNING:backup:" + binDir
			if out != want {
				t.Fatalf("expected restored backup execution %q, got %q", want, out)
			}

			if _, err := os.Stat(pendingFile); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("expected %s to be deleted, stat err=%v", pendingFile, err)
			}
			if _, err := os.Stat(backupBin); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("expected %s to be moved to active, stat err=%v", backupBin, err)
			}
			verOut, err := exec.Command(activeBin, "--version").CombinedOutput()
			if err != nil || strings.TrimSpace(string(verOut)) != "1.1.0" {
				t.Fatalf("expected active binary restored to 1.1.0, err=%v out=%q", err, string(verOut))
			}
		})

		t.Run("tried_marker_with_whitespace_restores_backup", func(t *testing.T) {
			tmp := t.TempDir()
			imageBin := filepath.Join(tmp, "image", "coursefin")
			dataDir := filepath.Join(tmp, "data")
			binDir := filepath.Join(dataDir, "updates", "bin")
			activeBin := filepath.Join(binDir, "coursefin")
			backupBin := filepath.Join(binDir, "coursefin.bak")
			pendingFile := filepath.Join(binDir, ".pending_update")

			writeFakeBinary(t, imageBin, "image", "1.0.1", 0o755)
			writeFakeBinary(t, activeBin, "active", "1.2.0", 0o755)
			writeFakeBinary(t, backupBin, "backup", "1.1.0", 0o755)
			if err := os.WriteFile(pendingFile, []byte("  tried \r\n"), 0o644); err != nil {
				t.Fatalf("write pending file: %v", err)
			}

			out := runEntrypoint(t, imageBin, dataDir)
			want := "RUNNING:backup:" + binDir
			if out != want {
				t.Fatalf("expected restored backup execution %q, got %q", want, out)
			}
		})

		t.Run("tried_marker_with_corrupt_backup_deletes_active_and_runs_image", func(t *testing.T) {
			tmp := t.TempDir()
			imageBin := filepath.Join(tmp, "image", "coursefin")
			dataDir := filepath.Join(tmp, "data")
			binDir := filepath.Join(dataDir, "updates", "bin")
			activeBin := filepath.Join(binDir, "coursefin")
			backupBin := filepath.Join(binDir, "coursefin.bak")
			pendingFile := filepath.Join(binDir, ".pending_update")

			writeFakeBinary(t, imageBin, "image", "1.0.1", 0o755)
			writeFakeBinary(t, activeBin, "active", "1.2.0", 0o755)
			writeFailingBinary(t, backupBin, "broken", 1, 0o755)
			if err := os.WriteFile(pendingFile, []byte("tried\n"), 0o644); err != nil {
				t.Fatalf("write pending file: %v", err)
			}

			out := runEntrypoint(t, imageBin, dataDir)
			want := "RUNNING:image:" + binDir
			if out != want {
				t.Fatalf("expected fallback to image %q, got %q", want, out)
			}
			if _, err := os.Stat(activeBin); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("expected %s to be deleted, stat err=%v", activeBin, err)
			}
			if _, err := os.Stat(pendingFile); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("expected %s to be deleted, stat err=%v", pendingFile, err)
			}
		})

		t.Run("tried_marker_without_backup_deletes_active_and_runs_image", func(t *testing.T) {
			tmp := t.TempDir()
			imageBin := filepath.Join(tmp, "image", "coursefin")
			dataDir := filepath.Join(tmp, "data")
			binDir := filepath.Join(dataDir, "updates", "bin")
			activeBin := filepath.Join(binDir, "coursefin")
			pendingFile := filepath.Join(binDir, ".pending_update")

			writeFakeBinary(t, imageBin, "image", "1.0.1", 0o755)
			writeFakeBinary(t, activeBin, "active", "1.2.0", 0o755)
			if err := os.WriteFile(pendingFile, []byte("tried\n"), 0o644); err != nil {
				t.Fatalf("write pending file: %v", err)
			}

			out := runEntrypoint(t, imageBin, dataDir)
			want := "RUNNING:image:" + binDir
			if out != want {
				t.Fatalf("expected fallback to image %q, got %q", want, out)
			}

			if _, err := os.Stat(activeBin); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("expected %s to be deleted, stat err=%v", activeBin, err)
			}
			if _, err := os.Stat(pendingFile); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("expected %s to be deleted, stat err=%v", pendingFile, err)
			}
		})

		t.Run("pending_marker_marks_tried_and_executes_active", func(t *testing.T) {
			tmp := t.TempDir()
			imageBin := filepath.Join(tmp, "image", "coursefin")
			dataDir := filepath.Join(tmp, "data")
			binDir := filepath.Join(dataDir, "updates", "bin")
			activeBin := filepath.Join(binDir, "coursefin")
			backupBin := filepath.Join(binDir, "coursefin.bak")
			pendingFile := filepath.Join(binDir, ".pending_update")

			writeFakeBinary(t, imageBin, "image", "1.0.1", 0o755)
			writeFakeBinary(t, activeBin, "active", "1.2.0", 0o755)
			writeFakeBinary(t, backupBin, "backup", "1.1.0", 0o755)
			if err := os.WriteFile(pendingFile, []byte("pending\n"), 0o644); err != nil {
				t.Fatalf("write pending file: %v", err)
			}

			out := runEntrypoint(t, imageBin, dataDir)
			want := "RUNNING:active:" + binDir
			if out != want {
				t.Fatalf("expected first boot of pending update to execute active %q, got %q", want, out)
			}

			data, err := os.ReadFile(pendingFile)
			if err != nil {
				t.Fatalf("read pending file: %v", err)
			}
			if strings.TrimSpace(string(data)) != "tried" {
				t.Fatalf("expected pending file updated to \"tried\", got %q", string(data))
			}
		})
	})
}
