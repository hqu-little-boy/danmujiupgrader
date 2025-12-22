package main

import (
	"context"
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

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		log.Printf("文件 %s 下载失败: %v", filename, err)
		return err
	}

	log.Printf("文件 %s 下载成功", filename)
	return err
}

func testDownloadSpeed(url string) DownloadSpeedResult {
	result := DownloadSpeedResult{URL: url}

	testURL := url + "test.bin"
	log.Printf("Testing download speed for URL: %s", testURL)

	// Create a context with 10 second timeout for the entire operation
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create a client with a reasonable timeout for connection
	client := &http.Client{
		Timeout: 30 * time.Second, // Higher timeout for the client, we control timing via context
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Allow up to 10 redirects
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			// Update the User-Agent for redirects
			req.Header.Add("User-Agent", "Mozilla/5.0 (compatible; Go-http-client/1.1)")
			return nil
		},
	}

	// Create request with proper headers
	req, err := http.NewRequestWithContext(ctx, "GET", testURL, nil)
	if err != nil {
		log.Printf("Failed to create request for %s: %v", testURL, err)
		result.Error = err
		return result
	}

	// Add headers to make request look more like a regular browser request
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Go-http-client/1.1)")
	req.Header.Set("Accept", "*/*")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to make request to %s: %v", testURL, err)
		result.Error = err
		return result
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Printf("关闭响应体时出错: %v", err)
		}
	}(resp.Body)

	log.Printf("Successfully connected to %s, status: %d", testURL, resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("HTTP request failed with status: %d", resp.StatusCode)
		log.Printf("Error: %v", err)
		result.Error = err
		return result
	}

	// Start measuring time
	startTime := time.Now()
	bytesDownloaded := int64(0)

	// Channel to signal completion
	done := make(chan error, 1)

	// Start a goroutine to read the response body
	go func() {
		buffer := make([]byte, 32*1024) // 32KB buffer
		for {
			select {
			case <-ctx.Done():
				// Context was cancelled (timeout reached)
				done <- nil
				return
			default:
				// Try to read more data
				n, err := resp.Body.Read(buffer)
				if n > 0 {
					bytesDownloaded += int64(n)
				}
				if err != nil {
					if err == io.EOF {
						// Finished reading the entire file
						done <- err
						return
					} else {
						// Some other error occurred
						log.Printf("Error reading from %s: %v", testURL, err)
						done <- err
						return
					}
				}
			}
		}
	}()

	// Wait for either timeout or completion
	select {
	case <-ctx.Done():
		// Context was cancelled (timeout reached)
		elapsed := time.Since(startTime)
		seconds := elapsed.Seconds()
		if seconds <= 0 {
			seconds = 0.001 // Avoid division by zero
		}
		speed := float64(bytesDownloaded) / seconds
		log.Printf("Timeout reached for %s, downloaded %d bytes in %.2f seconds, speed: %.2f bytes/sec",
			testURL, bytesDownloaded, seconds, speed)
		result.Speed = speed
	case err := <-done:
		if err == io.EOF {
			// Finished reading the entire file
			elapsed := time.Since(startTime)
			seconds := elapsed.Seconds()
			if seconds <= 0 {
				seconds = 0.001 // Avoid division by zero
			}
			speed := float64(bytesDownloaded) / seconds
			log.Printf("Completed download from %s, total %d bytes in %.2f seconds, speed: %.2f bytes/sec",
				testURL, bytesDownloaded, seconds, speed)
			result.Speed = speed
		} else if err != nil {
			log.Printf("Error during download from %s: %v", testURL, err)
			result.Error = err
		}
		// If err is nil, it means context was cancelled (timeout case already handled above)
	}

	log.Printf("Received result for %s: speed=%.2f, error=%v", url, result.Speed, result.Error)
	return result
}

