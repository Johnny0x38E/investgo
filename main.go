package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"investgo/internal/api"
	"investgo/internal/core"
	"investgo/internal/core/hot"
	"investgo/internal/core/marketdata"
	"investgo/internal/core/pool"
	"investgo/internal/core/store"
	"investgo/internal/logger"
	"investgo/internal/platform"
	sqlitestorage "investgo/internal/storage/sqlite"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var (
	defaultTerminalLogging = "0"
	defaultDevToolsBuild   = "0"
	appVersion             = "dev"
)

// Embed frontend build assets for Wails to serve as static resources at runtime.
//
//go:embed frontend/dist
var frontendAssets embed.FS

// Embed application icon
//
//go:embed build/appicon.png
var appIcon []byte

func main() {
	logs := logger.NewLogBook(400)
	if terminalLoggingEnabled() {
		logs.EnableConsole(os.Stderr)
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetOutput(logs.Writer("backend", "stdlib", logger.DeveloperLogError))
	if err := logs.ConfigureFile(defaultLogPath()); err != nil {
		log.Printf("configure log file: %v", err)
	}
	defer func() { _ = logs.Close() }() // nolint:errcheck

	logs.Info("backend", "app", "starting InvestGo")

	// Bootstrap the shared HTTP transport with the "system" default so that
	// HTTP clients are available before the Store is initialised. The transport
	// will be updated to the actual persisted setting once the Store is ready.
	proxyTransport, err := platform.NewProxyTransport("system", "")
	if err != nil {
		log.Fatalf("initialise proxy transport: %v", err)
	}
	httpClient := platform.NewHTTPClient(proxyTransport)

	var appStore *store.Store
	currentSettings := func() core.AppSettings {
		if appStore == nil {
			return core.AppSettings{}
		}
		return appStore.CurrentSettings()
	}
	registry := marketdata.DefaultRegistry(httpClient, currentSettings)

	legacyStatePath, databasePath := defaultStoragePaths()
	migrationResult, err := store.EnsureSQLiteState(context.Background(), legacyStatePath, databasePath)
	if err != nil {
		log.Fatalf("prepare sqlite state: %v", err)
	}
	if migrationResult.Migrated {
		logs.Info("backend", "storage", "migrated legacy JSON state to SQLite")
	}

	appDatabase, err := sqlitestorage.Open(databasePath)
	if err != nil {
		log.Fatalf("open sqlite state: %v", err)
	}
	instrumentRepository := sqlitestorage.NewInstrumentRepository(appDatabase)
	poolRepository := sqlitestorage.NewPoolRepository(appDatabase)
	if err := hot.SeedBuiltInPools(context.Background(), hot.Repositories{
		Instruments: instrumentRepository,
		Pools:       poolRepository,
	}, hot.BuiltInPoolDataVersion); err != nil {
		_ = appDatabase.Close() // nolint:errcheck
		log.Fatalf("seed built-in instrument pools: %v", err)
	}
	var closeDatabaseOnce sync.Once
	var closeDatabaseErr error
	closeDatabase := func() error {
		closeDatabaseOnce.Do(func() {
			closeDatabaseErr = appDatabase.Close()
		})
		return closeDatabaseErr
	}

	appStore, err = store.NewStoreWithRepository(
		store.NewSQLiteRepository(appDatabase, databasePath),
		registry.QuoteProviders(),
		registry.QuoteSourceOptions(),
		registry.NewHistoryRouter(currentSettings),
		logs,
		appVersion,
		httpClient, // shared http.Client so FX rate requests respect the configured proxy transport
	)
	if err != nil {
		_ = closeDatabase() // nolint:errcheck
		log.Fatalf("initialise store: %v", err)
	}

	// The Store is now loaded — sync the proxy transport with the persisted
	// settings. ApplySystemProxy populates the process environment before the
	// transport snapshots it for "system" mode.
	// Only read persisted settings here. Building a full snapshot before the
	// asynchronous FX fetch would cache dashboard values without current rates.
	settings := appStore.CurrentSettings()
	proxyMode := settings.ProxyMode
	proxyURL := settings.ProxyURL
	logs.Info("backend", "proxy", fmt.Sprintf("proxy mode: %s", proxyMode))
	if proxyMode == "system" {
		platform.ApplySystemProxy(logs)
	} else if proxyMode == "custom" && proxyURL != "" {
		logs.Info("backend", "proxy", fmt.Sprintf("custom proxy: %s", proxyURL))
	}
	if err := proxyTransport.Update(proxyMode, proxyURL); err != nil {
		log.Fatalf("configure proxy transport: %v", err)
	}
	appStore.StartInitialFXFetch()

	poolService := pool.NewService(instrumentRepository, poolRepository)
	hotService := hot.NewHotService(httpClient, logs.NewSlogLogger("hot", slog.LevelInfo), registry, poolService)

	frontendFS, err := fs.Sub(frontendAssets, "frontend/dist")
	if err != nil {
		log.Fatalf("load frontend assets: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", api.NewHandler(appStore, hotService, logs, proxyTransport, poolService))
	mux.Handle("/", application.BundledAssetFileServer(frontendFS))

	app := application.New(application.Options{
		Name:        "InvestGo",
		Description: "Go + Wails v3 Investment Monitor Desktop App",
		Icon:        appIcon,
		Logger:      logs.NewSlogLogger("system", slog.LevelWarn),
		Assets: application.AssetOptions{
			Handler:        mux,
			DisableLogging: true,
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		PanicHandler: func(details *application.PanicDetails) {
			logs.Error("backend", "panic", fmt.Sprintf("%s\n%s", details.Error, details.StackTrace))
		},
		OnShutdown: func() {
			logs.Info("backend", "app", "shutdown requested")
			// Flush pending writes before closing SQLite so dirty state is not lost.
			if err := appStore.Flush(); err != nil {
				logs.Error("backend", "storage", fmt.Sprintf("flush state on shutdown failed: %v", err))
			}
			if err := closeDatabase(); err != nil {
				logs.Error("backend", "storage", fmt.Sprintf("close sqlite state on shutdown failed: %v", err))
			}
		},
	})

	useNativeTitleBar := settings.UseNativeTitleBar
	windowOptions := platform.BuildMainWindowOptions(useNativeTitleBar)
	windowOptions.KeyBindings = map[string]func(window application.Window){
		"F12": func(window application.Window) {
			snapshot := appStore.Snapshot()
			if !snapshot.Settings.DeveloperMode {
				logs.Warn("system", "devtools", "ignored F12 because developer mode is disabled")
				return
			}
			if !devToolsBuildEnabled() {
				logs.Warn("system", "devtools", "ignored F12 because this binary was built without devtools support")
				return
			}
			logs.Info("system", "devtools", "opening web inspector")
			window.OpenDevTools()
		},
	}

	app.Window.NewWithOptions(windowOptions)

	if err := app.Run(); err != nil {
		if flushErr := appStore.Flush(); flushErr != nil {
			logs.Error("backend", "storage", fmt.Sprintf("flush state after run failure: %v", flushErr))
		}
		if closeErr := closeDatabase(); closeErr != nil {
			logs.Error("backend", "storage", fmt.Sprintf("close sqlite state after run failure: %v", closeErr))
		}
		log.Printf("run app: %v", err)
		os.Exit(1)
	}
}

func defaultStoragePaths() (jsonPath, databasePath string) {
	configDirectory, err := os.UserConfigDir()
	if err != nil {
		configDirectory = ""
	}
	return storagePathsForConfigDirectory(configDirectory)
}

func storagePathsForConfigDirectory(configDirectory string) (jsonPath, databasePath string) {
	baseDirectory := filepath.Join("data")
	if configDirectory != "" {
		baseDirectory = filepath.Join(configDirectory, "investgo")
	}
	return filepath.Join(baseDirectory, "state.json"), filepath.Join(baseDirectory, "investgo.db")
}

// defaultLogPath returns the default storage path for the log file.
// Log files and state.json are located at $HOME/Library/Application Support/investgo/.
func defaultLogPath() string {
	if configDir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(configDir, "investgo", "logs", "app.log")
	}

	return filepath.Join(".", "data", "logs", "app.log")
}

// terminalLoggingEnabled returns whether the current process should output development logs to the terminal.
func terminalLoggingEnabled() bool {
	if defaultTerminalLogging == "1" {
		return true
	}

	for _, arg := range os.Args[1:] {
		if arg == "-dev" || arg == "--dev" {
			return true
		}
	}

	return false
}

// devToolsBuildEnabled returns whether the current binary has DevTools support enabled.
func devToolsBuildEnabled() bool {
	return defaultDevToolsBuild == "1"
}
