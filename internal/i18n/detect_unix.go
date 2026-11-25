//go:build !windows && !darwin

package i18n

// detectSystemLocaleTags 读取类 Unix 系统的语言环境变量（LANGUAGE / LC_ALL / LC_MESSAGES / LANG）。
func detectSystemLocaleTags() []string {
	return envLocaleTags()
}
