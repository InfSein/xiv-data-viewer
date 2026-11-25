//go:build darwin

package i18n

import (
	"os/exec"
	"strings"
)

// detectSystemLocaleTags 读取 macOS 全局 AppleLanguages 设置及环境变量回退。
func detectSystemLocaleTags() []string {
	var tags []string
	if out, err := exec.Command("defaults", "read", "-g", "AppleLanguages").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || line == "(" || line == ")" {
				continue
			}
			line = strings.TrimSuffix(line, ",")
			line = strings.Trim(line, `"' `)
			if line != "" {
				tags = append(tags, line)
			}
		}
	}
	tags = append(tags, envLocaleTags()...)
	return tags
}
