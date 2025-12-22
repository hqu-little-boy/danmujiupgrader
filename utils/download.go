package utils

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/schollz/progressbar/v3"
)

func downloadFileWithProgress(url, filename string, showProgress bool) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Printf("关闭响应体时出错: %v", err)
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status code: %d", resp.StatusCode)
	}

	out, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer func(out *os.File) {
		err := out.Close()
		if err != nil {
			log.Printf("关闭文件 %s 时出错: %v", filename, err)
		}
	}(out)

	// Get the total size from Content-Length header
	totalSize := resp.ContentLength

	if showProgress && totalSize > 0 {
		// Create a progress bar
		bar := progressbar.DefaultBytes(
			totalSize,
			fmt.Sprintf("下载 %s", filename),
		)

		// Create a reader with progress bar
		reader := progressbar.NewReader(resp.Body, bar)

		// Copy with progress bar
		_, err = io.Copy(out, &reader)
	} else {
		_, err = io.Copy(out, resp.Body)
	}

	if err != nil {
		log.Printf("文件 %s 下载失败: %v", filename, err)
		return err
	}

	log.Printf("文件 %s 下载成功", filename)
	return err
}
