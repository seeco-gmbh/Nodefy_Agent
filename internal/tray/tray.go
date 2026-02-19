//go:build (windows || darwin || linux) && cgo

package tray

import (
	"runtime"

	"github.com/getlantern/systray"
)

var iconWhite []byte
var iconBlue []byte

func SetIcons(white, blue []byte) {
	iconWhite = white
	iconBlue = blue
}

type StatusProvider interface {
	ClientCount() int
	IsRunning() bool
}

type Tray struct {
	statusProvider StatusProvider
	onQuit         func()
}

func New(statusProvider StatusProvider, onQuit func()) *Tray {
	return &Tray{
		statusProvider: statusProvider,
		onQuit:         onQuit,
	}
}

func (t *Tray) Run() {
	systray.Run(t.onReady, t.onExit)
}

func (t *Tray) onReady() {
	if runtime.GOOS == "darwin" {
		systray.SetTemplateIcon(iconWhite, iconWhite)
	} else {
		systray.SetIcon(iconBlue)
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
