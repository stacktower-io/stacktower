package goproxy

import (
	"net"
	"testing"
)

func TestAllowedVanityHost(t *testing.T) {
	allowed := []string{
		"gopkg.in",
		"golang.org",
		"k8s.io",
		"go.uber.org",
		"my-vanity.example.com",
	}
	for _, host := range allowed {
		if !allowedVanityHost(host) {
			t.Errorf("allowedVanityHost(%q) = false, want true", host)
		}
	}

	rejected := []string{
		"",
		"localhost",
		"foo.localhost",
		"printer.local",
		"db.internal",
		"intranet",         // single label
		"169.254.169.254",  // cloud metadata IP
		"10.0.0.5",         // RFC1918 IP
		"127.0.0.1",        // loopback
		"[::1]",            // IPv6 literal
		"example.com:8080", // port smuggling
		"user@example.com", // userinfo smuggling
		"example.com/evil", // path is not part of the host
		"exa mple.com",     // whitespace
		"example..com",     // empty label
		"-bad.example.com", // label starting with hyphen
		"example.com?x=1",  // query smuggling
		"metadata.google.internal",
	}
	for _, host := range rejected {
		if allowedVanityHost(host) {
			t.Errorf("allowedVanityHost(%q) = true, want false", host)
		}
	}
}

func TestSplitVanityModule(t *testing.T) {
	host, rest, ok := splitVanityModule("gopkg.in/yaml.v3")
	if !ok || host != "gopkg.in" || rest != "yaml.v3" {
		t.Errorf("splitVanityModule = (%q, %q, %v)", host, rest, ok)
	}

	host, rest, ok = splitVanityModule("example.com")
	if !ok || host != "example.com" || rest != "" {
		t.Errorf("splitVanityModule = (%q, %q, %v)", host, rest, ok)
	}

	if _, _, ok := splitVanityModule(""); ok {
		t.Error("splitVanityModule(\"\") should not be ok")
	}
	if _, _, ok := splitVanityModule("/leading/slash"); ok {
		t.Error("splitVanityModule with empty host should not be ok")
	}
}

func TestIsPublicIP(t *testing.T) {
	private := []string{
		"127.0.0.1", "::1",
		"10.0.0.5", "172.16.1.1", "192.168.1.1",
		"169.254.169.254", // link-local / cloud metadata
		"fe80::1",         // IPv6 link-local
		"fd00::1",         // ULA
		"0.0.0.0", "::",
	}
	for _, s := range private {
		if isPublicIP(net.ParseIP(s)) {
			t.Errorf("isPublicIP(%s) = true, want false", s)
		}
	}

	public := []string{"8.8.8.8", "140.82.112.3", "2606:4700::6810:84e5"}
	for _, s := range public {
		if !isPublicIP(net.ParseIP(s)) {
			t.Errorf("isPublicIP(%s) = false, want true", s)
		}
	}
}
