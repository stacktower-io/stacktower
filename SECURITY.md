# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| Latest  | :white_check_mark: |
| < Latest | :x:               |

We recommend always using the latest release.

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, report them privately via one of these methods:

1. **GitHub Security Advisories** (Preferred)  
   Go to the [Security tab](https://github.com/stacktower-io/stacktower/security/advisories) and click "Report a vulnerability"

2. **Email**  
   Contact the maintainer directly (see GitHub profile)

### What to Include

- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

### Response Timeline

- **Initial response**: Within 48 hours
- **Status update**: Within 7 days
- **Fix timeline**: Depends on severity, typically within 30 days

### Scope

This security policy covers:
- The Stacktower CLI binary
- The Go library (`pkg/`)

Out of scope:
- Third-party dependencies (report to the respective maintainers)
- The documentation website (stacktower.io)

## Security Best Practices

When using Stacktower:

- **API Tokens**: Never commit `GITHUB_TOKEN` to version control
- **Cache Directory**: The cache at `~/.cache/stacktower/` may contain API responses; treat it as potentially sensitive
- **Dependencies**: Run `make vuln` or `govulncheck ./...` to check for known vulnerabilities

