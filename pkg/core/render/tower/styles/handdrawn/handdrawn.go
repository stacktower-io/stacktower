package handdrawn

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/matzehuels/stacktower/pkg/core/render/tower/styles"
	"github.com/matzehuels/stacktower/pkg/fonts"
	"github.com/matzehuels/stacktower/pkg/security"
)

const (
	// Flag icon dimensions (pennant anchored at the block's top edge)
	flagPoleH   = 36.0 // height of the flag pole
	flagW       = 26.0 // width of the flag pennant
	flagH       = 18.0 // height of the pennant triangle
	flagPadX    = 18.0 // padding from the right edge for the first (rightmost) flag
	flagPadY    = 2.0  // minimal gap so the flag tip sits right at the top edge
	flagPoleW   = 2.5  // pole stroke width
	flagSlotGap = 4.0  // horizontal gap between adjacent flags

	vulnFlagColor = "#c2410c" // dark orange — all vulnerability severities use this uniform colour
)

const (
	popupWidth      = 340.0
	popupLineHeight = 26.0
	charsPerLine    = 48
	popupPadding    = 20.0
	popupTextX      = 12.0
	popupTextStartY = 22.0
	popupTextSize   = 14.0
	popupStarSize   = 22.0
	popupStarShift  = 14.0
	dateLineSpacing = 0.9
	textWidthRatio  = 0.45
	textHeightRatio = 1.0
)

// HandDrawn implements a casual, hand-drawn visual style with wobbly
// lines and xkcd Script typography (embedded in the binary).
type HandDrawn struct{ seed uint64 }

// New creates a new HandDrawn style with the given seed forReproducible
// line wobbling.
func New(seed uint64) *HandDrawn { return &HandDrawn{seed: seed} }

func (h *HandDrawn) RenderDefs(buf *bytes.Buffer) {
	buf.WriteString(`  <defs>
    <style>
      @font-face {
        font-family: 'xkcd Script';
        src: url('data:font/woff;base64,`)
	buf.WriteString(fonts.XKCDScriptWOFFBase64())
	buf.WriteString(`') format('woff');
        font-weight: normal;
        font-style: normal;
      }
      .license-flag, .vuln-flag { transition: opacity 0.15s ease; }
      .license-flag.highlight, .vuln-flag.highlight { opacity: 0.7; }
    </style>
    <pattern id="brittleTexture" patternUnits="userSpaceOnUse" width="200" height="200">
      <image href="`)
	buf.WriteString(getBrittleTextureDataURI())
	buf.WriteString(`" x="0" y="0" width="200" height="200" preserveAspectRatio="xMidYMid slice" opacity="0.6"/>
    </pattern>
  </defs>
`)
}

func (h *HandDrawn) RenderBlock(buf *bytes.Buffer, b styles.Block) {
	fill := greyForID(b.ID)

	rot := rotationFor(b.ID, b.W, b.H)
	path := wobbledRect(b.X, b.Y, b.W, b.H, h.seed, b.ID)

	styles.WrapURL(buf, b.URL, func() {
		class := "block"
		if b.Brittle {
			class = "block brittle"
		}
		if b.VulnSeverity != "" {
			class += " vuln vuln-" + b.VulnSeverity
		}
		fmt.Fprintf(buf, `<path id="block-%s" class="%s" d="%s" fill="%s" stroke="#333" stroke-width="2" stroke-linejoin="round" transform="rotate(%.3f %.2f %.2f)"/>`,
			styles.EscapeXML(b.ID), class, path, fill, rot, b.CX, b.CY)
	})
	buf.WriteByte('\n')

	if b.Brittle {
		fmt.Fprintf(buf, `  <path class="block-texture" d="%s" fill="url(#brittleTexture)" style="pointer-events: none;" transform="rotate(%.3f %.2f %.2f)"/>`+"\n",
			path, rot, b.CX, b.CY)
	}
}

