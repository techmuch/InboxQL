//go:build windows

package power

import (
	"syscall"
	"unsafe"
)

// systemPowerStatus is SYSTEM_POWER_STATUS.
type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

var getSystemPowerStatus = syscall.NewLazyDLL("kernel32.dll").NewProc("GetSystemPowerStatus")

func windowsSource() State {
	var s systemPowerStatus
	if r, _, _ := getSystemPowerStatus.Call(uintptr(unsafe.Pointer(&s))); r == 0 {
		return Unknown
	}
	switch s.ACLineStatus {
	case 0:
		return Battery
	case 1:
		return Mains
	default:
		return Unknown
	}
}
