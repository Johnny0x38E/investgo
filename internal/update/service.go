// Package update coordinates background update checks and downloads for the
// desktop application. It owns the scheduling policy — automatic checks are
// enabled by default and can be turned off in settings — while the Wails
// updater performs the release lookup, download, verification, and restart.
package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	wailsupdater "github.com/wailsapp/wails/v3/pkg/updater"

	"investgo/internal/core"
	"investgo/internal/logger"
)

// State values exposed to the frontend. They mirror the Wails updater's
// lifecycle, collapsed to the states the settings UI renders.
const (
	StateUnconfigured = "unconfigured"
	StateIdle         = "idle"
	StateChecking     = "checking"
	StateUpToDate     = "up-to-date"
	StateAvailable    = "available"
	StateDownloading  = "downloading"
	StateReady        = "ready"
	StateError        = "error"
)

const (
	// defaultInitialDelay waits a moment after startup so the first update
	// check does not compete with the initial quote and FX fetches.
	defaultInitialDelay = 30 * time.Second
	defaultInterval     = 6 * time.Hour
	// minimumRetryDelay throttles scheduler wake-ups triggered by settings
	// changes right after a check already ran.
	minimumRetryDelay = 10 * time.Second
)

// Errors surfaced by the service and mapped onto HTTP statuses by the API.
var (
	ErrNotSupported         = errors.New("update service is not available in this build")
	ErrDownloadNotAvailable = errors.New("no update is available to download")
	ErrNoPendingUpdate      = errors.New("no downloaded update is ready to install")
)

// Updater is the subset of the Wails updater the service drives.
type Updater interface {
	Check(ctx context.Context) (*wailsupdater.Release, error)
	DownloadAndInstall(ctx context.Context) error
	Restart(ctx context.Context) error
}

// Progress is the download progress snapshot exposed to the frontend.
type Progress struct {
	Written int64 `json:"written"`
	Total   int64 `json:"total"`
}

// Status is the update state snapshot exposed to the frontend.
type Status struct {
	Supported        bool       `json:"supported"`
	CurrentVersion   string     `json:"currentVersion"`
	State            string     `json:"state"`
	AvailableVersion string     `json:"availableVersion,omitempty"`
	ReleaseNotes     string     `json:"releaseNotes,omitempty"`
	ErrorMessage     string     `json:"errorMessage,omitempty"`
	LastCheckedAt    *time.Time `json:"lastCheckedAt,omitempty"`
	Progress         *Progress  `json:"progress,omitempty"`
}

// Options configures a Service.
type Options struct {
	CurrentVersion string
	Settings       func() core.AppSettings
	Logs           *logger.LogBook
	// InitialDelay and Interval override the scheduler defaults; tests use them.
	InitialDelay time.Duration
	Interval     time.Duration
}

// Service schedules update checks, tracks the update state for the settings UI,
// and forwards manual check/download/restart requests to the Wails updater.
type Service struct {
	mu       sync.Mutex
	updater  Updater
	settings func() core.AppSettings
	logs     *logger.LogBook

	currentVersion string
	initialDelay   time.Duration
	interval       time.Duration

	state            string
	availableVersion string
	releaseNotes     string
	errorMessage     string
	lastCheckedAt    *time.Time
	progress         *Progress

	busy bool

	ctx     context.Context
	cancel  context.CancelFunc
	started bool
	wake    chan struct{}
}

// New creates a Service. Call Start to launch the scheduler and Attach to
// connect the Wails updater.
func New(opts Options) *Service {
	service := &Service{
		settings:       opts.Settings,
		logs:           opts.Logs,
		currentVersion: opts.CurrentVersion,
		initialDelay:   opts.InitialDelay,
		interval:       opts.Interval,
		state:          StateUnconfigured,
		wake:           make(chan struct{}, 1),
	}
	if service.initialDelay <= 0 {
		service.initialDelay = defaultInitialDelay
	}
	if service.interval <= 0 {
		service.interval = defaultInterval
	}
	return service
}