func (h *HandDrawn) RenderFlags(buf *bytes.Buffer, b styles.Block) {
	rot := rotationFor(b.ID, b.W, b.H)
	slotIdx := 0
	licenseRisk := security.LicenseRiskFromString(b.LicenseRisk)
	if licenseRisk == security.LicenseRiskCopyleft || licenseRisk == security.LicenseRiskWeakCopyleft {
		licenseTooltip := b.LicenseRisk
		if b.License != "" {
			licenseTooltip = fmt.Sprintf("%s (%s)", b.License, b.LicenseRisk)
		}
		renderFlag(buf, b, "license-flag license-"+b.LicenseRisk, licenseRisk.IconColor(), licenseTooltip, slotIdx, rot, h.seed)
		slotIdx++
	}
	if b.VulnSeverity != "" {
		renderFlag(buf, b, "vuln-flag vuln-flag-"+b.VulnSeverity, vulnFlagColor, "vuln: "+b.VulnSeverity, slotIdx, rot, h.seed)
	}
}

// renderFlag draws a hand-drawn pennant flag anchored at the top of the block.
// slotIdx controls horizontal placement: 0 = rightmost, 1 = next slot to the left, etc.
// This ensures flags are always fully visible at the top edge regardless of block height.
func renderFlag(buf *bytes.Buffer, b styles.Block, cssClass, color, tooltip string, slotIdx int, rot float64, seed uint64) {
	// Each slot shifts the flag one slot-width to the left so multiple flags don't overlap.
	poleX := b.X + b.W - flagPadX - float64(slotIdx)*(flagW+flagSlotGap)
	poleTopY := b.Y + flagPadY
	poleBotY := poleTopY + flagPoleH

	// Pennant triangle: attaches to the pole at top/bottom, tip points leftward into the block.
	p1x, p1y := poleX, poleTopY               // top attachment (at block top edge)
	p2x, p2y := poleX-flagW, poleTopY+flagH/2 // left tip
	p3x, p3y := poleX, poleTopY+flagH         // bottom attachment

	pennantPath := wobblyTriangle(p1x, p1y, p2x, p2y, p3x, p3y, seed)

	fmt.Fprintf(buf, `  <g class="%s" data-block="%s" cursor="pointer" transform="rotate(%.3f %.2f %.2f)">`+"\n",
		cssClass, styles.EscapeXML(b.ID), rot, b.CX, b.CY)
	fmt.Fprintf(buf, `    <title>%s</title>`+"\n", styles.EscapeXML(tooltip))
	fmt.Fprintf(buf, `    <line x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f" stroke="#333" stroke-width="%.1f" stroke-linecap="round"/>`+"\n",
		poleX, poleTopY, poleX, poleBotY, flagPoleW)
	fmt.Fprintf(buf, `    <path d="%s" fill="%s" stroke="#333" stroke-width="2" stroke-linejoin="round"/>`+"\n",
		pennantPath, color)
	buf.WriteString("  </g>\n")
}

func (h *HandDrawn) RenderEdge(buf *bytes.Buffer, e styles.Edge) {
	path := curvedEdge(e.X1, e.Y1, e.X2, e.Y2)
	fmt.Fprintf(buf, `  <path class="edge" d="%s" fill="none" stroke="#333" stroke-width="2.5" stroke-dasharray="8,5" stroke-linecap="round"/>`+"\n", path)
}

