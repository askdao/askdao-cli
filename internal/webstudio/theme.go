// [INPUT]: 依赖标准库 strings
// [OUTPUT]: 对外提供 ThemeToken / Palette / DefaultThemeForCategory / Categories
// [POS]: webstudio 的视觉品牌色板真相源 —— theme_color 存 token 名，CLI 默认 + 前端渲染共用此表；
//
//	订阅者端按同一 token→hex 契约渲染 Group 页品牌色（服务端透传 token 不碰 hex）；改 token/hex 端到端同步
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
// Token (NOT the hex) so the CLI and the subscriber-facing renderer stay aligned.
// SOURCE OF TRUTH for the token→hex table: any change to a token name or hex
// here must be mirrored in the subscriber web app's token table (kept in sync via
// CI diff); the server only passes the token string through (never hex).
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

// categoryDefaultIcon maps a coarse agent category to its default lucide icon
// name. Mirror of categoryDefault; every value MUST be in AvatarIcons (icons.go)
// and in the shared avatar icon whitelist so the frontend can render it.
var categoryDefaultIcon = map[string]string{
	"education": "graduation-cap",
	"finance":   "trending-up",
	"tech":      "code",
	"health":    "heart-pulse",
	"creative":  "palette",
	"design":    "palette",
	"business":  "briefcase",
	"data":      "database",
	"lifestyle": "leaf",
}

// DefaultAvatarForCategory returns the avatar token ("icon:<lucide-name>") to
// default for a category, falling back to "icon:bot" for unknown/empty —— so a
// deployed agent always has an icon avatar rather than a bare initial. Mirrors
// DefaultThemeForCategory; cli-side deterministic backstop when the recommend
// LLM omits avatar.
func DefaultAvatarForCategory(category string) string {
	if ic, ok := categoryDefaultIcon[strings.ToLower(strings.TrimSpace(category))]; ok {
		return "icon:" + ic
	}
	return "icon:bot"
}
