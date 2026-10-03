package update

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	wailsupdater "github.com/wailsapp/wails/v3/pkg/updater"

	"investgo/internal/core"
)

type fakeUpdater struct {
	release       *wailsupdater.Release
	checkErr      error
	downloadErr   error
	restartErr    error
	downloadCalls atomic.Int32
	restartCalls  atomic.Int32
}

func (f *fakeUpdater) Check(context.Context) (*wailsupdater.Release, error) {
	return f.release, f.checkErr
}

func (f *fakeUpdater) DownloadAndInstall(context.Context) error {
	f.downloadCalls.Add(1)
	return f.downloadErr
}

func (f *fakeUpdater) Restart(context.Context) error {
	f.restartCalls.Add(1)
	return f.restartErr
}

type fakeProvider struct {
	name string
}

func (p *fakeProvider) Name() string {
	return p.name
}

func (p *fakeProvider) Check(context.Context, wailsupdater.CheckRequest) (*wailsupdater.Release, error) {
	return nil, nil
}

func (p *fakeProvider) Download(
	_ context.Context,
	_ *wailsupdater.Release,
	_ io.Writer,
	onProgress func(written, total int64),
) error {
	onProgress(50, 100)
	return nil
}

// lockedSettings guards a settings snapshot for tests whose scheduler and
// test goroutine read and write it concurrently, matching the real store.
type lockedSettings struct {
	mu   sync.Mutex
	data core.AppSettings
}

func (s *lockedSettings) load() core.AppSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data
}

func (s *lockedSettings) setAutoUpdateEnabled(enabled bool) {
	s.mu.Lock()
	s.data.AutoUpdateEnabled = enabled
	s.mu.Unlock()
}

func newTestService(
	t *testing.T,
	updater Updater,
	settings *lockedSettings,
	initialDelay time.Duration,
) *Service {
	t.Helper()
	service := New(Options{
		CurrentVersion: "1.0.0",
		Settings:       settings.load,
		InitialDelay:   initialDelay,
		Interval:       time.Hour,
	})
	service.Attach(updater)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	service.Start(ctx)
	return service
}

