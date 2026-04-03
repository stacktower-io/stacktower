package github

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AppConfig holds GitHub App configuration.
type AppConfig struct {
	AppID           int64
	PrivateKey      *rsa.PrivateKey
	ClientID        string
	ClientSecret    string
	RedirectURI     string
	PrivateKeyBytes []byte // Raw PEM bytes for device flow
}

// AppClient handles GitHub App authentication and API calls.
type AppClient struct {
	config     AppConfig
	httpClient *http.Client
}

// NewAppClient creates a new GitHub App client.
func NewAppClient(config AppConfig) *AppClient {
	return &AppClient{
		config:     config,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// ParsePrivateKey parses a PEM-encoded RSA private key.
// The key can be base64 encoded (for env vars) or raw PEM.
func ParsePrivateKey(keyData string) (*rsa.PrivateKey, error) {
	// Try base64 decode first (for env var storage)
	decoded, err := base64.StdEncoding.DecodeString(keyData)
	if err != nil {
		// Not base64, use raw PEM
		decoded = []byte(keyData)
	}

	block, _ := pem.Decode(decoded)
	if block == nil {
		return nil, fmt.Errorf("failed to parse PEM block")
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS8 format
		keyInterface, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		var ok bool
		key, ok = keyInterface.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key is not RSA")
		}
	}

	return key, nil
}

// generateJWT creates a JWT for authenticating as the GitHub App.
// JWTs are valid for 10 minutes maximum.
func (c *AppClient) generateJWT() (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"iat": now.Unix() - 60, // Issued 60 seconds ago to account for clock drift
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": c.config.AppID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(c.config.PrivateKey)
}

// AuthorizationURL generates the OAuth authorization URL for the GitHub App.
// The allowPrivate parameter controls whether to request access to private repos.
func (c *AppClient) AuthorizationURL(state string, allowPrivate bool) string {
	params := url.Values{
		"client_id":    {c.config.ClientID},
		"redirect_uri": {c.config.RedirectURI},
		"state":        {state},
	}

	// GitHub Apps don't use scopes the same way OAuth Apps do.
	// The permissions are defined in the App settings.
	// However, we still use the authorize endpoint for user authentication.
	return "https://github.com/login/oauth/authorize?" + params.Encode()
}

// UserAccessToken represents an OAuth token for user authentication.
type UserAccessToken struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
}

// ExchangeCode exchanges an authorization code for a user access token.
func (c *AppClient) ExchangeCode(ctx context.Context, code string) (*UserAccessToken, error) {
	data := url.Values{
		"client_id":     {c.config.ClientID},
		"client_secret": {c.config.ClientSecret},
		"code":          {code},
		"redirect_uri":  {c.config.RedirectURI},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://github.com/login/oauth/access_token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if result.Error != "" {
		return nil, fmt.Errorf("%s: %s", result.Error, result.ErrorDesc)
	}

	return &UserAccessToken{
		AccessToken:  result.AccessToken,
		TokenType:    result.TokenType,
		Scope:        result.Scope,
		RefreshToken: result.RefreshToken,
		ExpiresIn:    result.ExpiresIn,
	}, nil
}

// RefreshAccessToken exchanges a refresh token for a new access token.
// GitHub App user access tokens expire after 8 hours; refresh tokens expire after 6 months.
// Returns a new UserAccessToken with fresh access_token and potentially new refresh_token.
func (c *AppClient) RefreshAccessToken(ctx context.Context, refreshToken string) (*UserAccessToken, error) {
	data := url.Values{
		"client_id":     {c.config.ClientID},
		"client_secret": {c.config.ClientSecret},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://github.com/login/oauth/access_token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if result.Error != "" {
		return nil, fmt.Errorf("%s: %s", result.Error, result.ErrorDesc)
	}

	return &UserAccessToken{
		AccessToken:  result.AccessToken,
		TokenType:    result.TokenType,
		Scope:        result.Scope,
		RefreshToken: result.RefreshToken,
		ExpiresIn:    result.ExpiresIn,
	}, nil
}

// Installation represents a GitHub App installation.
type Installation struct {
	ID      int64 `json:"id"`
	Account struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
		Type  string `json:"type"` // "User" or "Organization"
	} `json:"account"`
	RepositorySelection string `json:"repository_selection"` // "all" or "selected"
	Permissions         struct {
		Contents string `json:"contents"` // "read" or "write"
		Metadata string `json:"metadata"` // "read"
	} `json:"permissions"`
}

