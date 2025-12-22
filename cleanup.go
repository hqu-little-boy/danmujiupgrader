package main

import (
	"log"
	"os"
	"path/filepath"
)

func cleanupDownloadedFiles(updateResp *UpdateResponse) {
	// 清理下载的安装和转换文件
	if updateResp != nil {
		setupFileName := filepath.Base(updateResp.Setup)
		convertFileName := filepath.Base(updateResp.Convert)

		// 尝试删除安装文件
		if err := os.Remove(setupFileName); err != nil {
			log.Printf("删除安装文件失败 %s: %v", setupFileName, err)
		} else {
			log.Printf("已删除安装文件: %s", setupFileName)
		}

		// 尝试删除转换文件
		if err := os.Remove(convertFileName); err != nil {
			log.Printf("删除转换文件失败 %s: %v", convertFileName, err)
		} else {
			log.Printf("已删除转换文件: %s", convertFileName)
		}
	}
}