func (h *HandDrawn) RenderText(buf *bytes.Buffer, b styles.Block) {
	rotate := styles.ShouldRotate(b)
	size := styles.FontSize(b)
	if rotate {
		size = styles.FontSizeRotated(b)
	}

	bgFill := greyForID(b.ID)
	textFill := "#333"

	label := styles.TruncateLabel(b, rotate)

	textW, textH := float64(len(label))*size*textWidthRatio, size*textHeightRatio
	if rotate {
		textW, textH = textH, textW
	}

	fmt.Fprintf(buf, `  <g class="block-text" data-block="%s">`+"\n", styles.EscapeXML(b.ID))
	styles.WrapURL(buf, b.URL, func() {
		fmt.Fprintf(buf, `    <rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" fill="%s"/>`+"\n",
			b.CX-textW/2, b.CY-textH/2, textW, textH, bgFill)

		if rotate {
			fmt.Fprintf(buf, `    <text x="%.2f" y="%.2f" text-anchor="middle" dominant-baseline="middle" font-family="%s" font-size="%.1f" fill="%s" transform="rotate(-90 %.2f %.2f)">%s</text>`+"\n",
				b.CX, b.CY, fonts.FallbackFontFamily, size, textFill, b.CX, b.CY, styles.EscapeXML(label))
		} else {
			fmt.Fprintf(buf, `    <text x="%.2f" y="%.2f" text-anchor="middle" dominant-baseline="middle" font-family="%s" font-size="%.1f" fill="%s">%s</text>`+"\n",
				b.CX, b.CY, fonts.FallbackFontFamily, size, textFill, styles.EscapeXML(label))
		}
	})
	buf.WriteString("  </g>\n")
}

