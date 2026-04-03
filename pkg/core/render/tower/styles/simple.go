package styles

import (
	"bytes"
	"fmt"

	"github.com/matzehuels/stacktower/pkg/security"
)

const (
	maxCornerRadius    = 18.0
	cornerRatioDivisor = 3.0
	textWidthRatio     = 0.6
	textHeightRatio    = 1.2
)

type Simple struct{}

func (Simple) RenderDefs(*bytes.Buffer) {}

func (Simple) RenderBlock(buf *bytes.Buffer, b Block) {
	radius := min(maxCornerRadius, b.W/cornerRatioDivisor, b.H/cornerRatioDivisor)
	WrapURL(buf, b.URL, func() {
		class := "block"
		if b.VulnSeverity != "" {
			class += " vuln vuln-" + b.VulnSeverity
		}
		fmt.Fprintf(buf, `<rect id="block-%s" class="%s" x="%.2f" y="%.2f" width="%.2f" height="%.2f" rx="%.1f" ry="%.1f" fill="%s" stroke="#333" stroke-width="1"/>`,
			EscapeXML(b.ID), class, b.X, b.Y, b.W, b.H, radius, radius, "white")
	})
	buf.WriteByte('\n')
}

func (Simple) RenderFlags(buf *bytes.Buffer, b Block) {
	slotIdx := 0
	licenseRisk := security.LicenseRiskFromString(b.LicenseRisk)
	if licenseRisk == security.LicenseRiskCopyleft || licenseRisk == security.LicenseRiskWeakCopyleft {
		licenseTooltip := b.LicenseRisk
		if b.License != "" {
			licenseTooltip = fmt.Sprintf("%s (%s)", b.License, b.LicenseRisk)
		}
		renderSimpleFlag(buf, b, "license-flag license-"+b.LicenseRisk, licenseRisk.IconColor(), licenseTooltip, slotIdx)
		slotIdx++
	}
	if b.VulnSeverity != "" {
		renderSimpleFlag(buf, b, "vuln-flag vuln-flag-"+b.VulnSeverity, simpleVulnFlagColor, "vuln: "+b.VulnSeverity, slotIdx)
	}
}

const (
	simpleFlagPoleH = 20.0
	simpleFlagW     = 14.0
	simpleFlagH     = 10.0
	simpleFlagPadX  = 8.0
	simpleFlagPadY  = 2.0 // near top edge so flags are always fully visible
	simpleFlagPoleW = 2.0
	simpleFlagGap   = 3.0 // horizontal gap between adjacent flags

	simpleVulnFlagColor = "#c2410c" // dark orange — all vulnerability severities
)

// renderSimpleFlag draws a pennant flag anchored at the top of the block.
// slotIdx controls horizontal placement: 0 = rightmost, 1 = next slot left, etc.
func renderSimpleFlag(buf *bytes.Buffer, b Block, cssClass, color, tooltip string, slotIdx int) {
	poleX := b.X + b.W - simpleFlagPadX - float64(slotIdx)*(simpleFlagW+simpleFlagGap)
	poleTopY := b.Y + simpleFlagPadY
	poleBotY := poleTopY + simpleFlagPoleH

	fmt.Fprintf(buf, `  <g class="%s" data-block="%s" cursor="pointer">`+"\n",
		cssClass, EscapeXML(b.ID))
	fmt.Fprintf(buf, `    <title>%s</title>`+"\n", EscapeXML(tooltip))
	// Pole
	fmt.Fprintf(buf, `    <line x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f" stroke="#333" stroke-width="%.1f" stroke-linecap="round"/>`+"\n",
		poleX, poleTopY, poleX, poleBotY, simpleFlagPoleW)
	// Pennant triangle: tip points leftward into the block
	fmt.Fprintf(buf, `    <path d="M%.2f %.2f L%.2f %.2f L%.2f %.2f Z" fill="%s"/>`+"\n",
		poleX, poleTopY,
		poleX-simpleFlagW, poleTopY+simpleFlagH/2,
		poleX, poleTopY+simpleFlagH,
		color)
	buf.WriteString("  </g>\n")
}

func (Simple) RenderEdge(buf *bytes.Buffer, e Edge) {
	fmt.Fprintf(buf, `  <line x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f" stroke="#333" stroke-width="1.5" stroke-dasharray="6,4"/>`+"\n",
		e.X1, e.Y1, e.X2, e.Y2)
}

func (Simple) RenderText(buf *bytes.Buffer, b Block) {
	rotate := ShouldRotate(b)
	size := FontSize(b)
	if rotate {
		size = FontSizeRotated(b)
	}
	label := TruncateLabel(b, rotate)

	textW, textH := float64(len(label))*size*textWidthRatio, size*textHeightRatio
	if rotate {
		textW, textH = textH, textW
	}

	fmt.Fprintf(buf, `  <g class="block-text" data-block="%s">`+"\n", EscapeXML(b.ID))
	WrapURL(buf, b.URL, func() {
		fmt.Fprintf(buf, `    <rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" fill="%s"/>`+"\n",
			b.CX-textW/2, b.CY-textH/2, textW, textH, "white")

		if rotate {
			fmt.Fprintf(buf, `    <text x="%.2f" y="%.2f" text-anchor="middle" dominant-baseline="middle" font-family="Times,serif" font-size="%.1f" fill="%s" transform="rotate(-90 %.2f %.2f)">%s</text>`+"\n",
				b.CX, b.CY, size, "#333", b.CX, b.CY, EscapeXML(label))
		} else {
			fmt.Fprintf(buf, `    <text x="%.2f" y="%.2f" text-anchor="middle" dominant-baseline="middle" font-family="Times,serif" font-size="%.1f" fill="%s">%s</text>`+"\n",
				b.CX, b.CY, size, "#333", EscapeXML(label))
		}
	})
	buf.WriteString("  </g>\n")
}

func (Simple) RenderPopup(*bytes.Buffer, Block) {}
