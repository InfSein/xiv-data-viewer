package main

import (
	"embed"
	"io/fs"
)

// 将 Git Submodule (xivdev/EXDSchema) 中的全部 YAML 字段定义直接嵌入单文件客户端中。
//
//go:embed schemas/*.yml
var embeddedSchemasFS embed.FS

func getSchemasFS() fs.FS {
	sub, err := fs.Sub(embeddedSchemasFS, "schemas")
	if err != nil {
		return embeddedSchemasFS
	}
	return sub
}
