package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxChecksumsBytes     = 256 << 10
	maxArchiveBytes       = 128 << 20
	maxBinaryBytes        = 128 << 20
	maxTarDecompressBytes = maxBinaryBytes + (1 << 20)
	preflightTimeout      = 5 * time.Second

	checksumsMirrorTimeout = 15 * time.Second
	checksumsDirectTimeout = 30 * time.Second
	archiveMirrorTimeout   = 5 * time.Minute
	archiveDirectTimeout   = 10 * time.Minute
)

type applyJob struct {
	targetVersion     string
	assetName         string
	assetSize         int64
	assetURL          string
	checksumsURL      string
	mirrorURL         string
	targetPath        string
	managedBinDir     string
	dataDir           string
	restartGraceDelay time.Duration
	renameFn          func(oldpath, newpath string) error
}

// StartApply validates eligibility and launches the asynchronous download, verification,
// pre-flight test, and atomic binary swap pipeline using lifecycleCtx.
func (s *Service) StartApply(lifecycleCtx context.Context) (Status, error) {
	if lifecycleCtx == nil {
		lifecycleCtx = context.Background()
	}

	s.mu.RLock()
	if s.isBusyLocked() {
		s.mu.RUnlock()
		return Status{}, ErrBusy
	}
	s.mu.RUnlock()

	cfg := s.loadSettings(lifecycleCtx)
	env := s.evaluateEnvironmentEligibility()

	s.mu.Lock()
	if s.isBusyLocked() {
		s.mu.Unlock()
		return Status{}, ErrBusy
	}
	if !s.updateAvailable {
		s.mu.Unlock()
		return Status{}, fmt.Errorf("%w: no update available", ErrCannotApply)
	}
	_, canApply, cannotReason := s.applyReleaseEligibility(env, s.updateAvailable, s.assetURL, s.checksumsURL)
	if !canApply {
		s.mu.Unlock()
		return Status{}, fmt.Errorf("%w: %s", ErrCannotApply, cannotReason)
	}
	if env.targetPath == "" || s.assetName == "" || s.latestVersion == "" {
		s.mu.Unlock()
		return Status{}, fmt.Errorf("%w: incomplete release or target path metadata", ErrCannotApply)
	}
	if err := lifecycleCtx.Err(); err != nil {
		s.mu.Unlock()
		return Status{}, err
	}

	s.state = stateDownloading
	s.targetVersion = s.latestVersion
	s.progressBytes = 0
	s.totalBytes = 0
	s.progressPercent = 0
	s.lastError = ""

	job := applyJob{
		targetVersion:     s.latestVersion,
		assetName:         s.assetName,
		assetSize:         s.assetSize,
		assetURL:          s.assetURL,
		checksumsURL:      s.checksumsURL,
		mirrorURL:         cfg.mirrorURL,
		targetPath:        env.targetPath,
		managedBinDir:     s.managedBinDir,
		dataDir:           s.dataDir,
		restartGraceDelay: s.restartGraceDelay,
		renameFn:          s.renameFn,
	}
	st, assetURL, checksumsURL := s.snapshotStatusLocked(cfg)
	s.wg.Add(1)
	go s.runApply(lifecycleCtx, job)
	s.mu.Unlock()

	return s.finalizeStatus(st, env, assetURL, checksumsURL), nil
}

func (s *Service) runApply(ctx context.Context, job applyJob) {
	defer s.wg.Done()

	if err := s.executeApply(ctx, job); err != nil {
		s.mu.Lock()
		s.state = stateFailed
		s.lastError = err.Error()
		s.mu.Unlock()
	}
}