func waitForState(t *testing.T, service *Service, want string) Status {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := service.Status()
		if status.State == want {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("state = %q, want %q", service.Status().State, want)
	return Status{}
}

func TestCheckFindsUpdateWithoutBackgroundDownload(t *testing.T) {
	updater := &fakeUpdater{release: &wailsupdater.Release{Version: "1.1.0", Notes: "notes"}}
	settings := &lockedSettings{data: core.AppSettings{AutoUpdateEnabled: true}}
	service := newTestService(t, updater, settings, time.Hour)

	service.TriggerCheck()

	status := waitForState(t, service, StateAvailable)
	if status.AvailableVersion != "1.1.0" {
		t.Fatalf("availableVersion = %q, want 1.1.0", status.AvailableVersion)
	}
	if updater.downloadCalls.Load() != 0 {
		t.Fatalf("download calls = %d, want 0 without background download", updater.downloadCalls.Load())
	}
	if status.LastCheckedAt == nil {
		t.Fatal("lastCheckedAt was not recorded")
	}
}

func TestCheckDownloadsInBackgroundWhenEnabled(t *testing.T) {
	updater := &fakeUpdater{release: &wailsupdater.Release{Version: "1.1.0"}}
	settings := &lockedSettings{data: core.AppSettings{AutoUpdateEnabled: true, AutoUpdateBackgroundDownload: true}}
	service := newTestService(t, updater, settings, time.Hour)

	service.TriggerCheck()

	waitForState(t, service, StateReady)
	if updater.downloadCalls.Load() != 1 {
		t.Fatalf("download calls = %d, want 1", updater.downloadCalls.Load())
	}
}

func TestCheckFailureExposesError(t *testing.T) {
	updater := &fakeUpdater{checkErr: errors.New("network unreachable")}
	settings := &lockedSettings{data: core.AppSettings{AutoUpdateEnabled: true}}
	service := newTestService(t, updater, settings, time.Hour)

	service.TriggerCheck()

	status := waitForState(t, service, StateError)
	if !strings.Contains(status.ErrorMessage, "network unreachable") {
		t.Fatalf("errorMessage = %q, want the check failure", status.ErrorMessage)
	}
}

func TestUpToDateAfterSuccessfulCheckWithoutRelease(t *testing.T) {
	updater := &fakeUpdater{}
	settings := &lockedSettings{data: core.AppSettings{AutoUpdateEnabled: true}}
	service := newTestService(t, updater, settings, time.Hour)

	service.TriggerCheck()

	waitForState(t, service, StateUpToDate)
}

func TestTriggerDownloadRequiresAvailableRelease(t *testing.T) {
	updater := &fakeUpdater{release: &wailsupdater.Release{Version: "1.1.0"}}
	settings := &lockedSettings{data: core.AppSettings{AutoUpdateEnabled: true}}
	service := newTestService(t, updater, settings, time.Hour)

	if err := service.TriggerDownload(); !errors.Is(err, ErrDownloadNotAvailable) {
		t.Fatalf("TriggerDownload before a check = %v, want ErrDownloadNotAvailable", err)
	}

	service.TriggerCheck()
	waitForState(t, service, StateAvailable)

	if err := service.TriggerDownload(); err != nil {
		t.Fatalf("TriggerDownload = %v, want nil", err)
	}
	waitForState(t, service, StateReady)
	if updater.downloadCalls.Load() != 1 {
		t.Fatalf("download calls = %d, want 1", updater.downloadCalls.Load())
	}
}

func TestRestartRequiresReadyUpdate(t *testing.T) {
	updater := &fakeUpdater{release: &wailsupdater.Release{Version: "1.1.0"}}
	settings := &lockedSettings{data: core.AppSettings{AutoUpdateEnabled: true, AutoUpdateBackgroundDownload: true}}
	service := newTestService(t, updater, settings, time.Hour)

	if err := service.Restart(); !errors.Is(err, ErrNoPendingUpdate) {
		t.Fatalf("Restart before download = %v, want ErrNoPendingUpdate", err)
	}

	service.TriggerCheck()
	waitForState(t, service, StateReady)

	if err := service.Restart(); err != nil {
		t.Fatalf("Restart = %v, want nil", err)
	}
	if updater.restartCalls.Load() != 1 {
		t.Fatalf("restart calls = %d, want 1", updater.restartCalls.Load())
	}
}

func TestSchedulerChecksAutomaticallyWhenEnabled(t *testing.T) {
	updater := &fakeUpdater{release: &wailsupdater.Release{Version: "1.1.0"}}
	settings := &lockedSettings{data: core.AppSettings{AutoUpdateEnabled: true}}
	service := newTestService(t, updater, settings, 10*time.Millisecond)

	waitForState(t, service, StateAvailable)
}

func TestSchedulerStaysIdleWhenDisabled(t *testing.T) {
	updater := &fakeUpdater{release: &wailsupdater.Release{Version: "1.1.0"}}
	settings := &lockedSettings{}
	service := newTestService(t, updater, settings, 10*time.Millisecond)

	time.Sleep(100 * time.Millisecond)
	if state := service.Status().State; state != StateIdle {
		t.Fatalf("state = %q, want idle with automatic checks disabled", state)
	}

	settings.setAutoUpdateEnabled(true)
	service.SettingsChanged()
	waitForState(t, service, StateAvailable)
}

func TestStatusUnsupportedWithoutUpdater(t *testing.T) {
	service := New(Options{})
	if service.Status().Supported {
		t.Fatal("supported = true without an updater")
	}
	if err := service.TriggerDownload(); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("TriggerDownload = %v, want ErrNotSupported", err)
	}
}

func TestReportProgressUpdatesDownloadingState(t *testing.T) {
	settings := &lockedSettings{data: core.AppSettings{AutoUpdateEnabled: true}}
	service := newTestService(t, &fakeUpdater{}, settings, time.Hour)

	service.ReportProgress(10, 100)
	if service.Status().Progress != nil {
		t.Fatal("progress recorded outside the downloading state")
	}
}

func TestWithProgressForwardsDownloadProgress(t *testing.T) {
	wrapped := WithProgress(&fakeProvider{name: "fake"}, func(written, total int64) {})

	if wrapped.Name() != "fake" {
		t.Fatalf("name = %q, want fake", wrapped.Name())
	}

	var reported []int64
	progressWrapper, ok := wrapped.(*progressProvider)
	if !ok {
		t.Fatalf("wrapped type = %T, want *progressProvider", wrapped)
	}
	progressWrapper.report = func(written, total int64) {
		reported = append(reported, written, total)
	}

	var forwarded []int64
	if err := wrapped.Download(
		context.Background(),
		nil,
		nil,
		func(written, total int64) { forwarded = append(forwarded, written, total) },
	); err != nil {
		t.Fatalf("download = %v, want nil", err)
	}

	if len(reported) != 2 || reported[0] != 50 || reported[1] != 100 {
		t.Fatalf("reported = %v, want [50 100]", reported)
	}
	if len(forwarded) != 2 || forwarded[0] != 50 || forwarded[1] != 100 {
		t.Fatalf("forwarded = %v, want [50 100]", forwarded)
	}
}
