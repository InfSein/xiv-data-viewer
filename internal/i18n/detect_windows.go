//go:build windows

package i18n

import (
	"syscall"
	"unsafe"
)

const muiLanguageName = 0x8 // MUI_LANGUAGE_NAME

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procGetUserPreferredUILangs  = kernel32.NewProc("GetUserPreferredUILanguages")
	procGetUserDefaultUILanguage = kernel32.NewProc("GetUserDefaultUILanguage")
	procLCIDToLocaleName         = kernel32.NewProc("LCIDToLocaleName")
	procGetUserDefaultLocaleName = kernel32.NewProc("GetUserDefaultLocaleName")
)

// detectSystemLocaleTags 读取 Windows 系统显示语言与区域偏好列表，避免在无窗口循环时阻塞。
func detectSystemLocaleTags() []string {
	var tags []string

	var count, size uint32
	if ok, _, _ := procGetUserPreferredUILangs.Call(muiLanguageName, uintptr(unsafe.Pointer(&count)), 0, uintptr(unsafe.Pointer(&size))); ok != 0 && size > 0 {
		buf := make([]uint16, size)
		if ok2, _, _ := procGetUserPreferredUILangs.Call(muiLanguageName, uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); ok2 != 0 {
			start := 0
			for i, c := range buf {
				if c == 0 {
					if i > start {
						tags = append(tags, syscall.UTF16ToString(buf[start:i]))
					}
					start = i + 1
				}
			}
		}
	}

	buf := make([]uint16, 85) // LOCALE_NAME_MAX_LENGTH
	if id, _, _ := procGetUserDefaultUILanguage.Call(); id != 0 {
		if n, _, _ := procLCIDToLocaleName.Call(id, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0); n > 0 {
			tags = append(tags, syscall.UTF16ToString(buf[:n]))
		}
	}

	if n, _, _ := procGetUserDefaultLocaleName.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); n > 0 {
		tags = append(tags, syscall.UTF16ToString(buf[:n]))
	}

	tags = append(tags, envLocaleTags()...)
	return tags
}