func (s *Service) executeApply(ctx context.Context, job applyJob) error {
	if job.assetName == "" || filepath.Base(job.assetName) != job.assetName || strings.Contains(job.assetName, "..") || strings.ContainsAny(job.assetName, "/\\") {
		return fmt.Errorf("invalid asset filename %q", job.assetName)
	}

	stagingBase := job.dataDir
	if stagingBase == "" {
		stagingBase = filepath.Join(os.TempDir(), "coursefin-updates")
	}
	stagingDir := filepath.Join(stagingBase, "updates", "staging")
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}
	stagingPath := filepath.Join(stagingDir, job.assetName+".part")
	defer func() {
		_ = os.Remove(stagingPath)
	}()

	expectedHex, err := s.fetchExpectedChecksum(ctx, job)
	if err != nil {
		return err
	}

	actualHex, downloadedBytes, err := s.downloadArchive(ctx, job, stagingPath)
	if err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	s.setState(stateVerifying)

	if subtle.ConstantTimeCompare([]byte(actualHex), []byte(expectedHex)) != 1 {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", job.assetName, expectedHex, actualHex)
	}

	targetDir := filepath.Dir(job.targetPath)
	if job.managedBinDir != "" {
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return fmt.Errorf("create managed bin dir: %w", err)
		}
	}

	candidatePath, err := extractCoursefinBinary(stagingPath, targetDir)
	if err != nil {
		return err
	}
	renamed := false
	defer func() {
		if !renamed && candidatePath != "" {
			_ = os.Remove(candidatePath)
		}
	}()

	if err := runVersionPreflight(ctx, candidatePath, job.targetVersion); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	s.setState(stateApplying)

	backupPath := job.targetPath + ".bak"
	if _, statErr := os.Stat(job.targetPath); statErr == nil {
		if err := copyFileAtomic(job.targetPath, backupPath, 0o755); err != nil {
			return fmt.Errorf("backup existing binary: %w", err)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("stat target binary: %w", statErr)
	}

	var pendingPath string
	if job.managedBinDir != "" {
		pendingPath = filepath.Join(job.managedBinDir, ".pending_update")
		if err := writeFileAtomic(pendingPath, []byte("tried\n"), 0o644); err != nil {
			return fmt.Errorf("write pending update marker: %w", err)
		}
	}

	rename := job.renameFn
	if rename == nil {
		rename = os.Rename
	}
	if err := rename(candidatePath, job.targetPath); err != nil {
		if pendingPath != "" {
			_ = os.Remove(pendingPath)
		}
		return fmt.Errorf("replace target binary: %w", err)
	}
	renamed = true
	syncDirBestEffort(targetDir)

	_ = os.Remove(stagingPath)

	s.mu.Lock()
	s.state = stateRestarting
	s.progressPercent = 100
	if downloadedBytes > 0 {
		s.progressBytes = downloadedBytes
		if s.totalBytes <= 0 {
			s.totalBytes = downloadedBytes
		}
	}
	s.mu.Unlock()

	if job.restartGraceDelay > 0 {
		timer := time.NewTimer(job.restartGraceDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	} else if err := ctx.Err(); err != nil {
		return err
	}

	select {
	case s.restartCh <- RestartRequest{
		ExecPath:   job.targetPath,
		BackupPath: backupPath,
	}:
	default:
	}
	return nil
}

func (s *Service) setState(state string) {
	s.mu.Lock()
	s.state = state
	s.mu.Unlock()
}

func (s *Service) fetchExpectedChecksum(ctx context.Context, job applyJob) (string, error) {
	resp, _, err := s.httpGetWithMirrorFallback(
		ctx,
		job.checksumsURL,
		job.mirrorURL,
		"text/plain, */*",
		false,
		"",
		"",
		checksumsMirrorTimeout,
		checksumsDirectTimeout,
		func(status int) bool {
			return status == http.StatusOK
		},
	)
	if err != nil {
		return "", fmt.Errorf("download checksums.txt: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("download checksums.txt returned HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxChecksumsBytes+1))
	if err != nil {
		return "", fmt.Errorf("read checksums.txt: %w", err)
	}
	if len(data) > maxChecksumsBytes {
		return "", fmt.Errorf("checksums.txt exceeds maximum size (%d bytes)", maxChecksumsBytes)
	}

	return parseChecksumForAsset(data, job.assetName)
}

func parseChecksumForAsset(data []byte, assetName string) (string, error) {
	var matched string
	lines := strings.Split(string(data), "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		hexDigest := fields[0]
		name := strings.TrimPrefix(fields[1], "*")
		name = strings.TrimPrefix(name, "./")
		if name != assetName {
			continue
		}
		if len(hexDigest) != 64 {
			return "", fmt.Errorf("malformed SHA-256 digest for %s", assetName)
		}
		decoded, err := hex.DecodeString(hexDigest)
		if err != nil || len(decoded) != 32 {
			return "", fmt.Errorf("invalid SHA-256 hex digest for %s", assetName)
		}
		normalized := hex.EncodeToString(decoded)
		if matched != "" && matched != normalized {
			return "", fmt.Errorf("conflicting checksum entries for %s", assetName)
		}
		matched = normalized
	}
	if matched == "" {
		return "", fmt.Errorf("checksum entry missing for %s", assetName)
	}
	return matched, nil
}

func (s *Service) downloadArchive(ctx context.Context, job applyJob, stagingPath string) (string, int64, error) {
	resp, _, err := s.httpGetWithMirrorFallback(
		ctx,
		job.assetURL,
		job.mirrorURL,
		"application/octet-stream, */*",
		false,
		"",
		"",
		archiveMirrorTimeout,
		archiveDirectTimeout,
		func(status int) bool {
			return status == http.StatusOK
		},
	)
	if err != nil {
		return "", 0, fmt.Errorf("download release archive: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return "", 0, fmt.Errorf("download release archive returned HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxArchiveBytes {
		return "", 0, fmt.Errorf("release archive exceeds maximum size (%d bytes)", maxArchiveBytes)
	}

	totalBytes := resp.ContentLength
	if totalBytes <= 0 && job.assetSize > 0 {
		totalBytes = job.assetSize
	}

	s.mu.Lock()
	s.totalBytes = totalBytes
	s.mu.Unlock()

	stagingFile, err := os.OpenFile(stagingPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", 0, fmt.Errorf("create staging archive: %w", err)
	}

	cleanupFile := true
	defer func() {
		if cleanupFile {
			_ = stagingFile.Close()
		}
	}()

	hasher := sha256.New()
	buf := make([]byte, 32*1024)
	limited := io.LimitReader(resp.Body, maxArchiveBytes+1)
	var written int64
	lastPct := -1
	lastProgressAt := time.Time{}

	for {
		n, readErr := limited.Read(buf)
		if n > 0 {
			written += int64(n)
			if written > maxArchiveBytes {
				return "", 0, fmt.Errorf("release archive exceeds maximum size (%d bytes)", maxArchiveBytes)
			}
			if _, writeErr := stagingFile.Write(buf[:n]); writeErr != nil {
				return "", 0, fmt.Errorf("write staging archive: %w", writeErr)
			}
			_, _ = hasher.Write(buf[:n])

			pct := computeProgressPercent(written, totalBytes)
			now := time.Now()
			if pct != lastPct || lastProgressAt.IsZero() || now.Sub(lastProgressAt) >= 50*time.Millisecond {
				s.mu.Lock()
				s.progressBytes = written
				s.progressPercent = pct
				s.mu.Unlock()
				lastPct = pct
				lastProgressAt = now
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", 0, fmt.Errorf("read release archive: %w", readErr)
		}
	}

	s.mu.Lock()
	s.progressBytes = written
	s.progressPercent = computeProgressPercent(written, totalBytes)
	s.mu.Unlock()

	if err := stagingFile.Sync(); err != nil {
		return "", 0, fmt.Errorf("sync staging archive: %w", err)
	}
	cleanupFile = false
	if err := stagingFile.Close(); err != nil {
		return "", 0, fmt.Errorf("close staging archive: %w", err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), written, nil
}

func computeProgressPercent(written, total int64) int {
	if total <= 0 {
		return 0
	}
	pct := int((written * 100) / total)
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

func extractCoursefinBinary(archivePath, targetDir string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("open staging archive: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("open gzip stream: %w", err)
	}
	defer gz.Close()

	limitedGz := &countingLimitReader{r: gz, limit: maxTarDecompressBytes}
	tr := tar.NewReader(limitedGz)

	var candidatePath string
	cleanupCandidate := true
	defer func() {
		if cleanupCandidate && candidatePath != "" {
			_ = os.Remove(candidatePath)
		}
	}()

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read tar entry: %w", err)
		}

		cleaned, err := validateTarEntryPath(hdr.Name)
		if err != nil {
			return "", err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg, tar.TypeRegA:
		default:
			return "", fmt.Errorf("unsafe tar entry %q: unsupported type %q", hdr.Name, hdr.Typeflag)
		}

		if hdr.Size < 0 || hdr.Size > maxBinaryBytes {
			return "", fmt.Errorf("tar entry %q exceeds maximum size (%d bytes)", hdr.Name, maxBinaryBytes)
		}

		if cleaned != "coursefin" && path.Base(cleaned) != "coursefin" {
			continue
		}
		if candidatePath != "" {
			return "", fmt.Errorf("multiple coursefin binaries found in release archive")
		}

		candidateFile, err := os.CreateTemp(targetDir, ".coursefin.new.*")
		if err != nil {
			return "", fmt.Errorf("create candidate binary: %w", err)
		}
		candidatePath = candidateFile.Name()

		written, copyErr := io.Copy(candidateFile, io.LimitReader(tr, maxBinaryBytes+1))
		if copyErr != nil {
			_ = candidateFile.Close()
			return "", fmt.Errorf("extract candidate binary: %w", copyErr)
		}
		if written > maxBinaryBytes || written != hdr.Size {
			_ = candidateFile.Close()
			return "", fmt.Errorf("extracted binary size mismatch: expected %d, got %d", hdr.Size, written)
		}
		if err := candidateFile.Chmod(0o755); err != nil {
			_ = candidateFile.Close()
			return "", fmt.Errorf("chmod candidate binary: %w", err)
		}
		if err := candidateFile.Sync(); err != nil {
			_ = candidateFile.Close()
			return "", fmt.Errorf("sync candidate binary: %w", err)
		}
		if err := candidateFile.Close(); err != nil {
			return "", fmt.Errorf("close candidate binary: %w", err)
		}
	}

	if candidatePath == "" {
		return "", fmt.Errorf("coursefin binary not found in release archive")
	}

	cleanupCandidate = false
	return candidatePath, nil
}

func validateTarEntryPath(raw string) (string, error) {
	if raw == "" || strings.Contains(raw, "\\") || strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("unsafe tar entry path %q", raw)
	}
	for _, seg := range strings.Split(raw, "/") {
		if seg == ".." {
			return "", fmt.Errorf("unsafe tar entry path traversal %q", raw)
		}
	}
	cleaned := path.Clean(raw)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || path.IsAbs(cleaned) {
		return "", fmt.Errorf("unsafe tar entry path %q", raw)
	}
	return cleaned, nil
}

func runVersionPreflight(ctx context.Context, candidatePath, targetVersion string) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, preflightTimeout)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, candidatePath, "--version")
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		pathEnv = "/usr/local/bin:/usr/bin:/bin"
	}
	cmd.Env = []string{"PATH=" + pathEnv}
	cmd.WaitDelay = time.Second

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitedBuffer{buf: &stdout, max: 4096}
	cmd.Stderr = &limitedBuffer{buf: &stderr, max: 4096}

	if err := cmd.Run(); err != nil {
		errOut := strings.TrimSpace(stderr.String())
		if errOut != "" {
			return fmt.Errorf("candidate --version preflight failed: %w (%s)", err, errOut)
		}
		return fmt.Errorf("candidate --version preflight failed: %w", err)
	}

	gotVer := NormalizeVersion(stdout.String())
	wantVer := NormalizeVersion(targetVersion)
	if !IsValidSemVer(gotVer) || gotVer != wantVer {
		return fmt.Errorf("candidate --version mismatch: expected %q, got %q", wantVer, gotVer)
	}
	return nil
}

