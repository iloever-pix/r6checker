package ui

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/iloever-pix/r6checker/checker"
	"github.com/iloever-pix/r6checker/models"
)

// Note: models.AccountData must have a "Valid bool" field for this UI to work.
// The checker package must implement the methods used below:
//   SetAccountsFile, SetProxiesFile, EnableProxies, SetConcurrency,
//   OnStart, OnProgress, OnResult, OnComplete, Start, Stop.

// Colors
var (
	colorRed    = &color{R: 0.9, G: 0.1, B: 0.1, A: 1.0}
	colorGreen  = &color{R: 0.1, G: 0.8, B: 0.2, A: 1.0}
	colorBlue   = &color{R: 0.1, G: 0.4, B: 0.9, A: 1.0}
	colorYellow = &color{R: 0.9, G: 0.8, B: 0.1, A: 1.0}
)

type color struct {
	R, G, B, A float64
}

func (c *color) RGBA() (r, g, b, a uint32) {
	return uint32(c.R * 65535), uint32(c.G * 65535), uint32(c.B * 65535), uint32(c.A * 65535)
}

// App represents the UI application
type App struct {
	checker           *checker.Checker
	fyneApp           fyne.App
	mainWindow        fyne.Window
	setupContainer    *fyne.Container
	progressContainer *fyne.Container
	resultsContainer  *fyne.Container

	// Setup panel
	accountsEntry     *widget.Entry
	accountsFileLabel *widget.Label
	proxiesEntry      *widget.Entry
	proxiesFileLabel  *widget.Label
	concurrencySlider *widget.Slider
	concurrencyLabel  *widget.Label
	useProxiesCheck   *widget.Check

	// Progress panel
	progressBar    *widget.ProgressBar
	progressLabel  *widget.Label
	statsProcessed *widget.Label
	statsValid     *widget.Label
	statsInvalid   *widget.Label
	statsSpeed     *widget.Label
	statsEta       *widget.Label

	// Results panel
	resultsList  *widget.List
	accountsData []*models.AccountData
	accountsMu   sync.Mutex // protects accountsData

	// Status
	isChecking    bool
	totalAccounts int // stored total for progress
}

// NewApp creates a new UI application
func NewApp(checker *checker.Checker) *App {
	a := &App{
		checker:      checker,
		fyneApp:      app.New(),
		accountsData: make([]*models.AccountData, 0),
	}

	a.mainWindow = a.fyneApp.NewWindow("R6 Account Checker")
	a.mainWindow.Resize(fyne.NewSize(900, 700))

	// Create UI components
	a.createSetupPanel()
	a.createProgressPanel()
	a.createResultsPanel()

	// Create main content
	content := container.NewMax(
		a.setupContainer,
		a.progressContainer,
		a.resultsContainer,
	)

	// Initially show setup panel
	a.showSetupPanel()

	a.mainWindow.SetContent(content)
	return a
}

// Run starts the UI application
func (a *App) Run() error {
	a.mainWindow.ShowAndRun()
	return nil
}

