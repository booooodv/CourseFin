package cover

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	DefaultStyleID = "editorial_modern"
	maxOverviewRunes = 160
	maxTagsInPrompt  = 6
	maxChaptersHint  = 6
)

// StylePalette holds preview color tokens used by frontend cards before or alongside sample image inspection.
type StylePalette struct {
	Background string `json:"background"`
	Surface    string `json:"surface"`
	Text       string `json:"text"`
	Accent     string `json:"accent"`
	AccentSoft string `json:"accent_soft"`
}

// StyleTemplate describes a built-in cohesive course cover art direction preset.
type StyleTemplate struct {
	ID                   string       `json:"id"`
	Name                 string       `json:"name"`
	Tagline              string       `json:"tagline"`
	Description          string       `json:"description"`
	RecommendedFor       string       `json:"recommended_for"`
	PreviewImageURL      string       `json:"preview_image_url"`
	Palette              StylePalette `json:"palette"`
	styleHeading         string
	backgroundSpec       string
	leftColumnBackdrop   string
	badgeStyleSpec       string
	titleStyleSpec       string
	subtitleStyleSpec    string
	noSubtitleAccentSpec string
	rightColumnSpec      string
}

// PromptInput carries course metadata and optional user overrides used to construct an image prompt.
type PromptInput struct {
	Title         string
	Subtitle      string
	Badge         string
	Overview      string
	Tags          []string
	ChapterTitles []string
	ExtraPrompt   string
}

