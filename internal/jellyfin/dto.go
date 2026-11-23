package jellyfin

// PublicSystemInfo represents Jellyfin's /System/Info/Public payload.
type PublicSystemInfo struct {
	LocalAddress           string `json:"LocalAddress"`
	ServerName             string `json:"ServerName"`
	Version                string `json:"Version"`
	ProductName            string `json:"ProductName"`
	OperatingSystem        string `json:"OperatingSystem"`
	Id                     string `json:"Id"`
	StartupWizardCompleted bool   `json:"StartupWizardCompleted"`
}

// SystemInfo represents Jellyfin's authenticated /System/Info payload.
type SystemInfo struct {
	PublicSystemInfo
	HasPendingRestart          bool   `json:"HasPendingRestart"`
	IsShuttingDown             bool   `json:"IsShuttingDown"`
	SupportsLibraryMonitor     bool   `json:"SupportsLibraryMonitor"`
	WebSocketPortNumber        int    `json:"WebSocketPortNumber"`
	CompletedInstallations     []any  `json:"CompletedInstallations"`
	CanSelfRestart             bool   `json:"CanSelfRestart"`
	CanLaunchWebBrowser        bool   `json:"CanLaunchWebBrowser"`
	HasUpdateAvailable         bool   `json:"HasUpdateAvailable"`
	EncoderLocation            string `json:"EncoderLocation"`
	SystemArchitecture         string `json:"SystemArchitecture"`
	OperatingSystemDisplayName string `json:"OperatingSystemDisplayName"`
	PackageName                string `json:"PackageName"`
}

// EndPointInfo represents Jellyfin's /System/Endpoint response.
type EndPointInfo struct {
	IsLocal     bool `json:"IsLocal"`
	IsInNetwork bool `json:"IsInNetwork"`
}

// BrandingConfiguration represents Jellyfin's /Branding/Configuration response.
type BrandingConfiguration struct {
	LoginDisclaimer     string `json:"LoginDisclaimer"`
	CustomCss           string `json:"CustomCss"`
	SplashscreenEnabled bool   `json:"SplashscreenEnabled"`
}

// ItemCounts represents Jellyfin's /Items/Counts response.
type ItemCounts struct {
	MovieCount      int `json:"MovieCount"`
	SeriesCount     int `json:"SeriesCount"`
	EpisodeCount    int `json:"EpisodeCount"`
	ArtistCount     int `json:"ArtistCount"`
	ProgramCount    int `json:"ProgramCount"`
	TrailerCount    int `json:"TrailerCount"`
	SongCount       int `json:"SongCount"`
	AlbumCount      int `json:"AlbumCount"`
	MusicVideoCount int `json:"MusicVideoCount"`
	BoxSetCount     int `json:"BoxSetCount"`
	BookCount       int `json:"BookCount"`
	ItemCount       int `json:"ItemCount"`
}

// DisplayPreferencesDto represents Jellyfin's /DisplayPreferences/{id} payload.
type DisplayPreferencesDto struct {
	Id                 string            `json:"Id"`
	ViewType           *string           `json:"ViewType"`
	SortBy             string            `json:"SortBy"`
	IndexBy            *string           `json:"IndexBy"`
	RememberIndexing   bool              `json:"RememberIndexing"`
	PrimaryImageHeight int               `json:"PrimaryImageHeight"`
	PrimaryImageWidth  int               `json:"PrimaryImageWidth"`
	CustomPrefs        map[string]string `json:"CustomPrefs"`
	ScrollDirection    string            `json:"ScrollDirection"`
	ShowBackdrop       bool              `json:"ShowBackdrop"`
	RememberSorting    bool              `json:"RememberSorting"`
	SortOrder          string            `json:"SortOrder"`
	ShowSidebar        bool              `json:"ShowSidebar"`
	Client             string            `json:"Client,omitempty"`
}