func copyFileAtomic(srcPath, dstPath string, mode os.FileMode) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	return writeTempAndRename(dstPath, mode, func(tmp *os.File) error {
		_, err := io.Copy(tmp, src)
		return err
	})
}

func writeFileAtomic(dstPath string, data []byte, mode os.FileMode) error {
	return writeTempAndRename(dstPath, mode, func(tmp *os.File) error {
		_, err := tmp.Write(data)
		return err
	})
}

func writeTempAndRename(dstPath string, mode os.FileMode, writeFn func(*os.File) error) error {
	dir := filepath.Dir(dstPath)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(dstPath)+".tmp.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := writeFn(tmp); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return err
	}
	cleanup = false
	syncDirBestEffort(dir)
	return nil
}

func syncDirBestEffort(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

type countingLimitReader struct {
	r     io.Reader
	limit int64
	read  int64
}

func (c *countingLimitReader) Read(p []byte) (int, error) {
	if c.read >= c.limit {
		var extra [1]byte
		n, err := c.r.Read(extra[:])
		if n > 0 {
			return 0, fmt.Errorf("decompressed archive exceeds maximum size (%d bytes)", c.limit)
		}
		if err != nil {
			return 0, err
		}
		return 0, nil
	}
	rem := c.limit - c.read
	if int64(len(p)) > rem {
		p = p[:rem]
	}
	n, err := c.r.Read(p)
	c.read += int64(n)
	return n, err
}

type limitedBuffer struct {
	buf *bytes.Buffer
	max int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	remaining := l.max - l.buf.Len()
	if remaining > 0 {
		chunk := p
		if len(chunk) > remaining {
			chunk = chunk[:remaining]
		}
		_, _ = l.buf.Write(chunk)
	}
	return len(p), nil
}
