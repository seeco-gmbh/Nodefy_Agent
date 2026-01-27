//go:build !(windows || darwin || linux) || !cgo

package tray

import (
	"os"
	"os/signal"
	"syscall"
)

type StatusProvider interface {
	ClientCount() int
	IsRunning() bool
}

type Tray struct {
	onQuit func()
}

func New(statusProvider StatusProvider, onQuit func()) *Tray {
	return &Tray{onQuit: onQuit}
}

func (t *Tray) Run() {
	// No systray - just wait for signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	if t.onQuit != nil {
		t.onQuit()
	}
}

func (t *Tray) UpdateStatus(connections int) {}