// UserPolicy mirrors Jellyfin's UserPolicy DTO.
type UserPolicy struct {
	IsAdministrator                 bool     `json:"IsAdministrator"`
	IsHidden                        bool     `json:"IsHidden"`
	IsDisabled                      bool     `json:"IsDisabled"`
	EnableAllFolders                bool     `json:"EnableAllFolders"`
	EnableMediaPlayback             bool     `json:"EnableMediaPlayback"`
	EnableAudioPlaybackTranscoding  bool     `json:"EnableAudioPlaybackTranscoding"`
	EnableVideoPlaybackTranscoding  bool     `json:"EnableVideoPlaybackTranscoding"`
	EnablePlaybackRemuxing          bool     `json:"EnablePlaybackRemuxing"`
	EnableContentDeletion           bool     `json:"EnableContentDeletion"`
	EnableRemoteControlOfOtherUsers bool     `json:"EnableRemoteControlOfOtherUsers"`
	EnableSharedDeviceControl       bool     `json:"EnableSharedDeviceControl"`
	EnableLiveTvAccess              bool     `json:"EnableLiveTvAccess"`
	EnablePublicSharing             bool     `json:"EnablePublicSharing"`
	EnabledFolders                  []string `json:"EnabledFolders"`
	BlockedChannels                 []string `json:"BlockedChannels"`
	InvalidLoginAttemptCount        int      `json:"InvalidLoginAttemptCount"`
	LoginAttemptsBeforeLockout      int      `json:"LoginAttemptsBeforeLockout"`
	MaxActiveSessions               int      `json:"MaxActiveSessions"`
	AuthenticationProviderId        string   `json:"AuthenticationProviderId"`
	PasswordResetProviderId         string   `json:"PasswordResetProviderId"`
}

// UserConfiguration mirrors Jellyfin's UserConfiguration DTO.
type UserConfiguration struct {
	PlayDefaultAudioTrack      bool     `json:"PlayDefaultAudioTrack"`
	SubtitleLanguagePreference string   `json:"SubtitleLanguagePreference"`
	DisplayMissingEpisodes     bool     `json:"DisplayMissingEpisodes"`
	SubtitleMode               string   `json:"SubtitleMode"`
	EnableLocalPassword        bool     `json:"EnableLocalPassword"`
	OrderedViews               []string `json:"OrderedViews"`
	LatestItemsExcludes        []string `json:"LatestItemsExcludes"`
	MyMediaExcludes            []string `json:"MyMediaExcludes"`
	HidePlayedInLatest         bool     `json:"HidePlayedInLatest"`
	RememberAudioSelections    bool     `json:"RememberAudioSelections"`
	RememberSubtitleSelections bool     `json:"RememberSubtitleSelections"`
	EnableNextEpisodeAutoPlay  bool     `json:"EnableNextEpisodeAutoPlay"`
}

// UserDto represents a Jellyfin user object.
type UserDto struct {
	Name                      string            `json:"Name"`
	ServerId                  string            `json:"ServerId"`
	Id                        string            `json:"Id"`
	HasPassword               bool              `json:"HasPassword"`
	HasConfiguredPassword     bool              `json:"HasConfiguredPassword"`
	HasConfiguredEasyPassword bool              `json:"HasConfiguredEasyPassword"`
	EnableAutoLogin           bool              `json:"EnableAutoLogin"`
	LastLoginDate             string            `json:"LastLoginDate,omitempty"`
	LastActivityDate          string            `json:"LastActivityDate,omitempty"`
	Configuration             UserConfiguration `json:"Configuration"`
	Policy                    UserPolicy        `json:"Policy"`
}

// AuthenticateByNameRequest represents POST /Users/AuthenticateByName input.
type AuthenticateByNameRequest struct {
	Username string `json:"Username"`
	Pw       string `json:"Pw"`
	Password string `json:"Password"`
}

// SessionInfo represents a Jellyfin client session.
type SessionInfo struct {
	Id                    string   `json:"Id"`
	UserId                string   `json:"UserId"`
	UserName              string   `json:"UserName"`
	Client                string   `json:"Client"`
	LastActivityDate      string   `json:"LastActivityDate"`
	DeviceName            string   `json:"DeviceName"`
	DeviceId              string   `json:"DeviceId"`
	ApplicationVersion    string   `json:"ApplicationVersion"`
	IsActive              bool     `json:"IsActive"`
	SupportsMediaControl  bool     `json:"SupportsMediaControl"`
	SupportsRemoteControl bool     `json:"SupportsRemoteControl"`
	PlayableMediaTypes    []string `json:"PlayableMediaTypes"`
	ServerId              string   `json:"ServerId"`
}

// AuthenticationResult represents POST /Users/AuthenticateByName output.
type AuthenticationResult struct {
	User        UserDto     `json:"User"`
	SessionInfo SessionInfo `json:"SessionInfo"`
	AccessToken string      `json:"AccessToken"`
	ServerId    string      `json:"ServerId"`
}

// QueryResult wraps paged Jellyfin list responses.
type QueryResult[T any] struct {
	Items            []T `json:"Items"`
	TotalRecordCount int `json:"TotalRecordCount"`
	StartIndex       int `json:"StartIndex"`
}