// createSetupPanel creates the setup panel
func (a *App) createSetupPanel() {
	// Accounts file
	a.accountsEntry = widget.NewEntry()
	a.accountsEntry.Disable()
	a.accountsFileLabel = widget.NewLabel("No file selected")
	a.accountsFileLabel.TextStyle = fyne.TextStyle{Italic: true}

	accountsFileBtn := widget.NewButtonWithIcon("Browse", theme.FolderOpenIcon(), func() {
		dialog.ShowFileOpen(func(uri fyne.URIReadCloser, err error) {
			if err != nil || uri == nil {
				return
			}
			path := uri.URI().Path()
			a.accountsEntry.SetText(path)
			a.accountsFileLabel.SetText(filepath.Base(path))
			uri.Close()
		}, a.mainWindow)
	})

	accountsContainer := container.NewBorder(
		widget.NewLabel("Accounts File:"),
		nil,
		nil,
		accountsFileBtn,
		container.NewVBox(
			a.accountsEntry,
			a.accountsFileLabel,
		),
	)

	// Proxies file
	a.proxiesEntry = widget.NewEntry()
	a.proxiesEntry.Disable()
	a.proxiesFileLabel = widget.NewLabel("No file selected")
	a.proxiesFileLabel.TextStyle = fyne.TextStyle{Italic: true}

	proxiesFileBtn := widget.NewButtonWithIcon("Browse", theme.FolderOpenIcon(), func() {
		dialog.ShowFileOpen(func(uri fyne.URIReadCloser, err error) {
			if err != nil || uri == nil {
				return
			}
			path := uri.URI().Path()
			a.proxiesEntry.SetText(path)
			a.proxiesFileLabel.SetText(filepath.Base(path))
			uri.Close()
		}, a.mainWindow)
	})

	proxiesContainer := container.NewBorder(
		widget.NewLabel("Proxies File:"),
		nil,
		nil,
		proxiesFileBtn,
		container.NewVBox(
			a.proxiesEntry,
			a.proxiesFileLabel,
		),
	)

	// Concurrency slider
	a.concurrencySlider = widget.NewSlider(1, 100)
	a.concurrencySlider.SetValue(10)
	a.concurrencySlider.OnChanged = func(v float64) {
		a.concurrencyLabel.SetText(fmt.Sprintf("Concurrency: %d", int(v)))
	}
	a.concurrencyLabel = widget.NewLabel("Concurrency: 10")

	concurrencyContainer := container.NewVBox(
		widget.NewLabel("Threads:"),
		a.concurrencySlider,
		a.concurrencyLabel,
	)

	// Use proxies checkbox
	a.useProxiesCheck = widget.NewCheck("Use Proxies", nil)
	a.useProxiesCheck.OnChanged = func(checked bool) {
		if checked {
			proxiesFileBtn.Enable()
		} else {
			proxiesFileBtn.Disable()
		}
	}

	// Start button
	startBtn := widget.NewButtonWithIcon("Start Checking", theme.MediaPlayIcon(), func() {
		if a.accountsEntry.Text == "" {
			dialog.ShowError(fmt.Errorf("please select an accounts file"), a.mainWindow)
			return
		}
		if a.useProxiesCheck.Checked && a.proxiesEntry.Text == "" {
			dialog.ShowError(fmt.Errorf("please select a proxies file or disable proxy usage"), a.mainWindow)
			return
		}

		// Configure checker
		a.checker.SetAccountsFile(a.accountsEntry.Text)
		if a.useProxiesCheck.Checked {
			a.checker.SetProxiesFile(a.proxiesEntry.Text)
			a.checker.EnableProxies(true)
		} else {
			a.checker.EnableProxies(false)
		}
		a.checker.SetConcurrency(int(a.concurrencySlider.Value))

		// Register callbacks
		a.checker.OnStart(a.handleCheckStart)
		a.checker.OnProgress(a.handleCheckProgress)
		a.checker.OnResult(a.handleCheckResult)
		a.checker.OnComplete(a.handleCheckComplete)

		// Start checking
		go a.checker.Start()
	})

	// Help button
	helpBtn := widget.NewButtonWithIcon("Help", theme.HelpIcon(), func() {
		helpContent := widget.NewRichTextFromMarkdown(`
# R6 Account Checker Help

## Accounts File Format
Each line should contain an account in the format: ` + "`email:password`" + `

## Proxies File Format (Optional)
Each line should contain a proxy in the format: ` + "`ip:port`" + ` or ` + "`ip:port:username:password`" + `

## Settings
- **Concurrency**: Number of simultaneous checks
- **Use Proxies**: Enable proxy rotation during checks
		`)

		helpDialog := dialog.NewCustom("Help", "Close", helpContent, a.mainWindow)
		helpDialog.Resize(fyne.NewSize(500, 400))
		helpDialog.Show()
	})

	// Create layout
	a.setupContainer = container.NewVBox(
		container.NewCenter(
			canvas.NewText("R6 Account Checker", colorBlue),
		),
		container.NewVBox(
			accountsContainer,
			widget.NewSeparator(),
			proxiesContainer,
			widget.NewSeparator(),
			concurrencyContainer,
			a.useProxiesCheck,
		),
		layout.NewSpacer(),
		container.NewHBox(
			layout.NewSpacer(),
			helpBtn,
			startBtn,
		),
	)
}

// createProgressPanel creates the progress panel
func (a *App) createProgressPanel() {
	// Progress bar
	a.progressBar = widget.NewProgressBar()
	a.progressLabel = widget.NewLabel("Initializing...")

	// Stats
	a.statsProcessed = widget.NewLabel("Processed: 0")
	a.statsValid = widget.NewLabel("Valid: 0")
	a.statsInvalid = widget.NewLabel("Invalid: 0")
	a.statsSpeed = widget.NewLabel("Speed: 0/s")
	a.statsEta = widget.NewLabel("ETA: --:--")

	statsContainer := container.NewVBox(
		widget.NewLabel("Statistics:"),
		a.statsProcessed,
		a.statsValid,
		a.statsInvalid,
		a.statsSpeed,
		a.statsEta,
	)

	// Stop button
	stopBtn := widget.NewButtonWithIcon("Stop", theme.MediaStopIcon(), func() {
		if a.isChecking {
			a.checker.Stop()
			a.isChecking = false
			a.showResultsPanel()
		}
	})

	// Create layout
	a.progressContainer = container.NewVBox(
		container.NewCenter(
			canvas.NewText("Checking Accounts", colorBlue),
		),
		container.NewVBox(
			a.progressBar,
			a.progressLabel,
		),
		statsContainer,
		layout.NewSpacer(),
		container.NewHBox(
			layout.NewSpacer(),
			stopBtn,
		),
	)
}