// Attach connects the Wails updater. A nil updater keeps the service
// unsupported (development builds and provider failures).
func (s *Service) Attach(updater Updater) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updater = updater
	if updater == nil {
		s.state = StateUnconfigured
		return
	}
	s.state = StateIdle
}

// Start launches the background scheduler. It is safe to call once.
func (s *Service) Start(parent context.Context) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	ctx, cancel := context.WithCancel(parent)
	s.ctx = ctx
	s.cancel = cancel
	s.mu.Unlock()

	go s.loop(ctx)
}

// Stop cancels the scheduler and any in-flight check or download.
func (s *Service) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// SettingsChanged nudges the scheduler to re-evaluate the check cadence after
// the auto-update settings were saved.
func (s *Service) SettingsChanged() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// TriggerCheck starts an asynchronous check unless one is already running.
func (s *Service) TriggerCheck() {
	s.mu.Lock()
	if s.updater == nil || s.ctx == nil || s.busy {
		s.mu.Unlock()
		return
	}
	s.busy = true
	s.state = StateChecking
	s.errorMessage = ""
	s.progress = nil
	s.mu.Unlock()

	go func() {
		defer s.finishBusy()
		s.check(s.ctx)
	}()
}

// TriggerDownload starts the download of the release found by the last check.
func (s *Service) TriggerDownload() error {
	s.mu.Lock()
	if s.updater == nil || s.ctx == nil {
		s.mu.Unlock()
		return ErrNotSupported
	}
	if s.busy {
		// A check or download is already running; report the current state.
		s.mu.Unlock()
		return nil
	}
	if s.state != StateAvailable {
		s.mu.Unlock()
		return ErrDownloadNotAvailable
	}
	s.busy = true
	s.mu.Unlock()

	go func() {
		defer s.finishBusy()
		s.download(s.ctx)
	}()
	return nil
}

// Restart installs the staged update and relaunches the application.
func (s *Service) Restart() error {
	s.mu.Lock()
	updater := s.updater
	ctx := s.ctx
	ready := s.state == StateReady
	s.mu.Unlock()

	if updater == nil {
		return ErrNotSupported
	}
	if !ready {
		return ErrNoPendingUpdate
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return updater.Restart(ctx)
}

// ReportProgress receives download progress from the wrapped provider.
func (s *Service) ReportProgress(written, total int64) {
	s.mu.Lock()
	if s.state == StateDownloading {
		s.progress = &Progress{Written: written, Total: total}
	}
	s.mu.Unlock()
}

// Status returns a snapshot for the settings UI.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := Status{
		Supported:        s.updater != nil,
		CurrentVersion:   s.currentVersion,
		State:            s.state,
		AvailableVersion: s.availableVersion,
		ReleaseNotes:     s.releaseNotes,
		ErrorMessage:     s.errorMessage,
	}
	if s.lastCheckedAt != nil {
		lastCheckedAt := *s.lastCheckedAt
		status.LastCheckedAt = &lastCheckedAt
	}
	if s.progress != nil {
		progress := *s.progress
		status.Progress = &progress
	}
	return status
}

// loop runs the automatic check schedule. A zero delay means automatic checks
// are disabled; the loop then waits for a settings change or shutdown.
func (s *Service) loop(ctx context.Context) {
	for {
		wait := s.nextCheckDelay()
		var timer *time.Timer
		var timerC <-chan time.Time
		if wait > 0 {
			timer = time.NewTimer(wait)
			timerC = timer.C
		}
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-s.wake:
			if timer != nil {
				timer.Stop()
			}
		case <-timerC:
			s.TriggerCheck()
		}
	}
}

// nextCheckDelay returns the time until the next automatic check, or 0 when
// automatic checks are disabled.
func (s *Service) nextCheckDelay() time.Duration {
	if !s.autoCheckEnabled() {
		return 0
	}
	s.mu.Lock()
	lastCheckedAt := s.lastCheckedAt
	s.mu.Unlock()
	if lastCheckedAt == nil {
		return s.initialDelay
	}
	remaining := s.interval - time.Since(*lastCheckedAt)
	if remaining < minimumRetryDelay {
		return minimumRetryDelay
	}
	return remaining
}

