// [INPUT]: 依赖标准库 strings
// [OUTPUT]: 对外提供 ThemeToken / Palette / DefaultThemeForCategory / Categories
// [POS]: webstudio 的视觉品牌色板真相源 —— theme_color 存 token 名，CLI 默认 + 前端渲染共用此表；
//
//	conductor 纯透传 token（不碰 hex）、askdao-ai-web 镜像此表（src/config/style/theme-tokens.ts + theme.css）渲染订阅者 Group 页品牌色；改 token/hex 三端同步
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package webstudio

import "strings"

// ThemeToken is one preset palette entry: a stable token name (what gets stored
// in metadata.theme_color), its hex value, and a short picker label.
type ThemeToken struct {
	Token string `json:"token"`
	Hex   string `json:"hex"`
	Label string `json:"label"`
}

// Palette is the ordered preset color palette. metadata.theme_color stores the
// Token (NOT the hex) so CLI / conductor / askdao-ai-web stay aligned on render.
// SOURCE OF TRUTH for the token→hex table: any change to a token name or hex
// here must be mirrored in askdao-ai-web src/config/style/{theme-tokens.ts,
// theme.css}; conductor only passes the token string through (never hex).
var Palette = []ThemeToken{
	{"sunset", "#FF6B35", "Sunset"},
	{"ocean", "#2E86DE", "Ocean"},
	{"forest", "#27AE60", "Forest"},
	{"berry", "#8E44AD", "Berry"},
	{"amber", "#F39C12", "Amber"},
	{"teal", "#16A085", "Teal"},
	{"rose", "#E84393", "Rose"},
	{"slate", "#475569", "Slate"},
}

// Categories is the preset agent-category list offered in the studio dropdown.
var Categories = []string{
	"education", "finance", "health", "tech",
	"creative", "business", "data", "lifestyle", "other",
}

// categoryDefault maps a coarse agent category to its default palette token.
var categoryDefault = map[string]string{
	"education": "sunset",
	"finance":   "ocean",
	"tech":      "ocean",
	"health":    "forest",
	"creative":  "berry",
	"design":    "berry",
	"business":  "amber",
	"data":      "teal",
	"lifestyle": "rose",
}

// DefaultThemeForCategory returns the palette token to default for a category,
// falling back to "slate" for unknown/empty categories.
func DefaultThemeForCategory(category string) string {
	if t, ok := categoryDefault[strings.ToLower(strings.TrimSpace(category))]; ok {
		return t
	}
	return "slate"
}
