package main

import (
	"flag"
	"log"
	"net/http"

	"chainresearch/internal/api"
	"chainresearch/internal/chain"
	"chainresearch/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP 监听地址")
	dataDir := flag.String("data", "./data", "索引数据目录")
	flag.Parse()

	s, err := store.Open(*dataDir)
	if err != nil {
		log.Fatalf("打开存储失败: %v", err)
	}
	// 首次启动且存储为空时不自动建版本；由导入触发版本生成。
	// 但若历史上无快照而已有区块（数据文件被外部放入），补建一次。
	if len(s.AllBlocks()) > 0 {
		if _, ok := s.LatestSnapshot(); !ok {
			snap := chain.BuildSnapshot(s.AllBlocks(), 1, "启动补建")
			if err := s.SaveSnapshot(snap); err != nil {
				log.Fatalf("补建索引失败: %v", err)
			}
		}
	}

	srv := api.New(s)
	log.Printf("链上数据研究服务监听 %s（数据目录 %s，未连接任何公网节点）", *addr, *dataDir)
	if err := http.ListenAndServe(*addr, srv.Mux); err != nil {
		log.Fatal(err)
	}
}
