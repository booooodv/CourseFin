package jellyfin

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"coursefin/internal/auth"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	jellyfinServerVersion = "10.9.11"
	jellyfinProductName   = "Jellyfin Server"
	defaultServerName     = "CourseFin"
	wsKeepAliveInterval   = 30 * time.Second
)

type serverIdentity struct {
	ID   string
	Name string
}

func (h *Handler) getServerIdentity(ctx context.Context) serverIdentity {
	ident := serverIdentity{
		ID:   "00000000000000000000000000000001",
		Name: defaultServerName,
	}
	rows, err := h.db.QueryContext(ctx, `SELECT key, value FROM app_settings WHERE key IN ('server_id', 'server_name')`)
	if err != nil {
		return ident
	}
	defer rows.Close()

	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		switch k {
		case "server_id":
			ident.ID = normalizeID(v)
		case "server_name":
			ident.Name = v
		}
	}
	return ident
}

func requestLocalAddress(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		host = "localhost:8096"
	}
	return scheme + "://" + host
}

func requestPort(r *http.Request) int {
	if _, portStr, err := net.SplitHostPort(r.Host); err == nil {
		if p, perr := strconv.Atoi(portStr); perr == nil && p > 0 {
			return p
		}
	}
	return 8096
}

func osDisplayName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	default:
		return "Linux"
	}
}

func (h *Handler) buildPublicSystemInfo(r *http.Request) PublicSystemInfo {
	ident := h.getServerIdentity(r.Context())
	return PublicSystemInfo{
		LocalAddress:           requestLocalAddress(r),
		ServerName:             ident.Name,
		Version:                jellyfinServerVersion,
		ProductName:            jellyfinProductName,
		OperatingSystem:        osDisplayName(),
		Id:                     ident.ID,
		StartupWizardCompleted: true,
	}
}

func (h *Handler) handlePublicSystemInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.buildPublicSystemInfo(r))
}

func (h *Handler) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	pub := h.buildPublicSystemInfo(r)
	writeJSON(w, http.StatusOK, SystemInfo{
		PublicSystemInfo:           pub,
		HasPendingRestart:          false,
		IsShuttingDown:             false,
		SupportsLibraryMonitor:     false,
		WebSocketPortNumber:        requestPort(r),
		CompletedInstallations:     []any{},
		CanSelfRestart:             false,
		CanLaunchWebBrowser:        false,
		HasUpdateAvailable:         false,
		EncoderLocation:            "System",
		SystemArchitecture:         runtime.GOARCH,
		OperatingSystemDisplayName: osDisplayName(),
		PackageName:                "CourseFin",
	})
}

func (h *Handler) handlePing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, jellyfinProductName)
}

func (h *Handler) handleEndpoint(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	ip := net.ParseIP(host)
	isLocal := ip == nil || ip.IsLoopback()
	isInNetwork := isLocal || ip.IsPrivate() || ip.IsLinkLocalUnicast()
	writeJSON(w, http.StatusOK, EndPointInfo{
		IsLocal:     isLocal,
		IsInNetwork: isInNetwork,
	})
}

func (h *Handler) handleBrandingConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, BrandingConfiguration{
		LoginDisclaimer:     "",
		CustomCss:           "",
		SplashscreenEnabled: false,
	})
}

func (h *Handler) handleBrandingCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleQuickConnectEnabled(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, false)
}