func (h *HandDrawn) RenderPopup(buf *bytes.Buffer, b styles.Block) {
	p := b.Popup
	if p == nil {
		return
	}

	descLines := wrapText(p.Description, charsPerLine)
	numDescLines := max(1, len(descLines))

	hasStats := p.Stars > 0 || p.LastCommit != "" || p.LastRelease != ""
	hasWarning := p.Archived || p.Brittle
	hasLicense := p.License != ""
	hasVuln := p.VulnSeverity != ""

	statsRows := 0
	if hasStats {
		statsRows = 1
		if p.LastCommit != "" && p.LastRelease != "" && p.LastRelease != "0001-01-01" {
			statsRows = 2
		}
	}

	licenseRows := 0
	if hasLicense {
		licenseRows = 1
	}

	vulnRows := 0
	if hasVuln {
		vulnRows = 1
	}

	height := float64(numDescLines+statsRows+licenseRows+vulnRows)*popupLineHeight + popupPadding
	path := wobbledRect(0, 0, popupWidth, height, h.seed, b.ID+"_popup")

	fmt.Fprintf(buf, `  <g class="popup" data-for="%s" visibility="hidden">`+"\n", styles.EscapeXML(b.ID))
	fmt.Fprintf(buf, `    <path d="%s" fill="white" stroke="#333" stroke-width="1.5" stroke-linejoin="round"/>`+"\n", path)

	textY := popupTextStartY
	for _, line := range descLines {
		fmt.Fprintf(buf, `    <text x="%.1f" y="%.1f" font-family="%s" font-size="%.0f" fill="#444">%s</text>`+"\n",
			popupTextX, textY, fonts.FallbackFontFamily, popupTextSize, styles.EscapeXML(line))
		textY += popupLineHeight
	}

	if hasStats {
		statsStartY := textY
		rightY := statsStartY
		leftCenterX := popupWidth / 4
		dateRightX := popupWidth - popupTextX // right-aligned anchor

		warnPrefix := ""
		if hasWarning {
			warnPrefix = "⚠ "
		}

		if p.LastCommit != "" {
			fmt.Fprintf(buf, `    <text x="%.1f" y="%.1f" text-anchor="end" font-family="%s" font-size="%.0f" fill="#444">%slast commit: %s</text>`+"\n",
				dateRightX, rightY, fonts.FallbackFontFamily, popupTextSize, warnPrefix, p.LastCommit)
			rightY += popupLineHeight * dateLineSpacing
		}
		if p.LastRelease != "" && p.LastRelease != "0001-01-01" {
			fmt.Fprintf(buf, `    <text x="%.1f" y="%.1f" text-anchor="end" font-family="%s" font-size="%.0f" fill="#444">%slast release: %s</text>`+"\n",
				dateRightX, rightY, fonts.FallbackFontFamily, popupTextSize, warnPrefix, p.LastRelease)
		}

		if p.Stars > 0 {
			starsCenterY := statsStartY + (popupLineHeight*float64(statsRows))/2 - popupStarShift
			fmt.Fprintf(buf, `    <text x="%.1f" y="%.1f" text-anchor="middle" dominant-baseline="middle" font-family="%s" font-size="%.0f" fill="#222" font-weight="bold">★ %s</text>`+"\n",
				leftCenterX, starsCenterY, fonts.FallbackFontFamily, popupStarSize, formatNumber(p.Stars))
		}
		textY += popupLineHeight * float64(statsRows)
	}

	// Footer badges row — license and/or vulnerability pills at the bottom
	if hasLicense || hasVuln {
		badgeY := textY - 2 // slight upward nudge to vertically center in the row
		badgeH := 20.0
		badgeR := 4.0
		badgeFontSize := 11.0
		badgeGap := 8.0
		badgePadX := 6.0   // horizontal padding inside pill
		maxBadgeW := 200.0 // max width per badge to keep things compact
		charW := badgeFontSize * 0.52

		// Collect badges to render: each has a label, bg color, text color
		type badge struct {
			label string
			bg    string
			fg    string
		}
		var badges []badge

		if hasLicense {
			risk := security.LicenseRiskFromString(p.LicenseRisk)
			// Use a short license label that fits within the badge
			shortLicense := truncateStr(p.License, 28)
			if risk.IsFlagged() {
				badges = append(badges, badge{
					label: fmt.Sprintf("⚖ %s · %s", shortLicense, p.LicenseRisk),
					bg:    risk.IconColor(),
					fg:    "#fff",
				})
			} else {
				badges = append(badges, badge{
					label: fmt.Sprintf("⚖ %s", shortLicense),
					bg:    "#e5e7eb", // gray-200
					fg:    "#333",
				})
			}
		}
		if hasVuln {
			sev := security.SeverityFromString(p.VulnSeverity)
			badges = append(badges, badge{
				label: fmt.Sprintf("⚠ %s", p.VulnSeverity),
				bg:    sev.Color(),
				fg:    sev.TextColor(),
			})
		}

		// Measure total width to center the row, capping each badge
		totalW := 0.0
		var widths []float64
		for _, bd := range badges {
			w := float64(len(bd.label))*charW + badgePadX*2
			if w > maxBadgeW {
				w = maxBadgeW
			}
			widths = append(widths, w)
			totalW += w
		}
		totalW += badgeGap * float64(len(badges)-1)

		x := (popupWidth - totalW) / 2
		for i, bd := range badges {
			w := widths[i]
			fmt.Fprintf(buf, `    <rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.1f" ry="%.1f" fill="%s"/>`+"\n",
				x, badgeY, w, badgeH, badgeR, badgeR, bd.bg)
			fmt.Fprintf(buf, `    <text x="%.1f" y="%.1f" text-anchor="middle" dominant-baseline="middle" font-family="%s" font-size="%.0f" fill="%s" font-weight="600">%s</text>`+"\n",
				x+w/2, badgeY+badgeH/2, fonts.FallbackFontFamily, badgeFontSize, bd.fg, styles.EscapeXML(bd.label))
			x += w + badgeGap
		}
	}

	buf.WriteString("  </g>\n")
}

func formatNumber(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

func wrapText(s string, maxChars int) []string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= maxChars {
		return []string{s}
	}

	var lines []string
	var line strings.Builder

	for _, word := range strings.Fields(s) {
		if line.Len() == 0 {
			line.WriteString(word)
		} else if line.Len()+1+len(word) <= maxChars {
			line.WriteByte(' ')
			line.WriteString(word)
		} else {
			lines = append(lines, line.String())
			line.Reset()
			line.WriteString(word)
		}
	}
	if line.Len() > 0 {
		lines = append(lines, line.String())
	}
	return lines
}
