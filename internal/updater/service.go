package updater

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultRepo              = "booooodv/CourseFin"
	defaultAPIBase           = "https://api.github.com"
	defaultCheckIntervalHrs  = 6
	minCheckIntervalHours    = 1
	maxCheckIntervalHours    = 168
	defaultInitialCheckDelay = 30 * time.Second
	defaultRestartGraceDelay = 400 * time.Millisecond

	mirrorCheckTimeout  = 10 * time.Second
	directCheckTimeout  = 15 * time.Second
	maxReleaseJSONBytes = 2 << 20

	settingKeyAutoCheck     = "update_auto_check"
	settingKeyIntervalHours = "update_check_interval_hours"
	settingKeyMirrorURL     = "update_mirror_url"

	stateIdle        = "idle"
	stateChecking    = "checking"
	stateDownloading = "downloading"
	stateVerifying   = "verifying"
	stateApplying    = "applying"
	stateRestarting  = "restarting"
	stateFailed      = "failed"
)

var (
	// ErrBusy indicates a check or apply operation is already in progress.
	ErrBusy = errors.New("updater is busy")
	// ErrInvalidConfig indicates invalid update configuration values.
	ErrInvalidConfig = errors.New("invalid update config")
	// ErrCannotApply indicates an update cannot be applied in the current environment or state.
	ErrCannotApply = errors.New("update cannot be applied")
	// ErrUpstream indicates a failure fetching or parsing release metadata from upstream.
	ErrUpstream = errors.New("upstream release check failed")
)

// Options configures a Service instance.
type Options struct {
	DB                *sql.DB
	DataDir           string
	CurrentVersion    string
	Repo              string
	APIBase           string
	GitHubToken       string
	AutoCheckEnv      *bool
	ManagedBinDir     string
	HTTPClient        *http.Client
	GOOS              string
	GOARCH            string
	ExecutablePath    func() (string, error)
	InitialCheckDelay time.Duration
	RestartGraceDelay time.Duration
}

// Status represents the serialized self-update state exposed by the API.
type Status struct {
	CurrentVersion       string `json:"current_version"`
	LatestVersion        string `json:"latest_version"`
	UpdateAvailable      bool   `json:"update_available"`
	ReleaseName          string `json:"release_name"`
	ReleaseNotes         string `json:"release_notes"`
	ReleaseURL           string `json:"release_url"`
	PublishedAt          string `json:"published_at"`
	AssetName            string `json:"asset_name"`
	AssetSize            int64  `json:"asset_size"`
	DeployMode           string `json:"deploy_mode"`
	Platform             string `json:"platform"`
	CanApply             bool   `json:"can_apply"`
	CannotApplyReason    string `json:"cannot_apply_reason"`
	AutoCheckEnabled     bool   `json:"auto_check_enabled"`
	AutoCheckLockedByEnv bool   `json:"auto_check_locked_by_env"`
	CheckIntervalHours   int    `json:"check_interval_hours"`
	MirrorURL            string `json:"mirror_url"`
	State                string `json:"state"`
	TargetVersion        string `json:"target_version"`
	ProgressBytes        int64  `json:"progress_bytes"`
	TotalBytes           int64  `json:"total_bytes"`
	ProgressPercent      int    `json:"progress_percent"`
	LastCheckedAt        string `json:"last_checked_at"`
	LastError            string `json:"last_error"`
}

// ConfigInput represents persistent update configuration fields.
type ConfigInput struct {
	AutoCheckEnabled   *bool   `json:"auto_check_enabled"`
	CheckIntervalHours *int    `json:"check_interval_hours"`
	MirrorURL          *string `json:"mirror_url"`
}

// RestartRequest instructs the process supervisor to shut down cleanly and hot-exec ExecPath.
type RestartRequest struct {
	ExecPath   string
	BackupPath string
}

type persistentSettings struct {
	autoCheckEnabled     bool
	autoCheckLockedByEnv bool
	checkIntervalHours   int
	mirrorURL            string
}

type githubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	Body        string        `json:"body"`
	HTMLURL     string        `json:"html_url"`
	PublishedAt string        `json:"published_at"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	Assets      []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Service manages update detection, configuration persistence, and binary replacement.
type Service struct {
	db                *sql.DB
	dataDir           string
	currentVersion    string
	repo              string
	apiBase           string
	githubToken       string
	autoCheckEnv      *bool
	managedBinDir     string
	httpClient        *http.Client
	goos              string
	goarch            string
	executablePath    func() (string, error)
	initialCheckDelay time.Duration
	restartGraceDelay time.Duration

	mu              sync.RWMutex
	wg              sync.WaitGroup
	restartCh       chan RestartRequest
	configChangedCh chan struct{}
	renameFn        func(oldpath, newpath string) error

	state           string
	latestVersion   string
	updateAvailable bool
	releaseName     string
	releaseNotes    string
	releaseURL      string
	publishedAt     string
	assetName       string
	assetSize       int64
	assetURL        string
	checksumsURL    string
	targetVersion   string
	progressBytes   int64
	totalBytes      int64
	progressPercent int
	lastCheckedAt   string
	lastError       string

	cachedETag       string
	cachedETagSource string
	cachedETagRepo   string
	cachedETagMirror string
}

// NewService constructs a Service with normalized defaults.
func NewService(opts Options) *Service {
	currentVersion := NormalizeVersion(opts.CurrentVersion)
	if currentVersion == "" {
		currentVersion = "0.0.0-dev"
	}

	repo := strings.Trim(strings.TrimSpace(opts.Repo), "/")
	if repo == "" {
		repo = defaultRepo
	}

	apiBase := strings.TrimRight(strings.TrimSpace(opts.APIBase), "/")
	if apiBase == "" {
		apiBase = defaultAPIBase
	}

	var autoCheckEnv *bool
	if opts.AutoCheckEnv != nil {
		v := *opts.AutoCheckEnv
		autoCheckEnv = &v
	}

	managedBinDir := strings.TrimSpace(opts.ManagedBinDir)
	if managedBinDir != "" {
		managedBinDir = filepath.Clean(managedBinDir)
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return errors.New("stopped after 10 redirects")
				}
				if len(via) > 0 && via[0].URL != nil {
					initial := via[0].URL
					schemeDowngraded := strings.EqualFold(initial.Scheme, "https") && !strings.EqualFold(req.URL.Scheme, "https")
					for _, prev := range via {
						if prev.URL != nil && strings.EqualFold(prev.URL.Scheme, "https") && !strings.EqualFold(req.URL.Scheme, "https") {
							schemeDowngraded = true
							break
						}
					}
					if !strings.EqualFold(req.URL.Host, initial.Host) || schemeDowngraded {
						req.Header.Del("Authorization")
					}
				}
				return nil
			},
		}
	}

	goos := strings.TrimSpace(opts.GOOS)
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := strings.TrimSpace(opts.GOARCH)
	if goarch == "" {
		goarch = runtime.GOARCH
	}

	execPathFn := opts.ExecutablePath
	if execPathFn == nil {
		execPathFn = os.Executable
	}

	initialDelay := opts.InitialCheckDelay
	if initialDelay == 0 {
		initialDelay = defaultInitialCheckDelay
	} else if initialDelay < 0 {
		initialDelay = 0
	}

	restartGrace := opts.RestartGraceDelay
	if restartGrace == 0 {
		restartGrace = defaultRestartGraceDelay
	} else if restartGrace < 0 {
		restartGrace = 0
	}

	return &Service{
		db:                opts.DB,
		dataDir:           strings.TrimSpace(opts.DataDir),
		currentVersion:    currentVersion,
		repo:              repo,
		apiBase:           apiBase,
		githubToken:       strings.TrimSpace(opts.GitHubToken),
		autoCheckEnv:      autoCheckEnv,
		managedBinDir:     managedBinDir,
		httpClient:        httpClient,
		goos:              goos,
		goarch:            goarch,
		executablePath:    execPathFn,
		initialCheckDelay: initialDelay,
		restartGraceDelay: restartGrace,
		restartCh:         make(chan RestartRequest, 1),
		configChangedCh:   make(chan struct{}, 1),
		state:             stateIdle,
	}
}

// RestartCh returns the channel notified when an update is staged and ready for hot-exec.
func (s *Service) RestartCh() <-chan RestartRequest {
	return s.restartCh
}

// Wait blocks until background apply tasks started by the service complete.
func (s *Service) Wait() {
	s.wg.Wait()
}

// Status returns a consistent snapshot of the current update state, configuration,
// and runtime eligibility.
func (s *Service) Status(ctx context.Context) Status {
	cfg := s.loadSettings(ctx)
	env := s.evaluateEnvironmentEligibility()
	s.mu.RLock()
	st, assetURL, checksumsURL := s.snapshotStatusLocked(cfg)
	s.mu.RUnlock()
	return s.finalizeStatus(st, env, assetURL, checksumsURL)
}

// UpdateConfig validates and persists update preferences into app_settings.
func (s *Service) UpdateConfig(ctx context.Context, in ConfigInput) (Status, error) {
	updates := make(map[string]string)

	if in.AutoCheckEnabled != nil && s.autoCheckEnv == nil {
		updates[settingKeyAutoCheck] = strconv.FormatBool(*in.AutoCheckEnabled)
	}

	if in.CheckIntervalHours != nil {
		hrs := *in.CheckIntervalHours
		if hrs < minCheckIntervalHours || hrs > maxCheckIntervalHours {
			return Status{}, fmt.Errorf("%w: check_interval_hours must be between %d and %d", ErrInvalidConfig, minCheckIntervalHours, maxCheckIntervalHours)
		}
		updates[settingKeyIntervalHours] = strconv.Itoa(hrs)
	}

	if in.MirrorURL != nil {
		normalized, err := validateMirrorURL(*in.MirrorURL)
		if err != nil {
			return Status{}, err
		}
		updates[settingKeyMirrorURL] = normalized
	}

	if len(updates) > 0 && s.db != nil {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return Status{}, fmt.Errorf("begin config tx: %w", err)
		}
		defer tx.Rollback()

		now := time.Now().UTC().Format(time.RFC3339)
		for k, v := range updates {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO app_settings (key, value, updated_at) VALUES (?, ?, ?)
				ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
				k, v, now,
			); err != nil {
				return Status{}, fmt.Errorf("persist %s: %w", k, err)
			}
		}
		if err := tx.Commit(); err != nil {
			return Status{}, fmt.Errorf("commit config tx: %w", err)
		}
	}

	if _, ok := updates[settingKeyMirrorURL]; ok {
		s.mu.Lock()
		if s.cachedETagMirror != updates[settingKeyMirrorURL] {
			s.cachedETag = ""
			s.cachedETagSource = ""
			s.cachedETagMirror = ""
		}
		s.mu.Unlock()
	}

	select {
	case s.configChangedCh <- struct{}{}:
	default:
	}

	return s.Status(ctx), nil
}

// CheckNow queries GitHub Releases (via configured mirror if set, with direct fallback)
// and updates cached release metadata.
func (s *Service) CheckNow(ctx context.Context) (Status, error) {
	s.mu.Lock()
	if s.isBusyLocked() {
		s.mu.Unlock()
		return Status{}, ErrBusy
	}
	s.state = stateChecking
	repo := s.repo
	apiBase := s.apiBase
	etag := s.cachedETag
	etagSource := s.cachedETagSource
	etagRepo := s.cachedETagRepo
	etagMirror := s.cachedETagMirror
	goos := s.goos
	goarch := s.goarch
	currentVer := s.currentVersion
	s.mu.Unlock()

	cfg := s.loadSettings(ctx)
	directURL := fmt.Sprintf("%s/repos/%s/releases/latest", apiBase, repo)

	var mirrorETag, directETag string
	if etag != "" && etagRepo == repo && etagMirror == cfg.mirrorURL {
		if etagSource == "mirror" {
			mirrorETag = etag
		} else if etagSource == "direct" {
			directETag = etag
		}
	}

	resp, usedSource, err := s.httpGetWithMirrorFallback(
		ctx,
		directURL,
		cfg.mirrorURL,
		"application/vnd.github+json",
		true,
		mirrorETag,
		directETag,
		mirrorCheckTimeout,
		directCheckTimeout,
		func(status int) bool {
			return status == http.StatusOK || status == http.StatusNotModified || status == http.StatusNotFound
		},
	)
	if err != nil {
		return s.finishCheckError(cfg, fmt.Errorf("%w: %v", ErrUpstream, err))
	}
	defer resp.Body.Close()

	nowStr := time.Now().UTC().Format(time.RFC3339)

	switch resp.StatusCode {
	case http.StatusNotModified:
		s.mu.Lock()
		s.state = stateIdle
		s.clearApplyProgressLocked()
		s.lastCheckedAt = nowStr
		s.lastError = ""
		st, assetURL, checksumsURL := s.snapshotStatusLocked(cfg)
		s.mu.Unlock()
		return s.finalizeStatus(st, s.evaluateEnvironmentEligibility(), assetURL, checksumsURL), nil

	case http.StatusNotFound:
		s.mu.Lock()
		s.state = stateIdle
		s.clearApplyProgressLocked()
		s.clearReleaseLocked()
		s.cachedETag = ""
		s.cachedETagSource = ""
		s.lastCheckedAt = nowStr
		s.lastError = ""
		st, assetURL, checksumsURL := s.snapshotStatusLocked(cfg)
		s.mu.Unlock()
		return s.finalizeStatus(st, s.evaluateEnvironmentEligibility(), assetURL, checksumsURL), nil

	case http.StatusOK:
		bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, maxReleaseJSONBytes+1))
		if readErr != nil {
			return s.finishCheckError(cfg, fmt.Errorf("%w: read release body: %v", ErrUpstream, readErr))
		}
		if len(bodyBytes) > maxReleaseJSONBytes {
			return s.finishCheckError(cfg, fmt.Errorf("%w: release metadata exceeds %d bytes", ErrUpstream, maxReleaseJSONBytes))
		}

		var rel githubRelease
		if unmarshalErr := json.Unmarshal(bodyBytes, &rel); unmarshalErr != nil {
			return s.finishCheckError(cfg, fmt.Errorf("%w: decode release json: %v", ErrUpstream, unmarshalErr))
		}

		respETag := strings.TrimSpace(resp.Header.Get("ETag"))

		if rel.Draft || rel.Prerelease {
			s.mu.Lock()
			s.state = stateIdle
			s.clearApplyProgressLocked()
			s.clearReleaseLocked()
			s.updateETagLocked(respETag, usedSource, repo, cfg.mirrorURL)
			s.lastCheckedAt = nowStr
			s.lastError = ""
			st, assetURL, checksumsURL := s.snapshotStatusLocked(cfg)
			s.mu.Unlock()
			return s.finalizeStatus(st, s.evaluateEnvironmentEligibility(), assetURL, checksumsURL), nil
		}

		if _, parseErr := ParseVersion(rel.TagName); parseErr != nil {
			return s.finishCheckError(cfg, fmt.Errorf("%w: invalid release tag %q: %v", ErrUpstream, rel.TagName, parseErr))
		}
		latestVer := NormalizeVersion(rel.TagName)

		updateAvailable := false
		if IsValidSemVer(currentVer) {
			cmp, cmpErr := CompareVersion(latestVer, currentVer)
			if cmpErr == nil && cmp > 0 {
				updateAvailable = true
			}
		}

		expectedAssetName := fmt.Sprintf("coursefin_%s_%s_%s.tar.gz", latestVer, goos, goarch)
		var matchedAssetName string
		var matchedAssetSize int64
		var matchedAssetURL string
		var matchedChecksumsURL string
		for _, a := range rel.Assets {
			switch a.Name {
			case expectedAssetName:
				matchedAssetName = a.Name
				matchedAssetSize = a.Size
				matchedAssetURL = sanitizeHTTPURL(a.BrowserDownloadURL)
			case "checksums.txt":
				matchedChecksumsURL = sanitizeHTTPURL(a.BrowserDownloadURL)
			}
		}

		s.mu.Lock()
		s.state = stateIdle
		s.clearApplyProgressLocked()
		s.latestVersion = latestVer
		s.updateAvailable = updateAvailable
		s.releaseName = rel.Name
		s.releaseNotes = rel.Body
		s.releaseURL = sanitizeHTTPURL(rel.HTMLURL)
		s.publishedAt = rel.PublishedAt
		s.assetName = matchedAssetName
		s.assetSize = matchedAssetSize
		s.assetURL = matchedAssetURL
		s.checksumsURL = matchedChecksumsURL
		s.updateETagLocked(respETag, usedSource, repo, cfg.mirrorURL)
		s.lastCheckedAt = nowStr
		s.lastError = ""
		st, assetURL, checksumsURL := s.snapshotStatusLocked(cfg)
		s.mu.Unlock()
		return s.finalizeStatus(st, s.evaluateEnvironmentEligibility(), assetURL, checksumsURL), nil

	default:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return s.finishCheckError(cfg, fmt.Errorf("%w: github release api returned HTTP %d", ErrUpstream, resp.StatusCode))
	}
}

// StartPeriodicLoop runs automatic update checks after InitialCheckDelay and
// thereafter on the configured interval until ctx is cancelled.
func (s *Service) StartPeriodicLoop(ctx context.Context) {
	if s.initialCheckDelay > 0 {
		timer := time.NewTimer(s.initialCheckDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	} else if ctx.Err() != nil {
		return
	}

	if cfg := s.loadSettings(ctx); cfg.autoCheckEnabled {
		_, _ = s.CheckNow(ctx)
	}

	for {
		cfg := s.loadSettings(ctx)
		interval := time.Duration(cfg.checkIntervalHours) * time.Hour
		timer := time.NewTimer(interval)

		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.configChangedCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			continue
		case <-timer.C:
			if latestCfg := s.loadSettings(ctx); latestCfg.autoCheckEnabled {
				_, _ = s.CheckNow(ctx)
			}
		}
	}
}

func (s *Service) finishCheckError(cfg persistentSettings, err error) (Status, error) {
	s.mu.Lock()
	s.state = stateIdle
	s.clearApplyProgressLocked()
	s.lastError = err.Error()
	st, assetURL, checksumsURL := s.snapshotStatusLocked(cfg)
	s.mu.Unlock()
	return s.finalizeStatus(st, s.evaluateEnvironmentEligibility(), assetURL, checksumsURL), err
}

func (s *Service) clearApplyProgressLocked() {
	s.targetVersion = ""
	s.progressBytes = 0
	s.totalBytes = 0
	s.progressPercent = 0
}

func (s *Service) isBusyLocked() bool {
	switch s.state {
	case stateChecking, stateDownloading, stateVerifying, stateApplying, stateRestarting:
		return true
	default:
		return false
	}
}

func (s *Service) clearReleaseLocked() {
	s.latestVersion = ""
	s.updateAvailable = false
	s.releaseName = ""
	s.releaseNotes = ""
	s.releaseURL = ""
	s.publishedAt = ""
	s.assetName = ""
	s.assetSize = 0
	s.assetURL = ""
	s.checksumsURL = ""
}

func (s *Service) updateETagLocked(etag, source, repo, mirrorURL string) {
	s.cachedETag = etag
	if etag == "" {
		s.cachedETagSource = ""
		s.cachedETagRepo = ""
		s.cachedETagMirror = ""
		return
	}
	s.cachedETagSource = source
	s.cachedETagRepo = repo
	s.cachedETagMirror = mirrorURL
}

func (s *Service) snapshotStatusLocked(cfg persistentSettings) (Status, string, string) {
	return Status{
		CurrentVersion:       s.currentVersion,
		LatestVersion:        s.latestVersion,
		UpdateAvailable:      s.updateAvailable,
		ReleaseName:          s.releaseName,
		ReleaseNotes:         s.releaseNotes,
		ReleaseURL:           s.releaseURL,
		PublishedAt:          s.publishedAt,
		AssetName:            s.assetName,
		AssetSize:            s.assetSize,
		Platform:             fmt.Sprintf("%s/%s", s.goos, s.goarch),
		AutoCheckEnabled:     cfg.autoCheckEnabled,
		AutoCheckLockedByEnv: cfg.autoCheckLockedByEnv,
		CheckIntervalHours:   cfg.checkIntervalHours,
		MirrorURL:            cfg.mirrorURL,
		State:                s.state,
		TargetVersion:        s.targetVersion,
		ProgressBytes:        s.progressBytes,
		TotalBytes:           s.totalBytes,
		ProgressPercent:      s.progressPercent,
		LastCheckedAt:        s.lastCheckedAt,
		LastError:            s.lastError,
	}, s.assetURL, s.checksumsURL
}

func (s *Service) finalizeStatus(st Status, env environmentEligibility, assetURL, checksumsURL string) Status {
	deployMode, canApply, cannotReason := s.applyReleaseEligibility(env, st.UpdateAvailable, assetURL, checksumsURL)
	st.DeployMode = deployMode
	st.CanApply = canApply
	st.CannotApplyReason = cannotReason
	return st
}

func (s *Service) loadSettings(ctx context.Context) persistentSettings {
	res := persistentSettings{
		autoCheckEnabled:   true,
		checkIntervalHours: defaultCheckIntervalHrs,
	}

	if s.db != nil {
		rows, err := s.db.QueryContext(
			ctx,
			`SELECT key, value FROM app_settings WHERE key IN (?, ?, ?)`,
			settingKeyAutoCheck, settingKeyIntervalHours, settingKeyMirrorURL,
		)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var k, v string
				if scanErr := rows.Scan(&k, &v); scanErr != nil {
					continue
				}
				switch k {
				case settingKeyAutoCheck:
					if b, parseErr := strconv.ParseBool(strings.TrimSpace(v)); parseErr == nil {
						res.autoCheckEnabled = b
					}
				case settingKeyIntervalHours:
					if hrs, convErr := strconv.Atoi(strings.TrimSpace(v)); convErr == nil && hrs >= minCheckIntervalHours && hrs <= maxCheckIntervalHours {
						res.checkIntervalHours = hrs
					}
				case settingKeyMirrorURL:
					if normalized, valErr := validateMirrorURL(v); valErr == nil {
						res.mirrorURL = normalized
					}
				}
			}
		}
	}

	if s.autoCheckEnv != nil {
		res.autoCheckEnabled = *s.autoCheckEnv
		res.autoCheckLockedByEnv = true
	}

	return res
}

type environmentEligibility struct {
	deployMode string
	targetPath string
	canApply   bool
	reason     string
}

func (s *Service) evaluateEnvironmentEligibility() environmentEligibility {
	deployMode := "binary"
	if s.managedBinDir != "" {
		deployMode = "docker"
	}

	if (s.goos != "linux" && s.goos != "darwin") || (s.goos == runtime.GOOS && !SupportsHotExec()) {
		return environmentEligibility{
			deployMode: deployMode,
			reason:     "当前操作系统不支持应用内热替换，请前往 GitHub Releases 手动下载更新",
		}
	}

	if !IsValidSemVer(s.currentVersion) {
		return environmentEligibility{
			deployMode: deployMode,
			reason:     "当前为开发构建版本，不支持自动覆盖更新",
		}
	}

	targetPath, err := s.resolveTargetBinaryPath()
	if err != nil {
		return environmentEligibility{
			deployMode: deployMode,
			reason:     "程序所在目录无写入权限，无法执行自动更新",
		}
	}
	targetDir := filepath.Dir(targetPath)
	if s.managedBinDir != "" {
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return environmentEligibility{
				deployMode: deployMode,
				reason:     "程序所在目录无写入权限，无法执行自动更新",
			}
		}
	}
	if err := checkDirWritable(targetDir); err != nil {
		return environmentEligibility{
			deployMode: deployMode,
			reason:     "程序所在目录无写入权限，无法执行自动更新",
		}
	}

	return environmentEligibility{
		deployMode: deployMode,
		targetPath: targetPath,
		canApply:   true,
	}
}

func (s *Service) applyReleaseEligibility(env environmentEligibility, updateAvailable bool, assetURL, checksumsURL string) (deployMode string, canApply bool, reason string) {
	if !env.canApply {
		return env.deployMode, false, env.reason
	}
	if updateAvailable && (assetURL == "" || checksumsURL == "") {
		return env.deployMode, false, fmt.Sprintf("该发布版本缺少当前平台 (%s/%s) 的更新包或校验文件", s.goos, s.goarch)
	}
	return env.deployMode, true, ""
}

func (s *Service) resolveTargetBinaryPath() (string, error) {
	if s.managedBinDir != "" {
		return filepath.Join(s.managedBinDir, "coursefin"), nil
	}
	exePath, err := s.executablePath()
	if err != nil || strings.TrimSpace(exePath) == "" {
		if err == nil {
			err = errors.New("empty executable path")
		}
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(exePath)
	if err == nil {
		return resolved, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		dir := filepath.Dir(exePath)
		if resolvedDir, dirErr := filepath.EvalSymlinks(dir); dirErr == nil {
			return filepath.Join(resolvedDir, filepath.Base(exePath)), nil
		}
		return exePath, nil
	}
	return "", err
}

func (s *Service) userAgent() string {
	return "CourseFin/" + s.currentVersion
}

// httpGetWithMirrorFallback tries <mirrorURL>/<directURL> first without Authorization
// (when mirrorURL is non-empty) and falls back to directURL if the mirror fails or
// returns a status rejected by isMirrorAcceptable.
func (s *Service) httpGetWithMirrorFallback(
	ctx context.Context,
	directURL string,
	mirrorURL string,
	accept string,
	sendTokenOnDirect bool,
	mirrorETag string,
	directETag string,
	mirrorTimeout time.Duration,
	directTimeout time.Duration,
	isMirrorAcceptable func(status int) bool,
) (*http.Response, string, error) {
	doOne := func(reqURL string, sendToken bool, etag string, timeout time.Duration) (*http.Response, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		reqCtx := ctx
		var cancel context.CancelFunc
		if timeout > 0 {
			reqCtx, cancel = context.WithTimeout(ctx, timeout)
		}

		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, reqURL, nil)
		if err != nil {
			if cancel != nil {
				cancel()
			}
			return nil, err
		}
		req.Header.Set("User-Agent", s.userAgent())
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		if sendToken && s.githubToken != "" {
			req.Header.Set("Authorization", "Bearer "+s.githubToken)
		}

		resp, err := s.httpClient.Do(req)
		if err != nil {
			if cancel != nil {
				cancel()
			}
			return nil, err
		}
		if cancel != nil {
			resp.Body = &cancelOnCloseReadCloser{
				ReadCloser: resp.Body,
				cancel:     cancel,
			}
		}
		return resp, nil
	}

	if mirrorURL != "" {
		mirrorReqURL := strings.TrimRight(mirrorURL, "/") + "/" + directURL
		resp, err := doOne(mirrorReqURL, false, mirrorETag, mirrorTimeout)
		if err == nil {
			if isMirrorAcceptable == nil || isMirrorAcceptable(resp.StatusCode) {
				return resp, "mirror", nil
			}
			_ = resp.Body.Close()
		}
	}

	resp, err := doOne(directURL, sendTokenOnDirect, directETag, directTimeout)
	if err != nil {
		return nil, "", err
	}
	return resp, "direct", nil
}

type cancelOnCloseReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnCloseReadCloser) Close() error {
	err := c.ReadCloser.Close()
	if c.cancel != nil {
		c.cancel()
	}
	return err
}

func validateMirrorURL(raw string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return "", nil
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("%w: invalid mirror_url: %v", ErrInvalidConfig, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if (scheme != "http" && scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%w: mirror_url must use http or https with a valid host", ErrInvalidConfig)
	}
	return trimmed, nil
}

func sanitizeHTTPURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	if (scheme != "http" && scheme != "https") || u.Hostname() == "" {
		return ""
	}
	return trimmed
}

func checkDirWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".coursefin-write-check-*")
	if err != nil {
		return err
	}
	name := f.Name()
	closeErr := f.Close()
	removeErr := os.Remove(name)
	if closeErr != nil {
		return closeErr
	}
	return removeErr
}
