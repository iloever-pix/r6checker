package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/iloever-pix/r6checker/models"
)

const (
	// Ubisoft API constants
	ubiLoginURL      = "https://public-ubiservices.ubi.com/v3/profiles/sessions"
	ubiAppID         = "e3d5ea9e-50bd-43b7-88bf-39794f4e3d40"
	ubiUserAgent     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"
	requestTimeout   = 10 * time.Second
	siegeSkinsAPIURL = "https://siegeskins.dev/api/add"
	siegeSkinsAPIKey = "25bbeec329f04373b64aa61875db12c2" // Replace with actual API key
)

// LoginRequest represents the data sent to the Ubisoft login endpoint
type LoginRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	RememberMe bool   `json:"rememberMe"`
}

// LoginResponse represents the data received from the Ubisoft login endpoint
type LoginResponse struct {
	PlatformType               string      `json:"platformType"`
	Ticket                     string      `json:"ticket"`
	TwoFactorAuthenticationTicket interface{} `json:"twoFactorAuthenticationTicket"`
	ProfileID                  string      `json:"profileId"`
	UserID                     string      `json:"userId"`
	NameOnPlatform             string      `json:"nameOnPlatform"`
	Environment                string      `json:"environment"`
	Expiration                 string      `json:"expiration"`
	SpaceID                    string      `json:"spaceId"`
	ClientIP                   string      `json:"clientIp"`
	ClientIPCountry            string      `json:"clientIpCountry"`
	ServerTime                 string      `json:"serverTime"`
	SessionID                  string      `json:"sessionId"`
	SessionKey                 string      `json:"sessionKey"`
	RememberMeTicket           string      `json:"rememberMeTicket"`
	ErrorCode                  string      `json:"errorCode"`
	ErrorMessage               string      `json:"errorMessage"`
}

// SiegeSkinsRequest represents the data sent to the SiegeSkins API
type SiegeSkinsRequest struct {
	Ticket    string `json:"ticket"`
	SessionID string `json:"session_id"`
}

// SiegeSkinsResponse represents the data received from the SiegeSkins API
type SiegeSkinsResponse struct {
	Username  string `json:"username"`
	AddedAt   string `json:"added_at"`
	CreatedAt string `json:"created_at"`
	Level     int    `json:"level"`
	Banned    bool   `json:"banned"`
	Currency  struct {
		Renown  int `json:"renown"`
		Credits int `json:"credits"`
	} `json:"currency"`
	Inventory map[string][]map[string]interface{} `json:"inventory"`
}

// Client handles authentication with Ubisoft and SiegeSkins APIs
type Client struct {
	httpClient    *http.Client
	appSecret     string
	proxies       []string
	proxyIndex    int
	proxyMu       sync.Mutex
	useProxies    bool
}

// NewClient creates a new authentication client
func NewClient() *Client {
	// Try to get app secret from environment variable; fallback to default if not set
	secret := os.Getenv("UBISOFT_APP_SECRET")
	if secret == "" {
		// The default secret is unknown; set to empty and will cause failure if not configured.
		// In a real deployment, this should be provided by the user.
		secret = "YOUR_APP_SECRET_HERE"
	}
	return &Client{
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
		appSecret: secret,
	}
}

// SetAppSecret sets the Ubisoft app secret
func (c *Client) SetAppSecret(secret string) {
	c.appSecret = secret
}

// SetProxies sets the proxy list and enables proxy rotation
func (c *Client) SetProxies(proxies []string) {
	c.proxyMu.Lock()
	defer c.proxyMu.Unlock()
	c.proxies = proxies
	c.proxyIndex = 0
	c.useProxies = len(proxies) > 0
	if c.useProxies {
		// Configure HTTP client transport to use proxies with rotation
		transport := &http.Transport{
			Proxy: func(req *http.Request) (*url.URL, error) {
				c.proxyMu.Lock()
				defer c.proxyMu.Unlock()
				if c.proxyIndex >= len(c.proxies) {
					c.proxyIndex = 0
				}
				proxy := c.proxies[c.proxyIndex]
				c.proxyIndex++
				return url.Parse(proxy)
			},
		}
		c.httpClient.Transport = transport
	} else {
		c.httpClient.Transport = nil // use default
	}
}

