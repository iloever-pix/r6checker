package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/yourusername/r6checker/models"
)

const (
	// Ubisoft API constants
	ubiLoginURL         = "https://public-ubiservices.ubi.com/v3/profiles/sessions"
	ubiAppID            = "e3d5ea9e-50bd-43b7-88bf-39794f4e3d40"
	ubiUserAgent        = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"
	requestTimeout      = 10 * time.Second
	siegeSkinsAPIURL    = "https://siegeskins.com/api/add"
	siegeSkinsAPIKey    = "25bbeec329f04373b64aa61875db12c2" // Replace with your actual API key
)

// LoginRequest represents the data sent to the Ubisoft login endpoint
type LoginRequest struct {
	RememberMe bool `json:"rememberMe"`
}

// LoginResponse represents the data received from the Ubisoft login endpoint
type LoginResponse struct {
	PlatformType               string `json:"platformType"`
	Ticket                     string `json:"ticket"`
	TwoFactorAuthenticationTicket interface{} `json:"twoFactorAuthenticationTicket"`
	ProfileID                  string `json:"profileId"`
	UserID                     string `json:"userId"`
	NameOnPlatform            string `json:"nameOnPlatform"`
	Environment               string `json:"environment"`
	Expiration                string `json:"expiration"`
	SpaceID                   string `json:"spaceId"`
	ClientIP                  string `json:"clientIp"`
	ClientIPCountry           string `json:"clientIpCountry"`
	ServerTime                string `json:"serverTime"`
	SessionID                 string `json:"sessionId"`
	SessionKey                string `json:"sessionKey"`
	RememberMeTicket          string `json:"rememberMeTicket"`
}

// SiegeSkinsRequest represents the data sent to the SiegeSkins API
type SiegeSkinsRequest struct {
	Ticket    string `json:"ticket"`
	SessionID string `json:"session_id"`
}

// SiegeSkinsResponse represents the data received from the SiegeSkins API
type SiegeSkinsResponse struct {
	Username string `json:"username"`
	AddedAt  string `json:"added_at"`
	CreatedAt string `json:"created_at"`
	Level    int    `json:"level"`
	Banned   bool   `json:"banned"`
	Currency struct {
		Renown  int `json:"renown"`
		Credits int `json:"credits"`
	} `json:"currency"`
	Inventory map[string][]map[string]interface{} `json:"inventory"`
}

// Client handles authentication with Ubisoft and SiegeSkins APIs
type Client struct {
	httpClient *http.Client
}

// NewClient creates a new authentication client
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
	}
}

// Login authenticates with Ubisoft servers
func (c *Client) Login(email, password string) (*models.AccountData, error) {
	// Create login request
	loginReq, err := json.Marshal(LoginRequest{RememberMe: true})
	if err != nil {
		return nil, fmt.Errorf("failed to create login request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", ubiLoginURL, bytes.NewBuffer(loginReq))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Add headers
	auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", email, password)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Ubi-AppId", ubiAppID)
	req.Header.Set("User-Agent", ubiUserAgent)
	req.Header.Set("Ubi-RequestedPlatformType", "uplay")
	req.Header.Set("Authorization", "Basic "+auth)

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("login failed with status %d: %s", resp.StatusCode, body)
	}

	// Parse response
	var loginResp LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return nil, fmt.Errorf("failed to parse login response: %w", err)
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
	// Create request
	reqData, err := json.Marshal(SiegeSkinsRequest{
		Ticket:    ticket,
		SessionID: sessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create SiegeSkins request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", siegeSkinsAPIURL, bytes.NewBuffer(reqData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Add headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", siegeSkinsAPIKey)

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SiegeSkins request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("SiegeSkins request failed with status %d: %s", resp.StatusCode, body)
	}

	// Parse response
	var siegeResp SiegeSkinsResponse
	if err := json.NewDecoder(resp.Body).Decode(&siegeResp); err != nil {
		return nil, fmt.Errorf("failed to parse SiegeSkins response: %w", err)
	}

	// Count inventory items
	inventoryCount := 0
	for _, items := range siegeResp.Inventory {
		inventoryCount += len(items)
	}

	// Create SiegeSkins data
	siegeSkinsData := &models.SiegeSkinsData{
		Level:         siegeResp.Level,
		Banned:        siegeResp.Banned,
		CreatedAt:     siegeResp.CreatedAt,
		Renown:        siegeResp.Currency.Renown,
		Credits:       siegeResp.Currency.Credits,
		InventoryCount: inventoryCount,
		RawData:       siegeResp,
	}

	return siegeSkinsData, nil
}