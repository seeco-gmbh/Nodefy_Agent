//go:build windows

package logging

import (
	"syscall"
	"unsafe"
)

var (
	user32          = syscall.NewLazyDLL("user32.dll")
	messageBoxW     = user32.NewProc("MessageBoxW")
)

const (
	MB_OK              = 0x00000000
	MB_ICONERROR       = 0x00000010
	MB_SETFOREGROUND   = 0x00010000
)

// showWindowsErrorDialog displays a Windows message box with the error
func showWindowsErrorDialog(message string) {
	title := "Nodefy Agent Error"
	
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	messagePtr, _ := syscall.UTF16PtrFromString(message + "\n\nCheck ~/.nodefy/agent.log for details.")
	
	messageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(messagePtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(MB_OK|MB_ICONERROR|MB_SETFOREGROUND),
	)
}
