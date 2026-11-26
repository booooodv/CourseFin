package cover

import (
	"strings"

	"coursefin/internal/ai"
)

// EffectiveImageSettings contains resolved AI image generation settings, readiness state,
// and the global default cover style template ID.
type EffectiveImageSettings struct {
	Image          ai.ImageConfig
	Configured     bool
	DefaultStyleID string
	RawBaseURL     string
	RawAPIKeySet   bool
}

// ResolveEffectiveImageSettings resolves AI image settings from raw app_settings rows.
// To prevent accidental credential leaks, the primary ai_api_key is ONLY reused for
// OpenAI-compatible protocols when ai_image_base_url is left empty (reusing ai_base_url).
// Custom ai_image_base_url or the native gemini protocol never inherits ai_api_key.
func ResolveEffectiveImageSettings(settings map[string]string) EffectiveImageSettings {
	protocol := ai.NormalizeImageProtocol(settings["ai_image_protocol"])
	rawBaseURL := strings.TrimSpace(settings["ai_image_base_url"])
	rawAPIKey := strings.TrimSpace(settings["ai_image_api_key"])
	model := strings.TrimSpace(settings["ai_image_model"])
	size := strings.TrimSpace(settings["ai_image_size"])

	aiBaseURL := strings.TrimSpace(settings["ai_base_url"])
	aiAPIKey := strings.TrimSpace(settings["ai_api_key"])

	var effectiveBaseURL string
	var effectiveAPIKey string
	var configured bool

	switch protocol {
	case ai.ImageProtocolGemini:
		effectiveBaseURL = rawBaseURL
		if effectiveBaseURL == "" {
			effectiveBaseURL = ai.DefaultGeminiBaseURL
		}
		effectiveAPIKey = rawAPIKey
		if rawBaseURL == "" {
			configured = effectiveAPIKey != "" && model != ""
		} else {
			configured = effectiveBaseURL != "" && model != ""
		}
	default:
		effectiveBaseURL = rawBaseURL
		if effectiveBaseURL == "" {
			effectiveBaseURL = aiBaseURL
		}
		effectiveAPIKey = rawAPIKey
		if effectiveAPIKey == "" && rawBaseURL == "" {
			effectiveAPIKey = aiAPIKey
		}
		configured = effectiveBaseURL != "" && model != ""
	}

	defaultStyle := strings.TrimSpace(settings["ai_cover_default_style"])
	if st, ok := GetStyle(defaultStyle); ok {
		defaultStyle = st.ID
	} else {
		defaultStyle = DefaultStyleID
	}

	return EffectiveImageSettings{
		Image: ai.ImageConfig{
			Protocol: protocol,
			BaseURL:  effectiveBaseURL,
			APIKey:   effectiveAPIKey,
			Model:    model,
			Size:     size,
		},
		Configured:     configured,
		DefaultStyleID: defaultStyle,
		RawBaseURL:     rawBaseURL,
		RawAPIKeySet:   rawAPIKey != "",
	}
}