func (s *Service) autoCheckEnabled() bool {
	s.mu.Lock()
	supported := s.updater != nil
	s.mu.Unlock()
	if !supported || s.settings == nil {
		return false
	}
	return s.settings().AutoUpdateEnabled
}

func (s *Service) backgroundDownloadEnabled() bool {
	if s.settings == nil {
		return false
	}
	return s.settings().AutoUpdateBackgroundDownload
}

// check performs one synchronous check cycle and optionally downloads the
// release when background downloading is enabled.
func (s *Service) check(ctx context.Context) {
	s.mu.Lock()
	updater := s.updater
	s.mu.Unlock()
	if updater == nil {
		return
	}

	release, err := updater.Check(ctx)
	now := time.Now()

	s.mu.Lock()
	s.lastCheckedAt = &now
	if err != nil {
		s.state = StateError
		s.errorMessage = err.Error()
		s.mu.Unlock()
		s.logWarn(fmt.Sprintf("check failed: %v", err))
		return
	}
	if release == nil {
		s.state = StateUpToDate
		s.availableVersion = ""
		s.releaseNotes = ""
		s.mu.Unlock()
		s.logInfo("already up to date")
		return
	}
	if s.state == StateReady && s.availableVersion == release.Version {
		// The same release is already downloaded; keep the ready state so the
		// scheduler cannot re-download it.
		s.mu.Unlock()
		s.logInfo("update already downloaded")
		return
	}
	s.state = StateAvailable
	s.availableVersion = release.Version
	s.releaseNotes = release.Notes
	s.mu.Unlock()
	s.logInfo(fmt.Sprintf("update available: %s", release.Version))

	if s.backgroundDownloadEnabled() {
		s.download(ctx)
	}
}

// download downloads and stages the pending release. Callers own the busy flag.
func (s *Service) download(ctx context.Context) {
	s.mu.Lock()
	updater := s.updater
	s.state = StateDownloading
	s.progress = &Progress{}
	s.errorMessage = ""
	s.mu.Unlock()
	if updater == nil {
		return
	}

	err := updater.DownloadAndInstall(ctx)

	s.mu.Lock()
	s.progress = nil
	if err != nil {
		s.state = StateError
		s.errorMessage = err.Error()
		s.mu.Unlock()
		s.logWarn(fmt.Sprintf("download failed: %v", err))
		return
	}
	s.state = StateReady
	s.mu.Unlock()
	s.logInfo("update downloaded and ready to install")
}

func (s *Service) finishBusy() {
	s.mu.Lock()
	s.busy = false
	s.mu.Unlock()
}

func (s *Service) logInfo(message string) {
	if s.logs != nil {
		s.logs.Info("backend", "update", message)
	}
}

func (s *Service) logWarn(message string) {
	if s.logs != nil {
		s.logs.Warn("backend", "update", message)
	}
}

// WithProgress wraps a provider so download progress also reaches report.
// The Wails updater forwards provider progress to its own events; this keeps
// the service's polling-friendly state in sync without subscribing to them.
func WithProgress(inner wailsupdater.Provider, report func(written, total int64)) wailsupdater.Provider {
	return &progressProvider{inner: inner, report: report}
}

type progressProvider struct {
	inner  wailsupdater.Provider
	report func(written, total int64)
}

func (p *progressProvider) Name() string {
	return p.inner.Name()
}

func (p *progressProvider) Check(
	ctx context.Context,
	request wailsupdater.CheckRequest,
) (*wailsupdater.Release, error) {
	return p.inner.Check(ctx, request)
}

func (p *progressProvider) Download(
	ctx context.Context,
	release *wailsupdater.Release,
	dst io.Writer,
	onProgress func(written, total int64),
) error {
	return p.inner.Download(ctx, release, dst, func(written, total int64) {
		if p.report != nil {
			p.report(written, total)
		}
		if onProgress != nil {
			onProgress(written, total)
		}
	})
}