// NameGuidPair pairs a display name with a deterministic Jellyfin ID.
type NameGuidPair struct {
	Name string `json:"Name"`
	Id   string `json:"Id,omitempty"`
}

// BaseItemPerson describes a course instructor/person entry.
type BaseItemPerson struct {
	Name string `json:"Name"`
	Id   string `json:"Id,omitempty"`
	Role string `json:"Role,omitempty"`
	Type string `json:"Type"`
}

// UserItemDataDto represents per-item playback progress and played state.
type UserItemDataDto struct {
	PlaybackPositionTicks int64   `json:"PlaybackPositionTicks"`
	PlayCount             int     `json:"PlayCount"`
	IsFavorite            bool    `json:"IsFavorite"`
	Played                bool    `json:"Played"`
	LastPlayedDate        *string `json:"LastPlayedDate,omitempty"`
	UnplayedItemCount     *int    `json:"UnplayedItemCount,omitempty"`
	PlayedPercentage      float64 `json:"PlayedPercentage,omitempty"`
	Key                   string  `json:"Key"`
}

// UpdateUserItemDataDto represents POST /Users/{uid}/Items/{id}/UserData input.
type UpdateUserItemDataDto struct {
	PlaybackPositionTicks *int64  `json:"PlaybackPositionTicks"`
	PlayCount             *int    `json:"PlayCount"`
	IsFavorite            *bool   `json:"IsFavorite"`
	Played                *bool   `json:"Played"`
	LastPlayedDate        *string `json:"LastPlayedDate"`
}

// BaseItemDto represents a Jellyfin catalog item (CollectionFolder, Series, Season, Episode, Genre, Tag).
type BaseItemDto struct {
	Name                  string            `json:"Name"`
	OriginalTitle         string            `json:"OriginalTitle,omitempty"`
	ServerId              string            `json:"ServerId"`
	Id                    string            `json:"Id"`
	SortName              string            `json:"SortName,omitempty"`
	ForcedSortName        string            `json:"ForcedSortName,omitempty"`
	Overview              string            `json:"Overview,omitempty"`
	ParentId              string            `json:"ParentId,omitempty"`
	SeriesId              string            `json:"SeriesId,omitempty"`
	SeriesName            string            `json:"SeriesName,omitempty"`
	SeasonId              string            `json:"SeasonId,omitempty"`
	SeasonName            string            `json:"SeasonName,omitempty"`
	Type                  string            `json:"Type"`
	CollectionType        string            `json:"CollectionType,omitempty"`
	MediaType             string            `json:"MediaType,omitempty"`
	IsFolder              bool              `json:"IsFolder"`
	IndexNumber           *int              `json:"IndexNumber,omitempty"`
	ParentIndexNumber     *int              `json:"ParentIndexNumber,omitempty"`
	ChildCount            *int              `json:"ChildCount,omitempty"`
	RecursiveItemCount    *int              `json:"RecursiveItemCount,omitempty"`
	RunTimeTicks          *int64            `json:"RunTimeTicks,omitempty"`
	Container             string            `json:"Container,omitempty"`
	Width                 int               `json:"Width,omitempty"`
	Height                int               `json:"Height,omitempty"`
	HasSubtitles          bool              `json:"HasSubtitles,omitempty"`
	DateCreated           string            `json:"DateCreated,omitempty"`
	Status                string            `json:"Status,omitempty"`
	DisplayOrder          string            `json:"DisplayOrder,omitempty"`
	LocationType          string            `json:"LocationType,omitempty"`
	DisplayPreferencesId  string            `json:"DisplayPreferencesId,omitempty"`
	Tags                  []string          `json:"Tags,omitempty"`
	Genres                []string          `json:"Genres,omitempty"`
	GenreItems            []NameGuidPair    `json:"GenreItems,omitempty"`
	People                []BaseItemPerson  `json:"People,omitempty"`
	ImageTags             map[string]string `json:"ImageTags"`
	BackdropImageTags     []string          `json:"BackdropImageTags"`
	SeriesPrimaryImageTag string            `json:"SeriesPrimaryImageTag,omitempty"`
	UserData              *UserItemDataDto  `json:"UserData,omitempty"`
	MediaSources          []MediaSourceInfo `json:"MediaSources,omitempty"`
}

// QueryFiltersLegacy represents GET /Items/Filters output.
type QueryFiltersLegacy struct {
	Genres          []string `json:"Genres"`
	Tags            []string `json:"Tags"`
	OfficialRatings []string `json:"OfficialRatings"`
	Years           []int    `json:"Years"`
}

