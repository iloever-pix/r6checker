package models

import "time"

// AccountCredentials represents the login credentials for an account
type AccountCredentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// AccountData represents a valid R6 account with authentication data
type AccountData struct {
	Email     string      `json:"email"`
	Password  string      `json:"password"`
	Username  string      `json:"username"`
	ProfileID string      `json:"profileId"`
	UserID    string      `json:"userId"`
	SessionID string      `json:"sessionId"`
	Ticket    string      `json:"ticket"`
	RawData   interface{} `json:"rawData,omitempty"`
	
	// Additional data
	SiegeSkinsData *SiegeSkinsData `json:"siegeSkinsData,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
}

// SiegeSkinsData represents data retrieved from the SiegeSkins API
type SiegeSkinsData struct {
	Level         int         `json:"level"`
	Banned        bool        `json:"banned"`
	CreatedAt     string      `json:"createdAt"`
	Renown        int         `json:"renown"`
	Credits       int         `json:"credits"`
	InventoryCount int        `json:"inventoryCount"`
	RawData       interface{} `json:"rawData,omitempty"`
}

// AccountLinks represents links to various R6 stats websites
type AccountLinks struct {
	SiegeSkins string `json:"siegeSkins"`
	R6Tabs     string `json:"r6Tabs"`
	R6Tracker  string `json:"r6Tracker"`
}

// GetLinks returns the links for an account
func (a *AccountData) GetLinks() AccountLinks {
	id := a.ProfileID
	if id == "" {
		id = a.UserID
	}
	
	return AccountLinks{
		SiegeSkins: "https://siegeskins.com/profile/" + id,
		R6Tabs:     "https://tabstats.com/siege/player/" + id,
		R6Tracker:  "https://r6.tracker.network/profile/id/" + id,
	}
}

// CheckerStats represents statistics about the account checking process
type CheckerStats struct {
	Processed   int       `json:"processed"`
	Valid       int       `json:"valid"`
	Invalid     int       `json:"invalid"`
	StartTime   time.Time `json:"startTime"`
	EndTime     time.Time `json:"endTime,omitempty"`
	ElapsedTime float64   `json:"elapsedTime,omitempty"` // In seconds
	Speed       float64   `json:"speed,omitempty"`       // Accounts per second
}

// ProgressUpdate represents an update to the checking progress
type ProgressUpdate struct {
	Processed int     `json:"processed"`
	Total     int     `json:"total"`
	Valid     int     `json:"valid"`
	Invalid   int     `json:"invalid"`
	Percent   int     `json:"percent"`
	Speed     float64 `json:"speed"`
	ETA       float64 `json:"eta"` // In seconds
}

// CheckResult represents the result of checking an account
type CheckResult struct {
	Account *AccountData `json:"account,omitempty"`
	Valid   bool         `json:"valid"`
	Error   string       `json:"error,omitempty"`
}