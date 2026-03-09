package tui

import "github.com/charmbracelet/lipgloss"

// ── Theme definition ──────────────────────────────────────────────────────────

// Theme holds every lipgloss style used by the TUI.
// Construct one with newTheme(palette).
type Theme struct {
	Name string

	// ── List panel ──────────────────────────────────────────────────────────
	ListPanel        lipgloss.Style
	ListPanelFoc     lipgloss.Style
	ListItem         lipgloss.Style
	ListItemSelected lipgloss.Style
	ListItemPinned   lipgloss.Style
	ListItemNormal   lipgloss.Style
	ListHeader       lipgloss.Style
	ListCount        lipgloss.Style
	Search           lipgloss.Style
	SearchActive     lipgloss.Style

	// ── Detail panel ────────────────────────────────────────────────────────
	DetailPanel    lipgloss.Style
	DetailPanelFoc lipgloss.Style
	DetailTitle    lipgloss.Style
	DetailBody     lipgloss.Style
	DetailMeta     lipgloss.Style
	Tag            lipgloss.Style
	Empty          lipgloss.Style

	// ── Editor ──────────────────────────────────────────────────────────────
	EditorPanel      lipgloss.Style
	EditorTitleLabel lipgloss.Style
	EditorBodyLabel  lipgloss.Style
	Cursor           lipgloss.Style

	// ── Status bar ──────────────────────────────────────────────────────────
	StatusBar lipgloss.Style
	StatusKey lipgloss.Style
	StatusSep lipgloss.Style
	StatusMsg lipgloss.Style
	StatusErr lipgloss.Style

	// ── Help overlay ────────────────────────────────────────────────────────
	HelpOverlay lipgloss.Style
	HelpTitle   lipgloss.Style
	HelpKey     lipgloss.Style
	HelpDesc    lipgloss.Style

	// ── App header ──────────────────────────────────────────────────────────
	AppHeader  lipgloss.Style
	AppVersion lipgloss.Style
}

// palette is the minimal set of named colors a theme needs to define.
type palette struct {
	bg         lipgloss.Color
	surface    lipgloss.Color
	border     lipgloss.Color
	borderFoc  lipgloss.Color
	text       lipgloss.Color
	muted      lipgloss.Color
	accent     lipgloss.Color
	accentDim  lipgloss.Color
	selected   lipgloss.Color
	selectedFg lipgloss.Color
	pin        lipgloss.Color
	tag        lipgloss.Color
	tagBg      lipgloss.Color
	danger     lipgloss.Color
	success    lipgloss.Color
	title      lipgloss.Color
	subtle     lipgloss.Color
}

// newTheme constructs the full set of lipgloss styles from a palette.
func newTheme(name string, p palette) Theme {
	return Theme{
		Name: name,

		// List panel
		ListPanel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.border).
			Padding(0, 0),
		ListPanelFoc: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.borderFoc).
			Padding(0, 0),
		ListItem: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(p.text),
		ListItemSelected: lipgloss.NewStyle().
			Padding(0, 1).
			Background(p.selected).
			Foreground(p.selectedFg).
			Bold(true),
		ListItemPinned: lipgloss.NewStyle().
			Foreground(p.pin).
			SetString("⏺ "),
		ListItemNormal: lipgloss.NewStyle().
			Foreground(p.muted).
			SetString("  "),
		ListHeader: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(p.accent).
			Bold(true),
		ListCount: lipgloss.NewStyle().
			Foreground(p.muted).
			Italic(true),
		Search: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(p.text),
		SearchActive: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(p.accent),

		// Detail panel
		DetailPanel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.border).
			Padding(0, 1),
		DetailPanelFoc: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.borderFoc).
			Padding(0, 1),
		DetailTitle: lipgloss.NewStyle().
			Foreground(p.title).
			Bold(true).
			MarginBottom(1),
		DetailBody: lipgloss.NewStyle().
			Foreground(p.text),
		DetailMeta: lipgloss.NewStyle().
			Foreground(p.subtle).
			Italic(true),
		Tag: lipgloss.NewStyle().
			Foreground(p.tag).
			Background(p.tagBg).
			Padding(0, 1).
			MarginRight(1),
		Empty: lipgloss.NewStyle().
			Foreground(p.muted).
			Italic(true).
			Align(lipgloss.Center),

		// Editor
		EditorPanel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.borderFoc).
			Padding(0, 1),
		EditorTitleLabel: lipgloss.NewStyle().
			Foreground(p.accent).
			Bold(true),
		EditorBodyLabel: lipgloss.NewStyle().
			Foreground(p.muted),
		Cursor: lipgloss.NewStyle().
			Background(p.accent).
			Foreground(p.bg),

		// Status bar
		StatusBar: lipgloss.NewStyle().
			Foreground(p.muted).
			PaddingLeft(1),
		StatusKey: lipgloss.NewStyle().
			Foreground(p.accent).
			Bold(true),
		StatusSep: lipgloss.NewStyle().
			Foreground(p.border),
		StatusMsg: lipgloss.NewStyle().
			Foreground(p.success).
			Bold(true),
		StatusErr: lipgloss.NewStyle().
			Foreground(p.danger).
			Bold(true),

		// Help overlay
		HelpOverlay: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.accent).
			Background(p.surface).
			Padding(0, 3),
		HelpTitle: lipgloss.NewStyle().
			Foreground(p.accent).
			Bold(true),
		HelpKey: lipgloss.NewStyle().
			Foreground(p.selectedFg).
			Bold(true).
			Width(14),
		HelpDesc: lipgloss.NewStyle().
			Foreground(p.text),

		// App header
		AppHeader: lipgloss.NewStyle().
			Foreground(p.accent).
			Bold(true).
			PaddingLeft(1),
		AppVersion: lipgloss.NewStyle().
			Foreground(p.muted).
			PaddingLeft(1),
	}
}