var builtInStyles = []StyleTemplate{
	{
		ID:              "editorial_modern",
		Name:            "现代书封",
		Tagline:         "温润米白 · 瑞士双栏",
		Description:     "温润米白纸纹底搭配左侧粗黑大标题与右侧现代几何矢量主视觉，整齐耐看。",
		RecommendedFor:  "编程、通识、商业与综合类课程",
		PreviewImageURL: "/web/cover-styles/editorial_modern.jpg",
		Palette: StylePalette{
			Background: "#F7F5F0",
			Surface:    "#E6EEFF",
			Text:       "#181B26",
			Accent:     "#1E5EFF",
			AccentSoft: "#F97316",
		},
		styleHeading:         `Style: "现代书封 (Editorial Modern)" Swiss grid fine book cover.`,
		backgroundSpec:       `- Background: warm off-white matte fine paper (#F7F5F0) with ultra-subtle paper grain and generous outer safe margins (at least 12% padding on all edges).`,
		leftColumnBackdrop:   `Left column (58% width): completely clean unobstructed paper behind typography.`,
		badgeStyleSpec:       `Badge style: soft cobalt rounded pill (#E6EEFF) with cobalt text (#1E5EFF).`,
		titleStyleSpec:       `Main title style: bold modern geometric sans-serif in deep charcoal (#181B26).`,
		subtitleStyleSpec:    `Below the title: a short thin cobalt accent bar (#1E5EFF) and clean muted slate subtitle (#5A6475).`,
		noSubtitleAccentSpec: `Below the title: a short thin cobalt accent bar (#1E5EFF).`,
		rightColumnSpec:      `- Right column (38% width): crisp Swiss geometric vector emblem symbolizing the course topic, using interlocking cobalt blue (#1E5EFF), sky cyan (#38BDF8), and warm coral (#F97316) clean shapes and directional motifs. Flat refined print aesthetic.`,
	},
	{
		ID:              "dark_tech",
		Name:            "暗夜科技",
		Tagline:         "深邃蓝黑 · 玻璃光效",
		Description:     "深石墨蓝黑背景搭配细微坐标点阵、磨砂青蓝角标与发光玻璃态科技主视觉。",
		RecommendedFor:  "工程架构、AI 算法、后端与运维课程",
		PreviewImageURL: "/web/cover-styles/dark_tech.jpg",
		Palette: StylePalette{
			Background: "#0B1220",
			Surface:    "#131F36",
			Text:       "#F8FAFC",
			Accent:     "#38BDF8",
			AccentSoft: "#1E40AF",
		},
		styleHeading:       `Style: "暗夜科技 (Dark Tech)" modern developer platform cover.`,
		backgroundSpec:     `- Background: deep graphite midnight-navy gradient (#0B1220 to #131F36) with ultra-subtle architectural dot grid and a soft electric blue-cyan ambient glow localized on the right half. Generous outer safe margins (at least 12% padding on all edges).`,
		leftColumnBackdrop: `Left column (58% width): dark and unobstructed for high-contrast legibility.`,
		badgeStyleSpec:     `Badge style: sleek frosted dark-glass pill with subtle cyan border and light-cyan text.`,
		titleStyleSpec:     `Main title style: bold crisp pure white (#F8FAFC) modern sans-serif.`,
		subtitleStyleSpec:  `Subtitle style: cool silver-gray (#94A3B8).`,
		rightColumnSpec:    `- Right column (38% width): sleek 3D frosted glassmorphism and neon-edged technical emblem representing the course subject, with luminous cyan and electric blue light conduits, dark translucent blocks, and orbiting nodes.`,
	},
	{
		ID:              "clay_3d",
		Name:            "柔和 3D 黏土",
		Tagline:         "莫兰迪渐变 · 磨砂立体",
		Description:     "柔和淡紫蓝渐变背景搭配圆润深海军蓝标题与右侧精巧磨砂 3D 黏土微缩模型。",
		RecommendedFor:  "设计、产品、效率工具与入门通识课",
		PreviewImageURL: "/web/cover-styles/clay_3d.jpg",
		Palette: StylePalette{
			Background: "#EEF2FF",
			Surface:    "#E0F2FE",
			Text:       "#1E293B",
			Accent:     "#4F46E5",
			AccentSoft: "#FB923C",
		},
		styleHeading:       `Style: "柔和 3D 黏土 (Soft 3D Clay)" tactile studio course cover.`,
		backgroundSpec:     `- Background: soft clean pastel lavender-to-mist-blue gradient (#EEF2FF to #E0F2FE) with gentle studio illumination and generous outer safe margins (at least 12% padding on all edges).`,
		leftColumnBackdrop: `Left column (55% width): clean unobstructed pastel backdrop behind typography.`,
		badgeStyleSpec:     `Badge style: soft rounded white-frosted pill with deep indigo text (#3730A3).`,
		titleStyleSpec:     `Main title style: bold friendly rounded sans-serif in deep navy (#1E293B).`,
		subtitleStyleSpec:  `Subtitle style: muted slate-blue (#475569).`,
		rightColumnSpec:    `- Right column (42% width): charming high-craft matte 3D clay and frosted acrylic miniature sculpture symbolizing the course topic, featuring smooth rounded pastel cobalt, mint, and soft coral objects with gentle studio shadows.`,
	},
	{
		ID:              "warm_illustration",
		Name:            "温暖扁平插画",
		Tagline:         "暖杏纸纹 · 编辑部插画",
		Description:     "暖杏奶油纸感底搭配深炭文字与右侧孔版印刷质感扁平插画，亲和富有故事感。",
		RecommendedFor:  "语言、人文、摄影与生活方式课程",
		PreviewImageURL: "/web/cover-styles/warm_illustration.jpg",
		Palette: StylePalette{
			Background: "#FBF3E4",
			Surface:    "#FDE8D7",
			Text:       "#231F20",
			Accent:     "#EA580C",
			AccentSoft: "#2563EB",
		},
		styleHeading:         `Style: "温暖扁平插画 (Warm Editorial Illustration)" independent bookshop series cover.`,
		backgroundSpec:       `- Background: warm cream apricot textured art paper (#FBF3E4) with subtle risograph print grain and generous outer safe margins (at least 12% padding on all edges).`,
		leftColumnBackdrop:   `Left column (56% width): clean paper backdrop behind typography.`,
		badgeStyleSpec:       `Badge style: warm soft terracotta pill tag with deep terracotta text (#C2410C).`,
		titleStyleSpec:       `Main title style: bold expressive modern sans-serif in deep espresso charcoal (#231F20).`,
		subtitleStyleSpec:    `Subtitle style: warm muted brown (#6B5B4D) with a short warm-terracotta accent bar underneath.`,
		noSubtitleAccentSpec: `Below the title: a short warm-terracotta accent bar.`,
		rightColumnSpec:      `- Right column (40% width): modern editorial risograph-textured flat illustration in warm terracotta orange, muted cobalt blue, sage green, and mustard yellow portraying an imaginative visual metaphor for the course subject.`,
	},
	{
		ID:              "blueprint_grid",
		Name:            "极简蓝图",
		Tagline:         "普鲁士蓝 · 精密线稿",
		Description:     "深普鲁士蓝工程坐标网格搭配锐利纯白文字与白色等轴测技术线稿，极具工科秩序感。",
		RecommendedFor:  "计算机原理、数学、硬件与底层源码课",
		PreviewImageURL: "/web/cover-styles/blueprint_grid.jpg",
		Palette: StylePalette{
			Background: "#0F3566",
			Surface:    "#174680",
			Text:       "#FFFFFF",
			Accent:     "#BAE6FD",
			AccentSoft: "#38BDF8",
		},
		styleHeading:       `Style: "极简蓝图 (Minimalist Blueprint)" engineering handbook cover.`,
		backgroundSpec:     `- Background: deep Prussian architectural blueprint blue (#0F3566) with crisp faint cyan-white millimeter engineering grid lines and subtle minimal corner crosshair marks. Generous outer safe margins (at least 12% padding on all edges).`,
		leftColumnBackdrop: `Left column (56% width): clean grid behind crisp typography.`,
		badgeStyleSpec:     `Badge style: sharp-cornered outlined engineering tag in white hairline border with pure white text.`,
		titleStyleSpec:     `Main title style: bold razor-sharp geometric sans-serif in pure white (#FFFFFF), followed by a thin white horizontal rule.`,
		subtitleStyleSpec:  `Subtitle style: light-cyan (#BAE6FD) below the horizontal rule.`,
		rightColumnSpec:    `- Right column (40% width): ultra-clean architectural white-line isometric wireframe schematic illustrating the course subject using crisp geometric lines, directional flow arrows, and technical tick marks (NO tiny pseudo-labels or unreadable text inside the diagram).`,
	},
	{
		ID:              "cinematic_studio",
		Name:            "质感影院",
		Tagline:         "暗调影棚 · 香槟金边",
		Description:     "低键黑曜石影棚光影与暗角搭配香槟金细线、暖白大标题与微景深主题静物摄影。",
		RecommendedFor:  "影视制作、音乐艺术、大师课系列",
		PreviewImageURL: "/web/cover-styles/cinematic_studio.jpg",
		Palette: StylePalette{
			Background: "#121318",
			Surface:    "#1F2129",
			Text:       "#F8F6F0",
			Accent:     "#D4AF6A",
			AccentSoft: "#A8A29E",
		},
		styleHeading:       `Style: "质感影院 (Cinematic Studio)" prestige documentary / MasterClass series cover.`,
		backgroundSpec:     `- Background: moody low-key obsidian studio backdrop (#121318) with subtle directional warm amber-gold and cool slate rim lighting, deep smooth dark vignette on the left for immaculate text contrast, and generous outer safe margins (at least 12% padding on all edges).`,
		leftColumnBackdrop: `Left column (58% width): refined dark backdrop behind typography.`,
		badgeStyleSpec:     `Badge style: muted champagne-gold hairline outlined pill (#D4AF6A).`,
		titleStyleSpec:     `Main title style: bold authoritative modern sans-serif in crisp warm-white (#F8F6F0), followed by a delicate champagne-gold horizontal rule.`,
		subtitleStyleSpec:  `Subtitle style: warm-gray (#A8A29E) below the horizontal rule.`,
		rightColumnSpec:    `- Right column (42% width): prestige macro studio still-life photography with cinematic shallow depth of field depicting symbolic physical objects, dark anodized metal, glass prisms, and warm golden-amber light trails inspired by the course topic.`,
	},
}