// GetUserInstallations lists all installations accessible to the authenticated user.
// Uses the user's OAuth token to find installations they can access.
func (c *AppClient) GetUserInstallations(ctx context.Context, userToken string) ([]Installation, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user/installations", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+userToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error: HTTP %d", resp.StatusCode)
	}

	var result struct {
		TotalCount    int            `json:"total_count"`
		Installations []Installation `json:"installations"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return result.Installations, nil
}

// GetUserInstallation finds the installation for the authenticated user.
// Returns the first installation accessible to the user, or an error if none found.
func (c *AppClient) GetUserInstallation(ctx context.Context, userToken string) (*Installation, error) {
	installations, err := c.GetUserInstallations(ctx, userToken)
	if err != nil {
		return nil, err
	}

	if len(installations) == 0 {
		return nil, fmt.Errorf("no GitHub App installation found for user")
	}

	// Return the first installation (users typically have one personal installation)
	return &installations[0], nil
}

// InstallationToken represents an installation access token.
type InstallationToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// GetInstallationToken creates an installation access token for repo access.
// The token is short-lived (~1 hour) and should be cached and refreshed as needed.
func (c *AppClient) GetInstallationToken(ctx context.Context, installationID int64) (*InstallationToken, error) {
	jwtToken, err := c.generateJWT()
	if err != nil {
		return nil, fmt.Errorf("generate JWT: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/app/installations/%d/access_tokens", installationID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("GitHub API error: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &InstallationToken{
		Token:     result.Token,
		ExpiresAt: result.ExpiresAt,
	}, nil
}

// RevokeInstallationToken revokes an installation access token.
func (c *AppClient) RevokeInstallationToken(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", "https://api.github.com/installation/token", nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	// 204 No Content = success
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("revoke token failed: HTTP %d", resp.StatusCode)
	}

	return nil
}

// FetchUser retrieves the authenticated user's info using a user access token.
func (c *AppClient) FetchUser(ctx context.Context, userToken string) (*User, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+userToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error: HTTP %d", resp.StatusCode)
	}

	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Fetch primary email if not public
	if user.Email == "" {
		if email, err := c.fetchPrimaryEmail(ctx, userToken); err == nil && email != "" {
			user.Email = email
		}
	}

	return &user, nil
}

// fetchPrimaryEmail retrieves the user's primary verified email address.
func (c *AppClient) fetchPrimaryEmail(ctx context.Context, userToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user/emails", nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+userToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil // Non-critical, return empty
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", nil
	}

	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}

	return "", nil
}

// AppDeviceCodeResponse contains the response from requesting a device code.
type AppDeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// RequestDeviceCode initiates the device authorization flow for the GitHub App.
func (c *AppClient) RequestDeviceCode(ctx context.Context) (*AppDeviceCodeResponse, error) {
	data := url.Values{
		"client_id": {c.config.ClientID},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://github.com/login/device/code", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	var result AppDeviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result, nil
}

// PollForToken polls GitHub for the access token after user authorization.
func (c *AppClient) PollForToken(ctx context.Context, deviceCode string, interval int) (*UserAccessToken, error) {
	if interval < 5 {
		interval = 5
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			token, err := c.checkDeviceToken(ctx, deviceCode)
			if err != nil {
				if strings.Contains(err.Error(), "authorization_pending") {
					continue
				}
				if strings.Contains(err.Error(), "slow_down") {
					ticker.Reset(time.Duration(interval+5) * time.Second)
					continue
				}
				return nil, err
			}
			return token, nil
		}
	}
}

// checkDeviceToken attempts to exchange the device code for a token.
func (c *AppClient) checkDeviceToken(ctx context.Context, deviceCode string) (*UserAccessToken, error) {
	data := url.Values{
		"client_id":   {c.config.ClientID},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://github.com/login/oauth/access_token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if result.Error != "" {
		return nil, fmt.Errorf("%s: %s", result.Error, result.ErrorDesc)
	}

	return &UserAccessToken{
		AccessToken:  result.AccessToken,
		TokenType:    result.TokenType,
		Scope:        result.Scope,
		RefreshToken: result.RefreshToken,
		ExpiresIn:    result.ExpiresIn,
	}, nil
}

// GetAppInstallationCount returns the total number of GitHub App installations.
// Uses the App's JWT to authenticate and fetch from GET /app/installations.
func (c *AppClient) GetAppInstallationCount(ctx context.Context) (int64, error) {
	jwtToken, err := c.generateJWT()
	if err != nil {
		return 0, fmt.Errorf("generate JWT: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/app/installations?per_page=1", nil)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("GitHub API error: HTTP %d", resp.StatusCode)
	}

	var result []Installation
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}

	// The total count is in the Link header or we need to parse pagination
	// Simpler: check the Link header for total, or count from response
	// GitHub doesn't return total_count for /app/installations, so we need to paginate
	// For efficiency, let's just count all installations with minimal data
	return c.countAllInstallations(ctx, jwtToken)
}

// countAllInstallations iterates through all pages to count installations.
func (c *AppClient) countAllInstallations(ctx context.Context, jwtToken string) (int64, error) {
	var total int64
	page := 1

	for {
		url := fmt.Sprintf("https://api.github.com/app/installations?per_page=100&page=%d", page)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return total, err
		}
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return total, err
		}

		var installs []Installation
		if err := json.NewDecoder(resp.Body).Decode(&installs); err != nil {
			resp.Body.Close()
			return total, err
		}
		resp.Body.Close()

		total += int64(len(installs))

		// If we got fewer than 100, we've reached the last page
		if len(installs) < 100 {
			break
		}
		page++
	}

	return total, nil
}

// RevokeUserToken revokes a user access token, removing the app's authorization.
// This removes the app from the user's "Authorized GitHub Apps" list.
func (c *AppClient) RevokeUserToken(ctx context.Context, accessToken string) error {
	url := fmt.Sprintf("https://api.github.com/applications/%s/grant", c.config.ClientID)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, strings.NewReader(`{"access_token":"`+accessToken+`"}`))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.SetBasicAuth(c.config.ClientID, c.config.ClientSecret)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	// 204 No Content = success
	// 404 Not Found = token already revoked or invalid (treat as success)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("GitHub API error: HTTP %d", resp.StatusCode)
	}

	return nil
}
