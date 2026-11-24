package gamedata

import (
	"fmt"
	"math"
	"time"
)

// eorzeaMultiplier 表示艾欧泽亚时间与现实时间换算比例（1 艾欧泽亚日 = 70 现实分钟）。
const eorzeaMultiplier = 3600.0 / 175.0

// EorzeaClock 表示当前时刻对应的艾欧泽亚时钟。
type EorzeaClock struct {
	Hours             int
	Minutes           int
	TotalMinutesOfDay int
	Formatted         string
}

// GetCurrentEorzeaTime 将指定现实时刻换算为艾欧泽亚时间 (ET)。
func GetCurrentEorzeaTime(now time.Time) EorzeaClock {
	eorzeaMs := float64(now.UnixMilli()) * eorzeaMultiplier
	totalMinutes := int(math.Floor(eorzeaMs / 60000.0))
	modDay := ((totalMinutes % 1440) + 1440) % 1440
	hours := modDay / 60
	minutes := modDay % 60
	return EorzeaClock{
		Hours:             hours,
		Minutes:           minutes,
		TotalMinutesOfDay: modDay,
		Formatted:         fmt.Sprintf("%02d:%02d", hours, minutes),
	}
}

func eorzeaMinutesToRealSeconds(etMinutes int) int {
	secs := math.Round((float64(etMinutes) * 60.0) / eorzeaMultiplier)
	if secs < 0 {
		return 0
	}
	return int(secs)
}

// FormatRealDuration 将现实秒数格式化为 "X分YY秒" 或 "Y秒"。
func FormatRealDuration(seconds int) string {
	if seconds < 0 {
		seconds = 0
	}
	mins := seconds / 60
	secs := seconds % 60
	if mins <= 0 {
		return fmt.Sprintf("%d秒", secs)
	}
	return fmt.Sprintf("%d分%02d秒", mins, secs)
}

// TimedNodeActiveStatus 表示限时采集点在当前 ET 下的开放状态与现实倒计时。
type TimedNodeActiveStatus struct {
	IsActive             bool
	ActiveWindow         *GatheringTimeWindow
	NextWindow           *GatheringTimeWindow
	RealSecondsRemaining int
	StatusText           string
}

// EvaluateGatheringWindows 评估限时采集点当前是否处于开放窗口及距离下次变化的现实时间。
func EvaluateGatheringWindows(windows []GatheringTimeWindow, now time.Time) TimedNodeActiveStatus {
	if len(windows) == 0 {
		return TimedNodeActiveStatus{
			IsActive:   true,
			StatusText: "常驻开放",
		}
	}

	et := GetCurrentEorzeaTime(now)
	curMin := et.TotalMinutesOfDay

	for i := range windows {
		w := windows[i]
		elapsed := (curMin - w.StartMinuteOfDay + 1440) % 1440
		if elapsed < w.DurationMinutes {
			etRemain := w.DurationMinutes - elapsed
			realSec := eorzeaMinutesToRealSeconds(etRemain)
			winCopy := w
			return TimedNodeActiveStatus{
				IsActive:             true,
				ActiveWindow:         &winCopy,
				RealSecondsRemaining: realSec,
				StatusText:           fmt.Sprintf("采集开放中 · 剩余 %s", FormatRealDuration(realSec)),
			}
		}
	}

	bestWait := 1441
	nextWin := windows[0]
	for _, w := range windows {
		wait := (w.StartMinuteOfDay - curMin + 1440) % 1440
		if wait < bestWait {
			bestWait = wait
			nextWin = w
		}
	}

	realWait := eorzeaMinutesToRealSeconds(bestWait)
	winCopy := nextWin
	return TimedNodeActiveStatus{
		IsActive:             false,
		NextWindow:           &winCopy,
		RealSecondsRemaining: realWait,
		StatusText:           fmt.Sprintf("距下次开放 (%s) 还有 %s", nextWin.StartET, FormatRealDuration(realWait)),
	}
}
