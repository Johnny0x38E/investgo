package platform

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestBuildMainWindowOptionsDarwinCustomTitleBar(t *testing.T) {
	t.Parallel()

	options := buildMainWindowOptions(false, "darwin")

	if options.Frameless {
		t.Fatal("darwin custom title bar must stay framed so native traffic lights remain")
	}
	if options.Mac.Backdrop != application.MacBackdropLiquidGlass {
		t.Fatalf("Backdrop = %v, want MacBackdropLiquidGlass", options.Mac.Backdrop)
	}
	if options.Mac.TitleBar != application.MacTitleBarHiddenInset {
		t.Fatalf("TitleBar = %+v, want MacTitleBarHiddenInset", options.Mac.TitleBar)
	}
	if got, want := options.BackgroundColour, application.NewRGB(251, 252, 254); got != want {
		t.Fatalf("BackgroundColour = %+v, want %+v", got, want)
	}
}

func TestBuildMainWindowOptionsDarwinNativeTitleBar(t *testing.T) {
	t.Parallel()

	options := buildMainWindowOptions(true, "darwin")

	if options.Mac.TitleBar != (application.MacTitleBar{}) {
		t.Fatalf("native title bar should leave TitleBar zero-value, got %+v", options.Mac.TitleBar)
	}
	if options.Frameless {
		t.Fatal("native title bar must not force frameless")
	}
}

func TestBuildMainWindowOptionsWindowsCustomTitleBar(t *testing.T) {
	t.Parallel()

	options := buildMainWindowOptions(false, "windows")

	if !options.Frameless {
		t.Fatal("windows custom title bar must be frameless")
	}
}
