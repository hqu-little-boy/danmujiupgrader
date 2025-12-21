package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
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

type DownloadSpeedResult struct {
	URL   string
	Speed float64 // bytes per second
	Error error
}

func downloadFile(url, filename string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status code: %d", resp.StatusCode)
	}

	out, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func testDownloadSpeed(url string) DownloadSpeedResult {
	result := DownloadSpeedResult{URL: url}

	testURL := url + "test.bin"

	// Create a channel to receive the result
	resultChan := make(chan DownloadSpeedResult, 1)

	// Start download in a goroutine
	go func() {
		startTime := time.Now()

		client := &http.Client{
			Timeout: 10 * time.Second, // Maximum 10 seconds as specified
		}

		resp, err := client.Get(testURL)
		if err != nil {
			result.Error = err
			resultChan <- result
			return
		}
		defer resp.Body.Close()

		// Calculate download time
		downloadTime := time.Since(startTime)

		// Read response body to measure actual download
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			result.Error = err
			resultChan <- result
			return
		}

		// Calculate speed in bytes per second
		size := len(data)
		seconds := downloadTime.Seconds()
		if seconds == 0 {
			seconds = 0.001 // Avoid division by zero
		}
		speed := float64(size) / seconds

		result.Speed = speed
		resultChan <- result
	}()

	// Wait for result or timeout after 10 seconds
	select {
	case res := <-resultChan:
		return res
	case <-time.After(10 * time.Second):
		return DownloadSpeedResult{
			URL:   url,
			Speed: 0,
			Error: fmt.Errorf("download timed out after 10 seconds"),
		}
	}
}

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

		log.Println("开始并发测试各源下载速度...")

		// Concurrently test download speeds of test.bin from each URL
		resultsChan := make(chan DownloadSpeedResult, len(updateResp.URL))

		// Start a goroutine for each URL to test download speed
		for _, baseURL := range updateResp.URL {
			go func(url string) {
				result := testDownloadSpeed(url)
				resultsChan <- result
			}(baseURL)
		}

		// Collect results
		speedResults := make([]DownloadSpeedResult, 0, len(updateResp.URL))
		timeout := time.After(10 * time.Second) // Overall timeout of 10 seconds
		completedSources := 0

		// Wait for all goroutines to complete or timeout
		for completedSources < len(updateResp.URL) {
			select {
			case result := <-resultsChan:
				speedResults = append(speedResults, result)
				completedSources++
				if result.Error != nil {
					log.Printf("源 %s 测试结果: 失败 - %v", result.URL, result.Error)
				} else {
					log.Printf("源 %s 测试结果: %.2f bytes/sec", result.URL, result.Speed)
				}
			case <-timeout:
				log.Println("达到10秒超时，停止测试")
				// Even if timeout, collect any results we got so far
				for {
					select {
					case result := <-resultsChan:
						speedResults = append(speedResults, result)
						completedSources++
						if result.Error != nil {
							log.Printf("源 %s 测试结果: 失败 - %v", result.URL, result.Error)
						} else {
							log.Printf("源 %s 测试结果: %.2f bytes/sec", result.URL, result.Speed)
						}
					default:
						goto finishTesting
					}
				}
			}
		}

	finishTesting:
		// Print all download speeds for debugging
		log.Println("--- 所有源下载速度汇总 ---")
		for _, result := range speedResults {
			if result.Error != nil {
				log.Printf("源 %s: 失败 - %v", result.URL, result.Error)
			} else {
				log.Printf("源 %s: %.2f bytes/sec", result.URL, result.Speed)
			}
		}
		log.Println("------------------------")

		// Find the fastest source among successful downloads
		fastestSource := ""
		maxSpeed := float64(0)
		for _, result := range speedResults {
			if result.Error == nil && result.Speed > maxSpeed {
				maxSpeed = result.Speed
				fastestSource = result.URL
			}
		}

		if fastestSource == "" {
			log.Println("所有源都无法正常下载test.bin，使用第一个源进行尝试")
			fastestSource = updateResp.URL[0]
		} else {
			log.Printf("最快源为: %s, 速度: %.2f bytes/sec", fastestSource, maxSpeed)
		}

		// Download both setup and convert files from the fastest source in parallel
		setupLink := fastestSource + updateResp.Setup
		convertLink := fastestSource + updateResp.Convert

		log.Printf("从最快源并行下载安装文件: %s", setupLink)
		log.Printf("从最快源并行下载转换文件: %s", convertLink)

		// Create channels to receive results
		setupResultChan := make(chan error, 1)
		convertResultChan := make(chan error, 1)

		// Download setup file in a goroutine
		go func() {
			err := downloadFile(setupLink, filepath.Base(updateResp.Setup))
			setupResultChan <- err
		}()

		// Download convert file in a goroutine
		go func() {
			err := downloadAndExtract(convertLink)
			convertResultChan <- err
		}()

		// Wait for both downloads to complete and handle errors
		setupErr := <-setupResultChan
		convertErr := <-convertResultChan

		// Handle setup file download error
		if setupErr != nil {
			log.Println("从最快源下载安装文件失败")
			log.Println("Error:", setupErr)

			// Try other sources if the fastest one failed
			for _, baseURL := range updateResp.URL {
				if baseURL == fastestSource {
					continue // Skip the one we already tried
				}

				setupLink = baseURL + updateResp.Setup
				log.Printf("尝试备用源下载安装文件: %s", setupLink)

				setupErr = downloadFile(setupLink, filepath.Base(updateResp.Setup))
				if setupErr == nil {
					log.Println("备用源下载安装文件成功")
					break
				}
			}

			if setupErr != nil {
				log.Println("所有下载源都未能下载安装文件")
				log.Println("Error:", setupErr)
				return
			}
		}

		// Handle convert file download error
		if convertErr != nil {
			log.Println("从最快源下载转换文件失败")
			log.Println("Error:", convertErr)

			// Try other sources if the fastest one failed
			for _, baseURL := range updateResp.URL {
				if baseURL == fastestSource {
					continue // Skip the one we already tried
				}

				convertLink = baseURL + updateResp.Convert
				log.Printf("尝试备用源下载转换文件: %s", convertLink)

				convertErr = downloadAndExtract(convertLink)
				if convertErr == nil {
					log.Println("备用源下载转换文件成功")
					break
				}
			}

			if convertErr != nil {
				log.Println("所有下载源都未能下载转换文件")
				log.Println("Error:", convertErr)
				return
			}
		}

		log.Println("安装文件和转换文件并行下载完成")

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
