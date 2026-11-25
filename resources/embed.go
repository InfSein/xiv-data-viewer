package resources

import _ "embed"

// IconPNG 保存经过去底、居中和抗锯齿处理的 1024×1024 应用图标 PNG 数据。
//
//go:embed icon.png
var IconPNG []byte