// createResultsPanel creates the results panel
func (a *App) createResultsPanel() {
	// Results list
	a.resultsList = widget.NewList(
		func() int {
			a.accountsMu.Lock()
			defer a.accountsMu.Unlock()
			return len(a.accountsData)
		},
		func() fyne.CanvasObject {
			return container.NewHBox(
				canvas.NewCircle(colorGreen),
				widget.NewLabel("Email:Password"),
				widget.NewLabel("Status"),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			hbox := obj.(*fyne.Container)
			a.accountsMu.Lock()
			defer a.accountsMu.Unlock()
			if id < 0 || int(id) >= len(a.accountsData) {
				return
			}
			account := a.accountsData[id]
			statusCircle := hbox.Objects[0].(*canvas.Circle)
			emailLabel := hbox.Objects[1].(*widget.Label)
			statusLabel := hbox.Objects[2].(*widget.Label)

			emailLabel.SetText(account.Email + ":" + account.Password)

			if account.Valid {
				statusCircle.FillColor = colorGreen
				statusLabel.SetText("Valid")
			} else {
				statusCircle.FillColor = colorRed
				statusLabel.SetText("Invalid")
			}
			statusCircle.Refresh()
		},
	)

	// Export button
	exportBtn := widget.NewButtonWithIcon("Export Results", theme.DocumentSaveIcon(), func() {
		dialog.ShowFileSave(func(writer fyne.URIWriteCloser, err error) {
			if err != nil || writer == nil {
				return
			}
			a.accountsMu.Lock()
			defer a.accountsMu.Unlock()
			var validCount, invalidCount int
			for _, account := range a.accountsData {
				status := "Invalid"
				if account.Valid {
					status = "Valid"
					validCount++
				} else {
					invalidCount++
				}
				fmt.Fprintf(writer, "%s:%s | %s\n", account.Email, account.Password, status)
			}
			fmt.Fprintf(writer, "\n--- Summary ---\n")
			fmt.Fprintf(writer, "Total: %d\n", len(a.accountsData))
			fmt.Fprintf(writer, "Valid: %d\n", validCount)
			fmt.Fprintf(writer, "Invalid: %d\n", invalidCount)
			writer.Close()
			dialog.ShowInformation("Export Complete", fmt.Sprintf("Exported %d accounts to %s", len(a.accountsData), writer.URI().Path()), a.mainWindow)
		}, a.mainWindow)
	})

	// New check button
	newCheckBtn := widget.NewButtonWithIcon("New Check", theme.ContentAddIcon(), func() {
		a.accountsMu.Lock()
		a.accountsData = make([]*models.AccountData, 0)
		a.accountsMu.Unlock()
		a.showSetupPanel()
	})

	// Create layout
	a.resultsContainer = container.NewVBox(
		container.NewCenter(
			canvas.NewText("Results", colorBlue),
		),
		container.NewMax(
			a.resultsList,
		),
		container.NewHBox(
			layout.NewSpacer(),
			exportBtn,
			newCheckBtn,
		),
	)
}

// showSetupPanel shows the setup panel
func (a *App) showSetupPanel() {
	a.setupContainer.Show()
	a.progressContainer.Hide()
	a.resultsContainer.Hide()
}

// showProgressPanel shows the progress panel
func (a *App) showProgressPanel() {
	a.setupContainer.Hide()
	a.progressContainer.Show()
	a.resultsContainer.Hide()
}

// showResultsPanel shows the results panel
func (a *App) showResultsPanel() {
	a.setupContainer.Hide()
	a.progressContainer.Hide()
	a.resultsContainer.Show()
}

// Handler for check start
func (a *App) handleCheckStart(total int) {
	fyne.Do(func() {
		a.isChecking = true
		a.totalAccounts = total
		a.progressBar.Min = 0
		a.progressBar.Max = float64(total)
		a.progressBar.SetValue(0)
		a.progressLabel.SetText(fmt.Sprintf("Processing 0/%d accounts", total))
		a.showProgressPanel()
	})
}

// Handler for check progress
func (a *App) handleCheckProgress(processed, valid, invalid int, speed float64, eta time.Duration) {
	fyne.Do(func() {
		a.progressBar.SetValue(float64(processed))
		a.progressLabel.SetText(fmt.Sprintf("Processing %d/%d accounts", processed, a.totalAccounts))
		a.statsProcessed.SetText(fmt.Sprintf("Processed: %d", processed))
		a.statsValid.SetText(fmt.Sprintf("Valid: %d", valid))
		a.statsInvalid.SetText(fmt.Sprintf("Invalid: %d", invalid))
		a.statsSpeed.SetText(fmt.Sprintf("Speed: %.1f/s", speed))
		minutes := int(eta.Minutes())
		seconds := int(eta.Seconds()) % 60
		a.statsEta.SetText(fmt.Sprintf("ETA: %02d:%02d", minutes, seconds))
	})
}

// Handler for check result
func (a *App) handleCheckResult(account *models.AccountData) {
	fyne.Do(func() {
		a.accountsMu.Lock()
		a.accountsData = append(a.accountsData, account)
		a.accountsMu.Unlock()
		a.resultsList.Refresh()
	})
}

// Handler for check complete
func (a *App) handleCheckComplete() {
	fyne.Do(func() {
		a.isChecking = false
		a.progressBar.SetValue(a.progressBar.Max)
		a.progressLabel.SetText("Check completed!")
		dialog.ShowInformation("Check Complete", fmt.Sprintf("Processed %d accounts", len(a.accountsData)), a.mainWindow)
		a.showResultsPanel()
	})
}