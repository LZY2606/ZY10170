package server

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web_assets
var staticFS embed.FS

func staticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "web_assets")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}
