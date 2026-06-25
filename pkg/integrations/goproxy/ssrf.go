package goproxy

import (
	"context"
	"net"
	"regexp"
	"strings"
)

// Vanity-import discovery fetches https://<module-path>?go-get=1, where the
// module path comes from untrusted go.mod files. Without validation a hostile
// module path like "169.254.169.254/latest/meta-data" or "internal-service/x"
// would make the client issue requests to internal infrastructure (SSRF).
// The helpers in this file restrict discovery to public DNS hostnames that
// resolve to public IP addresses.

// publicHostnamePattern matches multi-label DNS names: at least two labels of
// letters, digits, and hyphens. Single-label names (internal hostnames), IP
// literals, ports, and userinfo are all rejected by this shape.
var publicHostnamePattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`)

// splitVanityModule splits a module path into its host (first path segment)
// and the remaining path. Returns ok=false for empty or hostless paths.
func splitVanityModule(mod string) (host, rest string, ok bool) {
	mod = strings.TrimSpace(mod)
	if mod == "" {
		return "", "", false
	}
	host, rest, _ = strings.Cut(mod, "/")
	if host == "" {
		return "", "", false
	}
	return host, rest, true
}

// allowedVanityHost reports whether host is a plausible public website host:
// a multi-label DNS name that is not an IP literal and not a known-local name.
func allowedVanityHost(host string) bool {
	host = strings.ToLower(host)

	if !publicHostnamePattern.MatchString(host) {
		return false
	}
	// The pattern already excludes bracketed IPv6 and ports, but a dotted
	// IPv4 literal matches the DNS shape, so reject explicitly.
	if net.ParseIP(host) != nil {
		return false
	}
	switch {
	case host == "localhost",
		strings.HasSuffix(host, ".localhost"),
		strings.HasSuffix(host, ".local"),
		strings.HasSuffix(host, ".internal"):
		return false
	}
	return true
}

// resolvesToPublicIP resolves host and reports whether ALL its addresses are
// public. Resolution failure or any private/loopback/link-local/ULA address
// rejects the host — a hostile DNS record must not be able to point vanity
// discovery at internal infrastructure.
func resolvesToPublicIP(ctx context.Context, host string) bool {
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if !isPublicIP(ip) {
			return false
		}
	}
	return true
}

func isPublicIP(ip net.IP) bool {
	return !ip.IsLoopback() &&
		!ip.IsPrivate() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() &&
		!ip.IsMulticast() &&
		!ip.IsUnspecified() &&
		!ip.IsInterfaceLocalMulticast()
}