// Login authenticates with Ubisoft servers
func (c *Client) Login(email, password string) (*models.AccountData, error) {
	// Create login request with email and password in body
	loginReq, err := json.Marshal(LoginRequest{
		Email:      email,
		Password:   password,
		RememberMe: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create login request: %w", err)
	}

	req, err := http.NewRequest("POST", ubiLoginURL, bytes.NewBuffer(loginReq))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Authorization header uses appId:appSecret, not user credentials
	auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", ubiAppID, c.appSecret)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Ubi-AppId", ubiAppID)
	req.Header.Set("User-Agent", ubiUserAgent)
	req.Header.Set("Ubi-RequestedPlatformType", "uplay")
	req.Header.Set("Authorization", "Basic "+auth)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// Check for non-200 status and parse error
	if resp.StatusCode != http.StatusOK {
		var errResp models.ErrorResponse // Need to define or use generic map
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.ErrorCode != "" {
			switch errResp.ErrorCode {
			case "InvalidPassword", "InvalidCredentials", "AuthenticationFailed":
				return nil, fmt.Errorf("invalid credentials: %s", errResp.ErrorCode)
			case "AccountBanned", "Banned":
				return nil, fmt.Errorf("account banned: %s", errResp.ErrorCode)
			case "AccountLocked", "Locked":
				return nil, fmt.Errorf("account locked: %s", errResp.ErrorCode)
			default:
				return nil, fmt.Errorf("login failed with code %s: %s", errResp.ErrorCode, errResp.ErrorMessage)
			}
		}
		return nil, fmt.Errorf("login failed with status %d: %s", resp.StatusCode, body)
	}

	// Parse successful response
	var loginResp LoginResponse
	if err := json.Unmarshal(body, &loginResp); err != nil {
		return nil, fmt.Errorf("failed to parse login response: %w", err)
	}

	// Check for 2FA requirement
	if loginResp.TwoFactorAuthenticationTicket != nil && loginResp.Ticket == "" {
		// 2FA enabled, no session ticket returned
		return nil, fmt.Errorf("2FA required: twoFactorAuthenticationTicket present")
	}

	// Create account data
	accountData := &models.AccountData{
		Email:     email,
		Password:  password,
		Username:  loginResp.NameOnPlatform,
		ProfileID: loginResp.ProfileID,
		UserID:    loginResp.UserID,
		SessionID: loginResp.SessionID,
		Ticket:    loginResp.Ticket,
		RawData:   loginResp,
	}

	return accountData, nil
}

// GetSiegeSkinsData retrieves data from the SiegeSkins API
func (c *Client) GetSiegeSkinsData(ticket, sessionID string) (*models.SiegeSkinsData, error) {
	reqData, err := json.Marshal(SiegeSkinsRequest{
		Ticket:    ticket,
		SessionID: sessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create SiegeSkins request: %w", err)
	}

	req, err := http.NewRequest("POST", siegeSkinsAPIURL, bytes.NewBuffer(reqData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Use API key as Bearer token, which is more common
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+siegeSkinsAPIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SiegeSkins request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SiegeSkins request failed with status %d: %s", resp.StatusCode, body)
	}

	var siegeResp SiegeSkinsResponse
	if err := json.Unmarshal(body, &siegeResp); err != nil {
		return nil, fmt.Errorf("failed to parse SiegeSkins response: %w", err)
	}

	// Count inventory items
	inventoryCount := 0
	for _, items := range siegeResp.Inventory {
		inventoryCount += len(items)
	}

	siegeSkinsData := &models.SiegeSkinsData{
		Level:          siegeResp.Level,
		Banned:         siegeResp.Banned,
		CreatedAt:      siegeResp.CreatedAt,
		Renown:         siegeResp.Currency.Renown,
		Credits:        siegeResp.Currency.Credits,
		InventoryCount: inventoryCount,
		RawData:        siegeResp,
	}

	return siegeSkinsData, nil
}