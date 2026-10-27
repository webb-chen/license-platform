// Package web 通过 go:embed 内嵌管理界面静态资源。
package web

import "embed"

// FS 是编译进单二进制的前端资源。
//
//go:embed index.html assets
var FS embed.FS
