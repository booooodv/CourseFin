package cover

import (
	"fmt"
	"strings"
)

const (
	DefaultStyleID  = "editorial_modern"
	maxTagsInPrompt = 4
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
	ID               string       `json:"id"`
	Name             string       `json:"name"`
	Tagline          string       `json:"tagline"`
	Description      string       `json:"description"`
	RecommendedFor   string       `json:"recommended_for"`
	PreviewImageURL  string       `json:"preview_image_url"`
	Palette          StylePalette `json:"palette"`
	openingScene     string
	leftSidePrefix   string
	badgeFormat      string
	titleFormat      string
	subtitleFormat   string
	noSubtitleSuffix string
	rightSideFormat  string
	minimalRightSide string
	closingStyle     string
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
		openingScene:     `A flat horizontal 16:10 Editorial Modern Swiss-grid graphic poster with clean edge-to-edge artwork on warm off-white matte fine paper (#F7F5F0) with subtle paper grain and generous outer margins.`,
		leftSidePrefix:   `On the clean open left side,`,
		badgeFormat:      `a soft cobalt rounded pill badge (#E6EEFF) shows %q in cobalt blue (#1E5EFF) above `,
		titleFormat:      `bold modern dark charcoal (#181B26) geometric sans-serif title text reading %q`,
		subtitleFormat:   `, followed by a short thin cobalt blue accent bar (#1E5EFF) and muted slate (#5A6475) subtitle text reading %q.`,
		noSubtitleSuffix: `, underlined by a short thin cobalt blue accent bar (#1E5EFF).`,
		rightSideFormat:  `On the right side, a crisp flat Swiss geometric vector emblem in cobalt blue (#1E5EFF), sky cyan (#38BDF8), and warm coral (#F97316) symbolizes %s.`,
		minimalRightSide: `On the right side, an abstract Swiss geometric vector emblem uses interlocking cobalt blue (#1E5EFF), sky cyan (#38BDF8), and warm coral (#F97316) shapes.`,
		closingStyle:     `Clean modern print design, high text contrast, crisp legible lettering, no watermarks, and no extra text.`,
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
		openingScene:     `A flat horizontal 16:10 Dark Tech modern developer graphic poster with clean edge-to-edge artwork on a deep graphite midnight-navy gradient (#0B1220 to #131F36) with a subtle architectural dot grid, soft electric cyan ambient glow on the right half, and generous outer margins.`,
		leftSidePrefix:   `On the clean dark left side,`,
		badgeFormat:      `a sleek frosted dark-glass pill badge with a thin cyan border shows %q in light cyan above `,
		titleFormat:      `bold crisp pure white (#F8FAFC) modern sans-serif title text reading %q`,
		subtitleFormat:   `, followed by cool silver-gray (#94A3B8) subtitle text reading %q.`,
		noSubtitleSuffix: `.`,
		rightSideFormat:  `On the right side, a sleek 3D frosted glassmorphism and neon-edged technical emblem with glowing cyan and electric blue light conduits, dark translucent blocks, and orbiting nodes symbolizes %s.`,
		minimalRightSide: `On the right side, a sleek abstract 3D frosted glassmorphism geometric emblem features glowing cyan and electric blue light lines and translucent blocks.`,
		closingStyle:     `Clean modern digital graphic, high text contrast, crisp legible lettering, no watermarks, and no extra text.`,
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
		openingScene:     `A flat horizontal 16:10 Soft 3D Clay studio graphic poster with clean edge-to-edge artwork on a pastel lavender-to-mist-blue gradient (#EEF2FF to #E0F2FE) with gentle studio lighting and generous outer margins.`,
		leftSidePrefix:   `On the clean pastel left side,`,
		badgeFormat:      `a soft rounded white-frosted pill badge shows %q in deep indigo (#3730A3) above `,
		titleFormat:      `bold friendly rounded deep navy (#1E293B) sans-serif title text reading %q`,
		subtitleFormat:   `, followed by muted slate-blue (#475569) subtitle text reading %q.`,
		noSubtitleSuffix: `.`,
		rightSideFormat:  `On the right side, a matte 3D clay and frosted acrylic miniature sculpture in pastel cobalt, mint, and soft coral with gentle studio shadows symbolizes %s.`,
		minimalRightSide: `On the right side, an abstract matte 3D clay and frosted acrylic geometric sculpture uses smooth rounded pastel cobalt, mint, and soft coral shapes with soft studio shadows.`,
		closingStyle:     `Tactile modern graphic design, high text contrast, crisp legible lettering, no watermarks, and no extra text.`,
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
		openingScene:     `A flat horizontal 16:10 Warm Editorial Illustration graphic poster with clean edge-to-edge artwork on warm cream apricot textured art paper (#FBF3E4) with subtle risograph print grain and generous outer margins.`,
		leftSidePrefix:   `On the clean warm paper left side,`,
		badgeFormat:      `a soft terracotta pill badge shows %q in deep terracotta (#C2410C) above `,
		titleFormat:      `bold expressive deep espresso (#231F20) sans-serif title text reading %q`,
		subtitleFormat:   `, followed by warm muted brown (#6B5B4D) subtitle text reading %q and a short terracotta accent bar underneath.`,
		noSubtitleSuffix: `, underlined by a short warm terracotta accent bar.`,
		rightSideFormat:  `On the right side, a risograph-textured flat illustration in warm terracotta orange, muted cobalt blue, sage green, and mustard yellow portrays a visual metaphor for %s.`,
		minimalRightSide: `On the right side, a risograph-textured flat abstract illustration arranges geometric organic shapes in warm terracotta orange, muted cobalt blue, sage green, and mustard yellow.`,
		closingStyle:     `Independent bookshop poster aesthetic, high text contrast, crisp legible lettering, no watermarks, and no extra text.`,
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
		openingScene:     `A flat horizontal 16:10 Minimalist Blueprint engineering handbook poster with clean edge-to-edge artwork on deep Prussian blueprint blue (#0F3566) with faint cyan-white millimeter grid lines, subtle corner crosshair marks, and generous outer margins.`,
		leftSidePrefix:   `On the clean blueprint left side,`,
		badgeFormat:      `a sharp-cornered white hairline outlined tag shows %q in pure white above `,
		titleFormat:      `bold razor-sharp pure white (#FFFFFF) geometric sans-serif title text reading %q, followed by a thin white horizontal line`,
		subtitleFormat:   ` and light-cyan (#BAE6FD) subtitle text reading %q.`,
		noSubtitleSuffix: `.`,
		rightSideFormat:  `On the right side, a crisp white-line isometric wireframe vector schematic with geometric lines, directional flow arrows, and technical tick marks (with no tiny labels inside) symbolizes %s.`,
		minimalRightSide: `On the right side, a crisp white-line abstract isometric wireframe geometric diagram uses clean technical lines, arrows, and tick marks with no labels inside.`,
		closingStyle:     `Clean technical handbook design, high text contrast, crisp legible lettering, no watermarks, and no extra text.`,
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
		openingScene:     `A flat horizontal 16:10 Cinematic Studio documentary graphic poster with clean edge-to-edge artwork on a low-key obsidian studio backdrop (#121318) with subtle warm amber-gold and cool slate rim lighting, a smooth dark vignette on the left, and generous outer margins.`,
		leftSidePrefix:   `On the dark uncluttered left side,`,
		badgeFormat:      `a muted champagne-gold hairline outlined pill badge (#D4AF6A) shows %q above `,
		titleFormat:      `bold authoritative warm-white (#F8F6F0) modern sans-serif title text reading %q, followed by a delicate champagne-gold horizontal line`,
		subtitleFormat:   ` and warm-gray (#A8A29E) subtitle text reading %q.`,
		noSubtitleSuffix: `.`,
		rightSideFormat:  `On the right side, macro studio still-life lighting with shallow depth of field depicts dark anodized metal, glass prisms, and warm golden-amber light trails symbolizing %s.`,
		minimalRightSide: `On the right side, macro studio lighting with shallow depth of field showcases dark anodized geometric metal forms, glass prisms, and warm golden-amber light trails.`,
		closingStyle:     `Prestige MasterClass poster aesthetic, high text contrast, crisp legible lettering, no watermarks, and no extra text.`,
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

// BuildPrompt renders a concise, single-paragraph cover prompt combining the selected
// template's locked art direction with the course title, optional badge/subtitle, tags, and extra cue.
func BuildPrompt(style StyleTemplate, input PromptInput) string {
	return buildSingleParagraphPrompt(style, input, false)
}

// BuildMinimalPrompt renders a stripped-down fallback cover prompt using only the style's
// abstract visual motif and the exact left-column typography, omitting topic cues, tags, and extra prompts.
func BuildMinimalPrompt(style StyleTemplate, input PromptInput) string {
	return buildSingleParagraphPrompt(style, input, true)
}

func buildSingleParagraphPrompt(style StyleTemplate, input PromptInput, minimal bool) string {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = "精选课程"
	}
	subtitle := strings.TrimSpace(input.Subtitle)
	badge := strings.TrimSpace(input.Badge)
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

	var left strings.Builder
	left.WriteString(style.leftSidePrefix)
	left.WriteByte(' ')
	if badge != "" && style.badgeFormat != "" {
		left.WriteString(fmt.Sprintf(style.badgeFormat, badge))
	}
	left.WriteString(fmt.Sprintf(style.titleFormat, title))
	if subtitle != "" && style.subtitleFormat != "" {
		left.WriteString(fmt.Sprintf(style.subtitleFormat, subtitle))
	} else {
		left.WriteString(style.noSubtitleSuffix)
	}

	var right string
	if minimal {
		right = style.minimalRightSide
	} else {
		topicCue := fmt.Sprintf("the course topic %q", title)
		if len(tags) > 0 {
			topicCue += fmt.Sprintf(" (%s)", strings.Join(tags, ", "))
		}
		if extra != "" {
			topicCue += fmt.Sprintf(", featuring %s", extra)
		}
		right = fmt.Sprintf(style.rightSideFormat, topicCue)
	}

	return strings.Join([]string{
		style.openingScene,
		left.String(),
		right,
		style.closingStyle,
	}, " ")
}
