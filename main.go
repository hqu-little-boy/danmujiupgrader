package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type UpdateResponse struct {
	Version string   `json:"version"`
	Date    string   `json:"date"`
	Changes []string `json:"changes"`
	URL     []string `json:"url"`
	Setup   string   `json:"setup"`
	Convert string   `json:"convert"`
}

var Version string

func main() {
	log.Printf("更新程序版本：%v", Version)
	log.Println("正在查询版本信息")

	// Try primary URL first, then fallback to secondary URL
	primaryURL := "https://gitee.com/hqu_little_boy/danmu-version/raw/master/BilibiliDanmuRobot2BiliBiliLiveRobot.json"
	secondaryURL := "https://bilibililiverobot.21645851.xyz/BilibiliDanmuRobot2BiliBiliLiveRobot.json"

	var resp *http.Response
	var err error

	resp, err = http.Get(primaryURL)
	if err != nil {
		log.Println("连接主版本服务器错误，尝试备用服务器")
		log.Println("Error:", err)

		// Try secondary URL
		resp, err = http.Get(secondaryURL)
		if err != nil {
			log.Println("连接备用版本服务器也失败")
			log.Println("Error:", err)
			return
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		updateResp := &UpdateResponse{}
		err := json.NewDecoder(resp.Body).Decode(updateResp)
		if err != nil {
			log.Println("版本信息解析失败")
			log.Println("Error decoding JSON response:", err)
			return
		}

		log.Printf("获取到版本信息：版本 %s, 发布日期 %s", updateResp.Version, updateResp.Date)
		log.Printf("更新内容: %v", updateResp.Changes)

		// Try to construct download link using the first available URL
		var downloadLink string
		for _, baseURL := range updateResp.URL {
			downloadLink = baseURL + updateResp.Convert
			log.Printf("尝试下载链接: %s", downloadLink)

			err = downloadAndExtract(downloadLink)
			if err != nil {
				log.Printf("从 %s 下载失败，尝试下一个URL", baseURL)
				continue
			}

			log.Println("下载和解压成功")
			break
		}

		if err != nil {
			log.Println("所有下载源都失败了")
			log.Println("Error:", err)
			return
		}

		log.Println("弹幕机更新完成即将启动")
		cmd := exec.Command("cmd.exe", "/C", "start", "GUI-BilibiliDanmuRobot.exe")
		if err := cmd.Start(); err != nil {
			log.Println("启动弹幕机失败，请手动启动")
			log.Println(err)
		}
		log.Println("更新完成即将退出更新程序")
	} else {
		log.Println("更新服务器链接失败")
		log.Printf("Request failed with status code: %d\n", resp.StatusCode)
	}

	time.Sleep(10 * time.Second)
	log.Println("upgrade exit")
	os.Exit(0)
}

func downloadAndExtract(link string) error {
	resp, err := http.Get(link)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Println("弹幕机下载失败")
		return err
	}
	log.Println("弹幕机下载成功，正在解压")
	zipReader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return err
	}
	log.Println("解压成功，正在更新软件")
	for _, file := range zipReader.File {
		if filepath.Base(file.Name) == "GUI-BilibiliDanmuRobot.exe" {
			zippedFile, err := file.Open()
			if err != nil {
				return err
			}
			defer zippedFile.Close()

			extractedFile, err := os.OpenFile(filepath.Base(file.Name), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
			if err != nil {
				return err
			}
			defer extractedFile.Close()

			_, err = io.Copy(extractedFile, zippedFile)
			if err != nil {
				return err
			}
			break // 只提取第一个匹配的文件
		}
	}

	return nil
}