// QueryFilters represents GET /Items/Filters2 output.
type QueryFilters struct {
	Genres []NameGuidPair `json:"Genres"`
	Tags   []string       `json:"Tags"`
}

// SearchHint represents a single search suggestion entry.
type SearchHint struct {
	ItemId            string  `json:"ItemId"`
	Id                string  `json:"Id"`
	Name              string  `json:"Name"`
	MatchedTerm       string  `json:"MatchedTerm,omitempty"`
	IndexNumber       *int    `json:"IndexNumber,omitempty"`
	ParentIndexNumber *int    `json:"ParentIndexNumber,omitempty"`
	RunTimeTicks      *int64  `json:"RunTimeTicks,omitempty"`
	Type              string  `json:"Type"`
	MediaType         string  `json:"MediaType,omitempty"`
	IsFolder          bool    `json:"IsFolder"`
	Series            string  `json:"Series,omitempty"`
	SeriesId          string  `json:"SeriesId,omitempty"`
	SeasonId          string  `json:"SeasonId,omitempty"`
	PrimaryImageTag   string  `json:"PrimaryImageTag,omitempty"`
	ChannelId         *string `json:"ChannelId"`
}

// SearchHintResult represents GET /Search/Hints output.
type SearchHintResult struct {
	SearchHints      []SearchHint `json:"SearchHints"`
	TotalRecordCount int          `json:"TotalRecordCount"`
}

// DirectPlayProfile describes a client's native playback capability.
type DirectPlayProfile struct {
	Container  string `json:"Container"`
	AudioCodec string `json:"AudioCodec"`
	VideoCodec string `json:"VideoCodec"`
	Type       string `json:"Type"`
}

// TranscodingProfile describes a client's accepted remux/transcode target.
type TranscodingProfile struct {
	Container  string `json:"Container"`
	Type       string `json:"Type"`
	VideoCodec string `json:"VideoCodec"`
	AudioCodec string `json:"AudioCodec"`
	Protocol   string `json:"Protocol"`
	Context    string `json:"Context"`
}

// SubtitleProfile describes how a client consumes a given subtitle format.
type SubtitleProfile struct {
	Format string `json:"Format"`
	Method string `json:"Method"`
}

// CodecProfile represents optional client codec constraints.
type CodecProfile struct {
	Type  string `json:"Type"`
	Codec string `json:"Codec"`
}

// DeviceProfile models Jellyfin's PlaybackInfo DeviceProfile payload.
type DeviceProfile struct {
	Name                string               `json:"Name"`
	Id                  string               `json:"Id"`
	MaxStreamingBitrate *int64               `json:"MaxStreamingBitrate"`
	DirectPlayProfiles  []DirectPlayProfile  `json:"DirectPlayProfiles"`
	TranscodingProfiles []TranscodingProfile `json:"TranscodingProfiles"`
	SubtitleProfiles    []SubtitleProfile    `json:"SubtitleProfiles"`
	CodecProfiles       []CodecProfile       `json:"CodecProfiles"`
}

// PlaybackInfoRequest models POST/GET /Items/{id}/PlaybackInfo parameters.
type PlaybackInfoRequest struct {
	UserId               string         `json:"UserId"`
	MediaSourceId        string         `json:"MediaSourceId"`
	MaxStreamingBitrate  *int64         `json:"MaxStreamingBitrate"`
	StartTimeTicks       *int64         `json:"StartTimeTicks"`
	AudioStreamIndex     *int           `json:"AudioStreamIndex"`
	SubtitleStreamIndex  *int           `json:"SubtitleStreamIndex"`
	EnableDirectPlay     *bool          `json:"EnableDirectPlay"`
	EnableDirectStream   *bool          `json:"EnableDirectStream"`
	EnableTranscoding    *bool          `json:"EnableTranscoding"`
	AllowVideoStreamCopy *bool          `json:"AllowVideoStreamCopy"`
	AllowAudioStreamCopy *bool          `json:"AllowAudioStreamCopy"`
	DeviceProfile        *DeviceProfile `json:"DeviceProfile"`
}