func main() {
	log.Printf("更新程序版本：%v", Version)
	log.Println("正在查询版本信息")

	// Try primary URL first, then fallback to secondary URL
	primaryURL := "https://gitee.com/hqu_little_boy/danmu-version/raw/master/BilibiliDanmuRobot2BiliBiliLiveRobot.json"
	secondaryURL := "https://bilibililiverobot.21645851.xyz/BilibiliDanmuRobot2BiliBiliLiveRobot.json"

	var resp *http.Response
	var err error
	var updateResp *UpdateResponse

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
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Printf("关闭响应体时出错: %v", err)
		}
	}(resp.Body)

	if resp.StatusCode == http.StatusOK {
		updateResp = &UpdateResponse{}
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

		// Collect results - give more time to account for the individual timeouts
		speedResults := make([]DownloadSpeedResult, 0, len(updateResp.URL))
		timeout := time.After(15 * time.Second) // Overall timeout of 15 seconds to account for individual 10s timeouts
		completedSources := 0

		// Wait for all goroutines to complete or overall timeout
		for completedSources < len(updateResp.URL) {
			select {
			case result := <-resultsChan:
				speedResults = append(speedResults, result)
				completedSources++
				if result.Error != nil {
					log.Printf("源 %s 测试结果: 失败 - %v", result.URL, result.Error)
				}
			case <-timeout:
				log.Println("达到总体超时，停止测试")
				// Collect any remaining results that might still come in
				remainingTimeout := time.After(2 * time.Second) // Give 2 more seconds for stragglers
				for {
					select {
					case result := <-resultsChan:
						speedResults = append(speedResults, result)
						completedSources++
						if result.Error != nil {
							log.Printf("源 %s 测试结果: 失败 - %v", result.URL, result.Error)
						}
					case <-remainingTimeout:
						log.Printf("最终结果收集完成，已收集 %d 个结果", len(speedResults))
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

		// Download and execute setup file in a goroutine
		go func() {
			setupFileName := filepath.Base(updateResp.Setup)
			err := downloadFile(setupLink, setupFileName)
			if err != nil {
				setupResultChan <- err
				return
			}

			// Get the current working directory for the install path
			currentDir, err := os.Getwd()
			if err != nil {
				log.Printf("获取当前目录失败: %v", err)
				// Use a default path if we can't get the current directory
				currentDir = "."
			}

			// Execute the downloaded setup file with InstallPath parameter
			installPath := currentDir
			log.Printf("正在执行安装程序: %s /InstallPath=%s", setupFileName, installPath)

			cmd := exec.Command("./"+setupFileName, "/InstallPath="+installPath)
			if err := cmd.Run(); err != nil {
				log.Printf("执行安装程序失败: %v", err)
				// Don't treat execution failure as a download error
				setupResultChan <- nil
				return
			}

			log.Printf("安装程序执行完成: %s", setupFileName)
			setupResultChan <- nil
		}()

		// Download and execute convert file in a goroutine
		go func() {
			convertFileName := filepath.Base(updateResp.Convert)
			err := downloadFile(convertLink, convertFileName)
			if err != nil {
				convertResultChan <- err
				return
			}

			// Execute the downloaded convert file
			log.Printf("正在执行转换文件: %s", convertFileName)

			cmd := exec.Command("./" + convertFileName)
			if err := cmd.Run(); err != nil {
				log.Printf("执行转换文件失败: %v", err)
				// Don't treat execution failure as a download error
				convertResultChan <- nil
				return
			}

			log.Printf("转换文件执行完成: %s", convertFileName)
			convertResultChan <- nil
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

				setupFileName := filepath.Base(updateResp.Setup)
				setupErr = downloadFile(setupLink, setupFileName)
				if setupErr == nil {
					log.Println("备用源下载安装文件成功")

					// Get the current working directory for the install path
					currentDir, err := os.Getwd()
					if err != nil {
						log.Printf("获取当前目录失败: %v", err)
						// Use a default path if we can't get the current directory
						currentDir = "."
					}

					// Execute the downloaded setup file with InstallPath parameter
					log.Printf("正在执行安装程序: %s /InstallPath=%s", setupFileName, currentDir)

					cmd := exec.Command("./"+setupFileName, "/InstallPath="+currentDir)
					if err := cmd.Run(); err != nil {
						log.Printf("执行安装程序失败: %v", err)
						// Don't treat execution failure as a download error
					} else {
						log.Printf("安装程序执行完成: %s", setupFileName)
					}
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

				convertFileName := filepath.Base(updateResp.Convert)
				convertErr = downloadFile(convertLink, convertFileName)
				if convertErr == nil {
					log.Println("备用源下载转换文件成功")

					// Execute the downloaded convert file
					log.Printf("正在执行转换文件: %s", convertFileName)

					cmd := exec.Command("./" + convertFileName)
					if err := cmd.Run(); err != nil {
						log.Printf("执行转换文件失败: %v", err)
						// Don't treat execution failure as a download error
					} else {
						log.Printf("转换文件执行完成: %s", convertFileName)
					}
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

		log.Println("更新完成即将退出更新程序")
	} else {
		log.Println("更新服务器链接失败")
		log.Printf("Request failed with status code: %d\n", resp.StatusCode)
	}

	//time.Sleep(10 * time.Second)

	// Clean up downloaded setup and convert files
	if updateResp != nil {
		setupFileName := filepath.Base(updateResp.Setup)
		convertFileName := filepath.Base(updateResp.Convert)

		// Try to delete setup file
		if err := os.Remove(setupFileName); err != nil {
			log.Printf("删除安装文件失败 %s: %v", setupFileName, err)
		} else {
			log.Printf("已删除安装文件: %s", setupFileName)
		}

		// Try to delete convert file
		if err := os.Remove(convertFileName); err != nil {
			log.Printf("删除转换文件失败 %s: %v", convertFileName, err)
		} else {
			log.Printf("已删除转换文件: %s", convertFileName)
		}
	}

	log.Println("upgrade exit")
	os.Exit(0)
}
