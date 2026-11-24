package uiapp

import (
	"fmt"
	"sync"

	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// BitmapCache 管理游戏图标与地图贴图的异步解码与 GPU 位图缓存，防止在 UI 主线程阻塞。
type BitmapCache struct {
	mu      sync.Mutex
	maxSize int
	order   []string
	bitmaps map[string]*ui.Bitmap
	missing map[string]bool
	pending map[string]bool
	sem     chan struct{}
}

// NewBitmapCache 创建容量上限为 maxSize 的 GPU 位图缓存。
func NewBitmapCache(maxSize int) *BitmapCache {
	if maxSize <= 0 {
		maxSize = 600
	}
	return &BitmapCache{
		maxSize: maxSize,
		order:   make([]string, 0, maxSize),
		bitmaps: make(map[string]*ui.Bitmap, maxSize),
		missing: make(map[string]bool),
		pending: make(map[string]bool),
		sem:     make(chan struct{}, 4),
	}
}

// Clear 清空所有缓存位图（切换客户端路径重新加载数据时调用）。
func (b *BitmapCache) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.order = b.order[:0]
	b.bitmaps = make(map[string]*ui.Bitmap, b.maxSize)
	b.missing = make(map[string]bool)
	b.pending = make(map[string]bool)
}

func (b *BitmapCache) put(key string, bmp *ui.Bitmap) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.pending, key)
	if bmp == nil {
		b.missing[key] = true
		return
	}
	if _, exists := b.bitmaps[key]; !exists {
		if len(b.order) >= b.maxSize {
			oldest := b.order[0]
			b.order = b.order[1:]
			delete(b.bitmaps, oldest)
		}
		b.order = append(b.order, key)
	}
	b.bitmaps[key] = bmp
}

// GetIcon 获取道具或职业图标位图；未命中时异步解码并在完成后通知窗口重绘。
func (b *BitmapCache) GetIcon(svc *gamedata.Service, win *mygo.Window, iconID int, isHQ bool) *ui.Bitmap {
	if svc == nil || iconID <= 0 {
		return nil
	}
	key := fmt.Sprintf("icon:%d:%v", iconID, isHQ)
	b.mu.Lock()
	if bmp, ok := b.bitmaps[key]; ok {
		b.mu.Unlock()
		return bmp
	}
	if b.missing[key] || b.pending[key] {
		b.mu.Unlock()
		return nil
	}
	b.pending[key] = true
	b.mu.Unlock()

	if win == nil {
		// 无窗口测试模式：避免后台 goroutine 竞争，直接跳过位图解码
		b.mu.Lock()
		delete(b.pending, key)
		b.mu.Unlock()
		return nil
	}

	go func() {
		b.sem <- struct{}{}
		img, err := svc.GetIconImage(iconID, isHQ)
		<-b.sem

		var bmp *ui.Bitmap
		if err == nil && img != nil {
			bmp = ui.NewBitmap(img)
		}
		b.put(key, bmp)
		win.Update(func() {})
	}()

	return nil
}

// GetMap 获取游戏地图贴图位图；未命中时异步解码并在完成后通知窗口重绘。
func (b *BitmapCache) GetMap(svc *gamedata.Service, win *mygo.Window, mapPath string) (*ui.Bitmap, bool) {
	if svc == nil || mapPath == "" {
		return nil, false
	}
	key := "map:" + mapPath
	b.mu.Lock()
	if bmp, ok := b.bitmaps[key]; ok {
		b.mu.Unlock()
		return bmp, false
	}
	if b.missing[key] {
		b.mu.Unlock()
		return nil, false
	}
	if b.pending[key] {
		b.mu.Unlock()
		return nil, true
	}
	b.pending[key] = true
	b.mu.Unlock()

	if win == nil {
		b.mu.Lock()
		delete(b.pending, key)
		b.mu.Unlock()
		return nil, false
	}

	go func() {
		b.sem <- struct{}{}
		img, err := svc.GetMapImage(mapPath)
		<-b.sem

		var bmp *ui.Bitmap
		if err == nil && img != nil {
			bmp = ui.NewBitmap(img)
		}
		b.put(key, bmp)
		win.Update(func() {})
	}()

	return nil, true
}

// drawGameIcon 绘制指定尺寸的游戏图标；未加载完成时显示柔和微灰占位块。
func (a *App) drawGameIcon(c *ui.Context, iconID int, isHQ bool, size float32, radius float32) {
	t := themeOf(c)
	bmp := a.bitmaps.GetIcon(a.service, a.win, iconID, isHQ)
	box := ui.Box(c).Size(size, size).Shrink(0).Radius(radius).Background(t.card).Border(hairline, t.border).Clip().Center()
	if bmp != nil {
		box.Children(func() {
			ui.Image(c, bmp).Size(size-2, size-2).Fit(ui.Contain)
		})
	}
}
