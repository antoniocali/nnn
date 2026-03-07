package tui

import "github.com/charmbracelet/lipgloss"

// Palette — dark terminal aesthetic with amber accents.
var (
	// Base colors
	colorBg         = lipgloss.Color("#0d0f14")
	colorSurface    = lipgloss.Color("#13161e")
	colorBorder     = lipgloss.Color("#2a2d3a")
	colorBorderFoc  = lipgloss.Color("#e8a045")
	colorText       = lipgloss.Color("#c9cdd6")
	colorMuted      = lipgloss.Color("#4a4e5c")
	colorAccent     = lipgloss.Color("#e8a045")
	colorAccentDim  = lipgloss.Color("#a06820")
	colorSelected   = lipgloss.Color("#1e2130")
	colorSelectedFg = lipgloss.Color("#f0c070")
	colorPin        = lipgloss.Color("#5b9cf6")
	colorTag        = lipgloss.Color("#6ec994")
	colorDanger     = lipgloss.Color("#e05c5c")
	colorSuccess    = lipgloss.Color("#6ec994")
	colorTitle      = lipgloss.Color("#f0c070")
	colorSubtle     = lipgloss.Color("#6b7080")

	// ── List panel ──────────────────────────────────────────────────────────

	listPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 0)

	listPanelFocStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorBorderFoc).
				Padding(0, 0)

	listItemStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(colorText)

	listItemSelectedStyle = lipgloss.NewStyle().
				Padding(0, 1).
				Background(colorSelected).
				Foreground(colorSelectedFg).
				Bold(true)

	listItemPinnedMark = lipgloss.NewStyle().
				Foreground(colorPin).
				SetString("⏺ ")

	listItemNormalMark = lipgloss.NewStyle().
				Foreground(colorMuted).
				SetString("  ")

	listHeaderStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(colorAccent).
			Bold(true)

	listCountStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	searchStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(colorText)

	searchActiveStyle = lipgloss.NewStyle().
				Padding(0, 1).
				Foreground(colorAccent)

	// ── Detail panel ────────────────────────────────────────────────────────

	detailPanelStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorBorder).
				Padding(0, 1)

	detailPanelFocStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorBorderFoc).
				Padding(0, 1)

	detailTitleStyle = lipgloss.NewStyle().
				Foreground(colorTitle).
				Bold(true).
				MarginBottom(1)

	detailBodyStyle = lipgloss.NewStyle().
			Foreground(colorText)

	detailMetaStyle = lipgloss.NewStyle().
			Foreground(colorSubtle).
			Italic(true)

	tagStyle = lipgloss.NewStyle().
			Foreground(colorTag).
			Background(lipgloss.Color("#1a2e22")).
			Padding(0, 1).
			MarginRight(1)

	emptyStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true).
			Align(lipgloss.Center)

	// ── Editor ──────────────────────────────────────────────────────────────

	editorPanelStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorBorderFoc).
				Padding(0, 1)

	editorTitleLabelStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	editorBodyLabelStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	cursorStyle = lipgloss.NewStyle().
			Background(colorAccent).
			Foreground(colorBg)

	// ── Status bar ──────────────────────────────────────────────────────────

	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			PaddingLeft(1)

	statusKeyStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	statusSepStyle = lipgloss.NewStyle().
			Foreground(colorBorder)

	statusMsgStyle = lipgloss.NewStyle().
			Foreground(colorSuccess).
			Bold(true)

	statusErrStyle = lipgloss.NewStyle().
			Foreground(colorDanger).
			Bold(true)

	// ── Help overlay ────────────────────────────────────────────────────────

	helpOverlayStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorAccent).
				Background(colorSurface).
				Padding(0, 3)

	helpTitleStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colorSelectedFg).
			Bold(true).
			Width(14)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(colorText)

	// ── App header ──────────────────────────────────────────────────────────

	appHeaderStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true).
			PaddingLeft(1)

	appVersionStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			PaddingLeft(1)
)