// MediaStream represents a single video, audio, or subtitle stream in MediaSourceInfo.
type MediaStream struct {
	Codec                  string `json:"Codec,omitempty"`
	Language               string `json:"Language,omitempty"`
	Title                  string `json:"Title,omitempty"`
	DisplayTitle           string `json:"DisplayTitle,omitempty"`
	Index                  int    `json:"Index"`
	IsDefault              bool   `json:"IsDefault"`
	IsForced               bool   `json:"IsForced"`
	IsExternal             bool   `json:"IsExternal"`
	Type                   string `json:"Type"`
	Width                  int    `json:"Width,omitempty"`
	Height                 int    `json:"Height,omitempty"`
	BitRate                int64  `json:"BitRate,omitempty"`
	Channels               int    `json:"Channels,omitempty"`
	SampleRate             int    `json:"SampleRate,omitempty"`
	IsTextSubtitleStream   bool   `json:"IsTextSubtitleStream,omitempty"`
	SupportsExternalStream bool   `json:"SupportsExternalStream,omitempty"`
	DeliveryMethod         string `json:"DeliveryMethod,omitempty"`
	DeliveryUrl            string `json:"DeliveryUrl,omitempty"`
}

// MediaSourceInfo describes playback URLs, container/codec support, and streams for a media file.
type MediaSourceInfo struct {
	Protocol                   string        `json:"Protocol"`
	Id                         string        `json:"Id"`
	Path                       string        `json:"Path,omitempty"`
	Type                       string        `json:"Type"`
	Container                  string        `json:"Container,omitempty"`
	Size                       int64         `json:"Size,omitempty"`
	Name                       string        `json:"Name,omitempty"`
	IsRemote                   bool          `json:"IsRemote"`
	RunTimeTicks               int64         `json:"RunTimeTicks"`
	ReadAtNativeFramerate      bool          `json:"ReadAtNativeFramerate"`
	SupportsTranscoding        bool          `json:"SupportsTranscoding"`
	SupportsDirectStream       bool          `json:"SupportsDirectStream"`
	SupportsDirectPlay         bool          `json:"SupportsDirectPlay"`
	IsInfiniteStream           bool          `json:"IsInfiniteStream"`
	RequiresOpening            bool          `json:"RequiresOpening"`
	RequiresClosing            bool          `json:"RequiresClosing"`
	RequiresLooping            bool          `json:"RequiresLooping"`
	SupportsProbing            bool          `json:"SupportsProbing"`
	DirectStreamUrl            string        `json:"DirectStreamUrl,omitempty"`
	TranscodingUrl             string        `json:"TranscodingUrl,omitempty"`
	TranscodingSubProtocol     string        `json:"TranscodingSubProtocol,omitempty"`
	TranscodingContainer       string        `json:"TranscodingContainer,omitempty"`
	MediaStreams               []MediaStream `json:"MediaStreams"`
	DefaultAudioStreamIndex    *int          `json:"DefaultAudioStreamIndex,omitempty"`
	DefaultSubtitleStreamIndex *int          `json:"DefaultSubtitleStreamIndex,omitempty"`
	Bitrate                    int64         `json:"Bitrate,omitempty"`
}

// PlaybackInfoResponse represents POST/GET /Items/{id}/PlaybackInfo output.
type PlaybackInfoResponse struct {
	MediaSources  []MediaSourceInfo `json:"MediaSources"`
	PlaySessionId string            `json:"PlaySessionId"`
	ErrorCode     string            `json:"ErrorCode,omitempty"`
}

// PlaybackStartInfo models POST /Sessions/Playing payload.
type PlaybackStartInfo struct {
	ItemId        string `json:"ItemId"`
	MediaSourceId string `json:"MediaSourceId"`
	PlaySessionId string `json:"PlaySessionId"`
	PositionTicks int64  `json:"PositionTicks"`
	IsPaused      bool   `json:"IsPaused"`
	PlayMethod    string `json:"PlayMethod"`
}

// PlaybackProgressInfo models POST /Sessions/Playing/Progress payload.
type PlaybackProgressInfo struct {
	ItemId        string `json:"ItemId"`
	MediaSourceId string `json:"MediaSourceId"`
	PlaySessionId string `json:"PlaySessionId"`
	PositionTicks int64  `json:"PositionTicks"`
	IsPaused      bool   `json:"IsPaused"`
	PlayMethod    string `json:"PlayMethod"`
}

// PlaybackStopInfo models POST /Sessions/Playing/Stopped payload.
type PlaybackStopInfo struct {
	ItemId        string `json:"ItemId"`
	MediaSourceId string `json:"MediaSourceId"`
	PlaySessionId string `json:"PlaySessionId"`
	PositionTicks *int64 `json:"PositionTicks"`
}

// wsMessage models Jellyfin WebSocket JSON messages.
type wsMessage struct {
	MessageType string `json:"MessageType"`
	MessageId   string `json:"MessageId,omitempty"`
	Data        any    `json:"Data,omitempty"`
}