// ── Built-in themes ───────────────────────────────────────────────────────────

// ThemeAmber is the original dark amber theme shipped with nnn.
var ThemeAmber = newTheme("amber", palette{
	bg:         lipgloss.Color("#0d0f14"),
	surface:    lipgloss.Color("#13161e"),
	border:     lipgloss.Color("#2a2d3a"),
	borderFoc:  lipgloss.Color("#e8a045"),
	text:       lipgloss.Color("#c9cdd6"),
	muted:      lipgloss.Color("#4a4e5c"),
	accent:     lipgloss.Color("#e8a045"),
	accentDim:  lipgloss.Color("#a06820"),
	selected:   lipgloss.Color("#1e2130"),
	selectedFg: lipgloss.Color("#f0c070"),
	pin:        lipgloss.Color("#5b9cf6"),
	tag:        lipgloss.Color("#6ec994"),
	tagBg:      lipgloss.Color("#1a2e22"),
	danger:     lipgloss.Color("#e05c5c"),
	success:    lipgloss.Color("#6ec994"),
	title:      lipgloss.Color("#f0c070"),
	subtle:     lipgloss.Color("#6b7080"),
})

// ThemeCatppuccin is Catppuccin Mocha — warm dark with pastel accents.
var ThemeCatppuccin = newTheme("catppuccin", palette{
	bg:         lipgloss.Color("#1e1e2e"),
	surface:    lipgloss.Color("#313244"),
	border:     lipgloss.Color("#45475a"),
	borderFoc:  lipgloss.Color("#cba6f7"),
	text:       lipgloss.Color("#cdd6f4"),
	muted:      lipgloss.Color("#6c7086"),
	accent:     lipgloss.Color("#cba6f7"),
	accentDim:  lipgloss.Color("#9374c0"),
	selected:   lipgloss.Color("#313244"),
	selectedFg: lipgloss.Color("#f5c2e7"),
	pin:        lipgloss.Color("#89b4fa"),
	tag:        lipgloss.Color("#a6e3a1"),
	tagBg:      lipgloss.Color("#1e3a2e"),
	danger:     lipgloss.Color("#f38ba8"),
	success:    lipgloss.Color("#a6e3a1"),
	title:      lipgloss.Color("#f5c2e7"),
	subtle:     lipgloss.Color("#585b70"),
})

// ThemeTokyoNight is Tokyo Night — cool blue/purple dark theme.
var ThemeTokyoNight = newTheme("tokyo-night", palette{
	bg:         lipgloss.Color("#1a1b26"),
	surface:    lipgloss.Color("#24283b"),
	border:     lipgloss.Color("#3b4261"),
	borderFoc:  lipgloss.Color("#7aa2f7"),
	text:       lipgloss.Color("#c0caf5"),
	muted:      lipgloss.Color("#565f89"),
	accent:     lipgloss.Color("#7aa2f7"),
	accentDim:  lipgloss.Color("#3d59a1"),
	selected:   lipgloss.Color("#283457"),
	selectedFg: lipgloss.Color("#bb9af7"),
	pin:        lipgloss.Color("#2ac3de"),
	tag:        lipgloss.Color("#9ece6a"),
	tagBg:      lipgloss.Color("#1a2e12"),
	danger:     lipgloss.Color("#f7768e"),
	success:    lipgloss.Color("#9ece6a"),
	title:      lipgloss.Color("#bb9af7"),
	subtle:     lipgloss.Color("#414868"),
})