// ListStyles returns copies of all built-in cover style templates in display order.
func ListStyles() []StyleTemplate {
	out := make([]StyleTemplate, len(builtInStyles))
	copy(out, builtInStyles)
	return out
}

// GetStyle looks up a built-in style template by ID.
func GetStyle(styleID string) (StyleTemplate, bool) {
	norm := strings.ToLower(strings.TrimSpace(styleID))
	for _, st := range builtInStyles {
		if st.ID == norm {
			return st, true
		}
	}
	return StyleTemplate{}, false
}

// BuildPrompt renders a cohesive end-to-end course cover prompt combining the selected
// template's locked art direction with the specific course's text and thematic cues.
func BuildPrompt(style StyleTemplate, input PromptInput) string {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = "精选课程"
	}
	subtitle := strings.TrimSpace(input.Subtitle)
	badge := strings.TrimSpace(input.Badge)
	overview := truncateRunes(strings.TrimSpace(input.Overview), maxOverviewRunes)
	extra := strings.TrimSpace(input.ExtraPrompt)

	var tags []string
	for _, t := range input.Tags {
		if trimmed := strings.TrimSpace(t); trimmed != "" {
			tags = append(tags, trimmed)
			if len(tags) >= maxTagsInPrompt {
				break
			}
		}
	}

	var chapters []string
	for _, ch := range input.ChapterTitles {
		if trimmed := strings.TrimSpace(ch); trimmed != "" {
			chapters = append(chapters, trimmed)
			if len(chapters) >= maxChaptersHint {
				break
			}
		}
	}

	var b strings.Builder
	b.WriteString("Create a brand-new standalone horizontal 16:10 digital course cover illustration from scratch (flat edge-to-edge graphic poster, no physical book mockup, no outer frame, no border shadows).\n\n")
	b.WriteString(style.styleHeading)
	b.WriteByte('\n')
	b.WriteString(style.backgroundSpec)
	b.WriteString("\n- ")
	b.WriteString(style.leftColumnBackdrop)
	if badge != "" && style.badgeStyleSpec != "" {
		b.WriteByte(' ')
		b.WriteString(style.badgeStyleSpec)
	}
	b.WriteByte(' ')
	b.WriteString(style.titleStyleSpec)
	if subtitle != "" && style.subtitleStyleSpec != "" {
		b.WriteByte(' ')
		b.WriteString(style.subtitleStyleSpec)
	} else if style.noSubtitleAccentSpec != "" {
		b.WriteByte(' ')
		b.WriteString(style.noSubtitleAccentSpec)
	}
	b.WriteByte('\n')
	b.WriteString(style.rightColumnSpec)
	b.WriteString("\n\nExact Text to Render in the Left Column (render ONLY these exact strings crisply, with zero typos and no extra words, badges, subtitles, or labels anywhere in the poster):\n")
	if badge != "" {
		b.WriteString(fmt.Sprintf("- Top badge pill: %q\n", badge))
	}
	b.WriteString(fmt.Sprintf("- Main title (large bold headline, wrapped cleanly across 1-3 lines): %q\n", title))
	if subtitle != "" {
		b.WriteString(fmt.Sprintf("- Subtitle below divider: %q\n", subtitle))
	}

	b.WriteString("\nRight-Side Visual Theme (illustrate a symbolic emblem on the right inspired by these subjects; draw purely visual shapes and motifs, never write these keywords as text):\n")
	b.WriteString(fmt.Sprintf("- Subject: %s\n", title))
	if len(tags) > 0 {
		b.WriteString(fmt.Sprintf("- Domain tags: %s\n", strings.Join(tags, ", ")))
	}
	if len(chapters) > 0 {
		b.WriteString(fmt.Sprintf("- Key module themes: %s\n", strings.Join(chapters, "; ")))
	}
	if overview != "" {
		b.WriteString(fmt.Sprintf("- Course focus: %s\n", overview))
	}
	if extra != "" {
		b.WriteString(fmt.Sprintf("- Extra visual elements: %s\n", extra))
	}

	b.WriteString("\nVisual Quality: high legibility with strong contrast and an uncluttered backdrop behind the left-column typography; no watermarks, no frames, and no random decorative English/Chinese gibberish text.")
	return b.String()
}

func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 || s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	count := 0
	for idx := range s {
		if count == maxRunes {
			return s[:idx] + "…"
		}
		count++
	}
	return s
}
