package main

import (
	"log"
	"os"
)

func main() {
	// 获取更新信息
	updateResp, err := getUpdateInfo()
	if err != nil {
		log.Printf("获取更新信息失败: %v", err)
		os.Exit(1)
	}

	// 执行更新过程
	err = performUpdate(updateResp)
	if err != nil {
		log.Printf("更新过程失败: %v", err)
		os.Exit(1)
	}

	// 清理下载的文件
	cleanupDownloadedFiles(updateResp)

	log.Println("upgrade exit")
	os.Exit(0)
}