// ThemeGruvbox is Gruvbox Dark — retro warm earth tones.
var ThemeGruvbox = newTheme("gruvbox", palette{
	bg:         lipgloss.Color("#282828"),
	surface:    lipgloss.Color("#32302f"),
	border:     lipgloss.Color("#504945"),
	borderFoc:  lipgloss.Color("#d79921"),
	text:       lipgloss.Color("#ebdbb2"),
	muted:      lipgloss.Color("#665c54"),
	accent:     lipgloss.Color("#d79921"),
	accentDim:  lipgloss.Color("#b57614"),
	selected:   lipgloss.Color("#3c3836"),
	selectedFg: lipgloss.Color("#fabd2f"),
	pin:        lipgloss.Color("#83a598"),
	tag:        lipgloss.Color("#b8bb26"),
	tagBg:      lipgloss.Color("#1d2414"),
	danger:     lipgloss.Color("#fb4934"),
	success:    lipgloss.Color("#b8bb26"),
	title:      lipgloss.Color("#fabd2f"),
	subtle:     lipgloss.Color("#7c6f64"),
})

// ThemeNord is Nord — arctic bluish dark with cool pale colors.
var ThemeNord = newTheme("nord", palette{
	bg:         lipgloss.Color("#2e3440"),
	surface:    lipgloss.Color("#3b4252"),
	border:     lipgloss.Color("#434c5e"),
	borderFoc:  lipgloss.Color("#88c0d0"),
	text:       lipgloss.Color("#eceff4"),
	muted:      lipgloss.Color("#4c566a"),
	accent:     lipgloss.Color("#88c0d0"),
	accentDim:  lipgloss.Color("#5e81ac"),
	selected:   lipgloss.Color("#434c5e"),
	selectedFg: lipgloss.Color("#81a1c1"),
	pin:        lipgloss.Color("#b48ead"),
	tag:        lipgloss.Color("#a3be8c"),
	tagBg:      lipgloss.Color("#1e2c1a"),
	danger:     lipgloss.Color("#bf616a"),
	success:    lipgloss.Color("#a3be8c"),
	title:      lipgloss.Color("#81a1c1"),
	subtle:     lipgloss.Color("#616e88"),
})

// ThemeSolarized is Solarized Light — light background with warm and cool accents.
var ThemeSolarized = newTheme("solarized", palette{
	bg:         lipgloss.Color("#fdf6e3"),
	surface:    lipgloss.Color("#eee8d5"),
	border:     lipgloss.Color("#93a1a1"),
	borderFoc:  lipgloss.Color("#268bd2"),
	text:       lipgloss.Color("#657b83"),
	muted:      lipgloss.Color("#93a1a1"),
	accent:     lipgloss.Color("#268bd2"),
	accentDim:  lipgloss.Color("#1a6091"),
	selected:   lipgloss.Color("#eee8d5"),
	selectedFg: lipgloss.Color("#073642"),
	pin:        lipgloss.Color("#6c71c4"),
	tag:        lipgloss.Color("#2aa198"),
	tagBg:      lipgloss.Color("#d4eeec"),
	danger:     lipgloss.Color("#dc322f"),
	success:    lipgloss.Color("#859900"),
	title:      lipgloss.Color("#073642"),
	subtle:     lipgloss.Color("#839496"),
})

// ThemeDracula is Dracula — dark theme with vibrant purple and pink accents.
var ThemeDracula = newTheme("dracula", palette{
	bg:         lipgloss.Color("#282a36"),
	surface:    lipgloss.Color("#44475a"),
	border:     lipgloss.Color("#6272a4"),
	borderFoc:  lipgloss.Color("#bd93f9"),
	text:       lipgloss.Color("#f8f8f2"),
	muted:      lipgloss.Color("#6272a4"),
	accent:     lipgloss.Color("#bd93f9"),
	accentDim:  lipgloss.Color("#7b5ea7"),
	selected:   lipgloss.Color("#44475a"),
	selectedFg: lipgloss.Color("#ff79c6"),
	pin:        lipgloss.Color("#8be9fd"),
	tag:        lipgloss.Color("#50fa7b"),
	tagBg:      lipgloss.Color("#1a3522"),
	danger:     lipgloss.Color("#ff5555"),
	success:    lipgloss.Color("#50fa7b"),
	title:      lipgloss.Color("#ff79c6"),
	subtle:     lipgloss.Color("#6272a4"),
})

// ── Theme registry ────────────────────────────────────────────────────────────

// AllThemes is the ordered list of available themes.
var AllThemes = []Theme{
	ThemeAmber,
	ThemeCatppuccin,
	ThemeTokyoNight,
	ThemeGruvbox,
	ThemeNord,
	ThemeSolarized,
	ThemeDracula,
}

// ThemeByName returns the theme matching name (case-insensitive).
// Falls back to ThemeAmber when no match is found.
func ThemeByName(name string) Theme {
	for _, t := range AllThemes {
		if t.Name == name {
			return t
		}
	}
	return ThemeAmber
}
