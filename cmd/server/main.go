// Command voicebench runs the 声纹校形台 local service.
package main

import (
	"flag"
	"log"
	"net/http"

	"voicebench/internal/server"
	"voicebench/internal/service"
	"voicebench/internal/store"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:5510", "HTTP listen address")
	dbPath := flag.String("db", "voicebench.db", "SQLite database path")
	flag.Parse()

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer st.Close()

	n, err := st.CountSpeakers()
	if err != nil {
		log.Fatalf("count data: %v", err)
	}
	if n == 0 {
		if err := server.SeedStore(st); err != nil {
			log.Fatalf("seed fixture: %v", err)
		}
		log.Print("已导入固定 fixture")
	}

	srv := server.New(service.New(st), st)
	log.Printf("声纹校形台 listening on http://%s", *listen)
	if err := http.ListenAndServe(server.ListenAddr(*listen), srv.Mux); err != nil {
		log.Fatal(err)
	}
}
