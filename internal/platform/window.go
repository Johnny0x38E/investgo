package platform

import (
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// BuildMainWindowOptions creates the baseline main window options and applies platform-specific window behaviour.
func BuildMainWindowOptions(useNativeTitleBar bool) application.WebviewWindowOptions {
	return buildMainWindowOptions(useNativeTitleBar, runtime.GOOS)
}

func buildMainWindowOptions(useNativeTitleBar bool, targetOS string) application.WebviewWindowOptions {
	// Match the light --app-bg so AppKit can pick inactive traffic-light contrast correctly
	// on recent macOS. The previous warm cream frame made inactive buttons look transparent.
	options := application.WebviewWindowOptions{
		Name:             "main",
		Title:            "InvestGo",
		URL:              "/",
		Width:            1200,
		Height:           828,
		MinWidth:         1200,
		MinHeight:        828,
		BackgroundColour: application.NewRGB(251, 252, 254),
		Windows: application.WindowsWindow{
			Theme: application.SystemDefault,
		},
		Mac: application.MacWindow{
			// Liquid Glass keeps native round traffic lights and a visible inactive state on
			// macOS 26+. Older releases fall back to the translucent material inside Wails.
			Backdrop: application.MacBackdropLiquidGlass,
		},
	}

	if !useNativeTitleBar {
		if targetOS == "darwin" {
			// HiddenInset (not Unified) keeps the standard circular traffic lights.
			// Unified toolbar styling can flatten them on recent macOS.
			options.Mac.TitleBar = application.MacTitleBarHiddenInset
		} else {
			options.Frameless = true
		}
	}

	return options
}
