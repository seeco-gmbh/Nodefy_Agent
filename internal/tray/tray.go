//go:build (windows || darwin || linux) && cgo

package tray

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"runtime"

	"github.com/getlantern/systray"
)

// StatusProvider provides status information
type StatusProvider interface {
	ClientCount() int
	IsRunning() bool
}

// Tray represents the system tray icon
type Tray struct {
	statusProvider StatusProvider
	onQuit         func()
}

// New creates a new Tray
func New(statusProvider StatusProvider, onQuit func()) *Tray {
	return &Tray{
		statusProvider: statusProvider,
		onQuit:         onQuit,
	}
}

// Run starts the system tray (blocking)
func (t *Tray) Run() {
	systray.Run(t.onReady, t.onExit)
}

func (t *Tray) onReady() {
	// Generate icon dynamically
	icon := generateIcon()

	// Use template icon on macOS (adapts to dark/light mode)
	if runtime.GOOS == "darwin" {
		systray.SetTemplateIcon(icon, icon)
	} else {
		systray.SetIcon(icon)
	}
	systray.SetTitle("")
	systray.SetTooltip("Nodefy Agent")

	mStatus := systray.AddMenuItem("Nodefy Agent - Running", "")
	mStatus.Disable()

	systray.AddSeparator()

	mQuit := systray.AddMenuItem("Quit", "Stop the agent")

	go func() {
		<-mQuit.ClickedCh
		if t.onQuit != nil {
			t.onQuit()
		}
		systray.Quit()
	}()
}

func (t *Tray) onExit() {}

// UpdateStatus updates the tray tooltip
func (t *Tray) UpdateStatus(connections int) {
	systray.SetTooltip(fmt.Sprintf("Nodefy Agent - %d connections", connections))
}

// generateIcon creates a 22x22 PNG icon with letter "N"
func generateIcon() []byte {
	const size = 22
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	// Icon color - white for macOS template, blue for others
	var iconColor color.RGBA
	if runtime.GOOS == "darwin" {
		iconColor = color.RGBA{255, 255, 255, 255} // White
	} else {
		iconColor = color.RGBA{59, 130, 246, 255} // Blue
	}

	// Draw "N" letter (simplified pixel art)
	// Left vertical bar
	for y := 4; y < 18; y++ {
		for x := 4; x < 7; x++ {
			img.Set(x, y, iconColor)
		}
	}
	// Right vertical bar
	for y := 4; y < 18; y++ {
		for x := 15; x < 18; x++ {
			img.Set(x, y, iconColor)
		}
	}
	// Diagonal
	for i := 0; i < 14; i++ {
		x := 4 + i
		y := 4 + i
		if x < 18 && y < 18 {
			img.Set(x, y, iconColor)
			img.Set(x+1, y, iconColor)
			img.Set(x+2, y, iconColor)
		}
	}

	// Encode to PNG
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}