func (h *Handler) handleGetDisplayPreferences(w http.ResponseWriter, r *http.Request) {
	prefID := pathParam(r, "displayPreferencesId")
	if prefID == "" {
		writeError(w, http.StatusBadRequest, "missing displayPreferencesId")
		return
	}

	var raw string
	err := h.db.QueryRowContext(r.Context(), `SELECT value FROM app_settings WHERE key = ?`, "display_pref:"+prefID).Scan(&raw)
	if err == nil && strings.TrimSpace(raw) != "" {
		var dto DisplayPreferencesDto
		if jerr := json.Unmarshal([]byte(raw), &dto); jerr == nil {
			dto.Id = prefID
			if dto.CustomPrefs == nil {
				dto.CustomPrefs = map[string]string{}
			}
			writeJSON(w, http.StatusOK, dto)
			return
		}
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, DisplayPreferencesDto{
		Id:                 prefID,
		SortBy:             "SortName",
		PrimaryImageHeight: 250,
		PrimaryImageWidth:  250,
		CustomPrefs:        map[string]string{},
		ScrollDirection:    "Horizontal",
		ShowBackdrop:       true,
		SortOrder:          "Ascending",
		Client:             queryParam(r, "client"),
	})
}

func (h *Handler) handlePostDisplayPreferences(w http.ResponseWriter, r *http.Request) {
	prefID := pathParam(r, "displayPreferencesId")
	if prefID == "" {
		writeError(w, http.StatusBadRequest, "missing displayPreferencesId")
		return
	}

	var dto DisplayPreferencesDto
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json payload")
		return
	}
	dto.Id = prefID
	if dto.CustomPrefs == nil {
		dto.CustomPrefs = map[string]string{}
	}
	if dto.SortBy == "" {
		dto.SortBy = "SortName"
	}
	if dto.SortOrder == "" {
		dto.SortOrder = "Ascending"
	}
	if dto.ScrollDirection == "" {
		dto.ScrollDirection = "Horizontal"
	}
	if dto.PrimaryImageHeight <= 0 {
		dto.PrimaryImageHeight = 250
	}
	if dto.PrimaryImageWidth <= 0 {
		dto.PrimaryImageWidth = 250
	}

	encoded, err := json.Marshal(dto)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := h.db.ExecContext(r.Context(), `
		INSERT INTO app_settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		"display_pref:"+prefID, string(encoded), now,
	); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleItemCounts(w http.ResponseWriter, r *http.Request) {
	var seriesCount, episodeCount int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM courses`).Scan(&seriesCount); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM lessons`).Scan(&episodeCount); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, ItemCounts{
		SeriesCount:  seriesCount,
		EpisodeCount: episodeCount,
		ItemCount:    seriesCount + episodeCount,
	})
}

func buildUserDTO(u *auth.User, serverID string) UserDto {
	return UserDto{
		Name:                      u.Username,
		ServerId:                  serverID,
		Id:                        u.ID,
		HasPassword:               true,
		HasConfiguredPassword:     true,
		HasConfiguredEasyPassword: false,
		EnableAutoLogin:           false,
		LastLoginDate:             u.UpdatedAt,
		LastActivityDate:          u.UpdatedAt,
		Configuration: UserConfiguration{
			PlayDefaultAudioTrack:      true,
			SubtitleMode:               "Default",
			EnableLocalPassword:        true,
			OrderedViews:               []string{},
			LatestItemsExcludes:        []string{},
			MyMediaExcludes:            []string{},
			RememberAudioSelections:    true,
			RememberSubtitleSelections: true,
			EnableNextEpisodeAutoPlay:  true,
		},
		Policy: UserPolicy{
			IsAdministrator:                true,
			EnableAllFolders:               true,
			EnableMediaPlayback:            true,
			EnableAudioPlaybackTranscoding: true,
			EnableVideoPlaybackTranscoding: false,
			EnablePlaybackRemuxing:         true,
			EnabledFolders:                 []string{},
			BlockedChannels:                []string{},
			LoginAttemptsBeforeLockout:     -1,
			MaxActiveSessions:              0,
			AuthenticationProviderId:       "Jellyfin.Server.Implementations.Users.DefaultAuthenticationProvider",
			PasswordResetProviderId:        "Jellyfin.Server.Implementations.Users.DefaultPasswordResetProvider",
		},
	}
}

func sessionPublicID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:16])
}

func buildSessionInfoDTO(sess *auth.Session, username, serverID string) SessionInfo {
	return SessionInfo{
		Id:                   sessionPublicID(sess.Token),
		UserId:               sess.UserID,
		UserName:             username,
		Client:               sess.ClientName,
		LastActivityDate:     sess.LastActiveAt,
		DeviceName:           sess.DeviceName,
		DeviceId:             sess.DeviceID,
		ApplicationVersion:   sess.ClientVersion,
		IsActive:             true,
		SupportsMediaControl: false,
		PlayableMediaTypes:   []string{"Video"},
		ServerId:             serverID,
	}
}

func (h *Handler) handlePublicUsers(w http.ResponseWriter, r *http.Request) {
	u, err := h.auth.GetAdminUser(r.Context())
	if errors.Is(err, auth.ErrNotSetup) {
		writeJSON(w, http.StatusOK, []UserDto{})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ident := h.getServerIdentity(r.Context())
	writeJSON(w, http.StatusOK, []UserDto{buildUserDTO(u, ident.ID)})
}

func (h *Handler) handleAuthenticateByName(w http.ResponseWriter, r *http.Request) {
	var req AuthenticateByNameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid authentication request")
		return
	}
	pw := req.Pw
	if pw == "" {
		pw = req.Password
	}

	_, dev := extractToken(r)
	sess, user, err := h.auth.Login(r.Context(), req.Username, pw, dev)
	if errors.Is(err, auth.ErrInvalidCredentials) || errors.Is(err, auth.ErrNotSetup) {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ident := h.getServerIdentity(r.Context())
	writeJSON(w, http.StatusOK, AuthenticationResult{
		User:        buildUserDTO(user, ident.ID),
		SessionInfo: buildSessionInfoDTO(sess, user.Username, ident.ID),
		AccessToken: sess.Token,
		ServerId:    ident.ID,
	})
}

func (h *Handler) handleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	ac := currentAuth(r)
	ident := h.getServerIdentity(r.Context())
	writeJSON(w, http.StatusOK, buildUserDTO(ac.User, ident.ID))
}

func (h *Handler) handleGetUser(w http.ResponseWriter, r *http.Request) {
	ac := currentAuth(r)
	if !resolveUserMatch(r, ac.User) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	ident := h.getServerIdentity(r.Context())
	writeJSON(w, http.StatusOK, buildUserDTO(ac.User, ident.ID))
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	ac := currentAuth(r)
	if err := h.auth.RevokeSession(r.Context(), ac.Token); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleGetSessions(w http.ResponseWriter, r *http.Request) {
	ac := currentAuth(r)
	sessions, err := h.auth.ListSessions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ident := h.getServerIdentity(r.Context())
	out := make([]SessionInfo, 0, len(sessions))
	for i := range sessions {
		out = append(out, buildSessionInfoDTO(&sessions[i], ac.User.Username, ident.ID))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) handleSocket(w http.ResponseWriter, r *http.Request) {
	// If client provides an explicit token, validate it before upgrading.
	if token, _ := extractToken(r); token != "" {
		if _, _, err := h.auth.Authenticate(r.Context(), token); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	var writeMu sync.Mutex
	writeMsg := func(msg wsMessage) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
		defer wcancel()
		return wsjson.Write(wctx, conn, msg)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(wsKeepAliveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := writeMsg(wsMessage{
					MessageType: "ForceKeepAlive",
					Data:        int(wsKeepAliveInterval.Seconds() * 2),
				}); err != nil {
					cancel()
					return
				}
			}
		}
	}()

	for {
		var msg wsMessage
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			break
		}
		if strings.EqualFold(msg.MessageType, "KeepAlive") {
			if err := writeMsg(wsMessage{MessageType: "KeepAlive"}); err != nil {
				break
			}
		}
	}

	cancel()
	<-done
	_ = conn.Close(websocket.StatusNormalClosure, "")
}
