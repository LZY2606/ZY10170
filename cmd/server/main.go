// Command server 启动“声纹校形台”本地服务。
//
// 用法:
//
//	go run ./cmd/server --listen 127.0.0.1:5510
//
// 首次启动若数据库为空, 自动导入内置固定 fixture。
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"voiceprintbench/internal/fixture"
	"voiceprintbench/internal/httpapi"
	"voiceprintbench/internal/store"

	_ "modernc.org/sqlite"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:5510", "监听地址")
	dbPath := flag.String("db", "voiceprintbench.db", "SQLite 数据库文件")
	flag.Parse()

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer st.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	importedAt, err := st.Meta(ctx, "imported_at")
	cancel()
	if err != nil && err != sql.ErrNoRows {
		log.Fatalf("读取导入状态失败: %v", err)
	}
	if importedAt == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := st.ResetAndImport(ctx, fixture.Build()); err != nil {
			cancel()
			log.Fatalf("导入 fixture 失败: %v", err)
		}
		cancel()
		log.Printf("已导入固定 fixture -> %s", *dbPath)
	}

	srv := httpapi.New(st)
	addr := *listen
	fmt.Fprintf(os.Stderr, "声纹校形台 已启动: http://%s\n", addr)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
