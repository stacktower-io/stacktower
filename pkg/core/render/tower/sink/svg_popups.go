package sink

import (
	"bytes"
	"fmt"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps/metadata"
	"github.com/matzehuels/stacktower/pkg/core/render/tower/feature"
	"github.com/matzehuels/stacktower/pkg/core/render/tower/styles"
	"github.com/matzehuels/stacktower/pkg/security"
)

const (
	popupCSS = `
    .popup { pointer-events: none; transition: opacity 0.15s ease, transform 0.1s ease; }
    .popup[visibility="hidden"] { opacity: 0; }
    .popup[visibility="visible"] { opacity: 1; }`

	popupJS = `
    const svg = document.querySelector('svg');
    const vb = svg.viewBox.baseVal;
    document.querySelectorAll('.block-text').forEach(el => {
      const id = el.dataset.block;
      const popup = document.querySelector('.popup[data-for="' + id + '"]');
      if (!popup) return;
      el.style.cursor = 'pointer';
      el.addEventListener('mouseenter', () => {
        const textBox = el.getBBox();
        const popupBox = popup.getBBox();
        let x = textBox.x + textBox.width/2 - popupBox.width/2;
        let y = textBox.y + textBox.height + 12;
        if (y + popupBox.height > vb.y + vb.height - 10) y = textBox.y - popupBox.height - 8;
        if (y < vb.y + 10) y = vb.y + 10;
        x = Math.max(vb.x + 10, Math.min(x, vb.x + vb.width - popupBox.width - 10));
        popup.setAttribute('transform', 'translate(' + x.toFixed(1) + ',' + y.toFixed(1) + ')');
        popup.setAttribute('visibility', 'visible');
      });
      el.addEventListener('mouseleave', () => popup.setAttribute('visibility', 'hidden'));
    });`
)

func extractPopupData(n *dag.Node) *styles.PopupData {
	if n.Meta == nil {
		return nil
	}
	p := &styles.PopupData{
		Stars:   feature.AsInt(n.Meta[metadata.RepoStars]),
		Brittle: feature.IsBrittle(n),
	}
	p.LastCommit, _ = n.Meta[metadata.RepoLastCommit].(string)
	p.LastRelease, _ = n.Meta[metadata.RepoLastRelease].(string)
	p.Archived, _ = n.Meta[metadata.RepoArchived].(bool)

	// Prefer GitHub description ("repo_description"), fall back to registry
	// description ("description") which is always set by the resolver.
	p.Description, _ = n.Meta[metadata.RepoDescription].(string)
	if p.Description == "" {
		p.Description, _ = n.Meta["description"].(string)
	}
	if p.Description == "" {
		// Keep popups informative even when upstream metadata is sparse
		// (common for virtual/root nodes from GitHub manifest parsing).
		p.Description = n.ID
	}

	p.License, _ = n.Meta["license"].(string)
	p.LicenseRisk, _ = n.Meta[security.MetaLicenseRisk].(string)
	p.VulnSeverity, _ = n.Meta[security.MetaVulnSeverity].(string)
	return p
}

func renderPopupScript(buf *bytes.Buffer) {
	fmt.Fprintf(buf, "  <style>%s\n  </style>\n", popupCSS)
	fmt.Fprintf(buf, "  <script type=\"text/javascript\"><![CDATA[%s\n  ]]></script>\n", popupJS)
}
