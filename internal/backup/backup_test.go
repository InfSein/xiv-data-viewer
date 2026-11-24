package backup

import (
	"path/filepath"
	"testing"

	"github.com/InfSein/xiv-data-viewer/internal/config"
	"github.com/InfSein/xiv-data-viewer/internal/gamedata"
)

func TestVersionBackupAndDiff(t *testing.T) {
	cfg := config.LoadAppConfigFromEnv(filepath.Join("..", "..", ".env"))
	resolved, err := config.ValidateAndResolve(cfg)
	if err != nil {
		t.Skipf("本地未配置可用 FF14 客户端，跳过版本快照对比测试: %v", err)
	}

	store, err := gamedata.LoadGameDataStore(resolved)
	if err != nil {
		t.Fatalf("加载数据失败: %v", err)
	}
	defer store.Close()

	svc := NewService(store, t.TempDir())
	status, err := svc.InitializeOnStartup()
	if err != nil {
		t.Fatalf("初始化快照失败: %v", err)
	}
	if len(status.Backups) < 2 {
		t.Fatalf("期望至少包含当前版与历史基准快照，实际 %d 个", len(status.Backups))
	}

	diff, err := svc.CompareVersions(VersionDiffQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("对比版本差异失败: %v", err)
	}
	if diff.Summary.AddedItems == 0 || diff.TotalFiltered == 0 {
		t.Fatalf("版本差异结果为空: %+v", diff.Summary)
	}
}
