package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

func performUpdate(updateResp *UpdateResponse) error {
	log.Printf("获取到版本信息：版本 %s, 发布日期 %s", updateResp.Version, updateResp.Date)
	log.Printf("更新内容: %v", updateResp.Changes)

	fastestSource, err := findFastestSource(updateResp.URL)
	if err != nil {
		return err
	}

	err = downloadAndExecuteFiles(updateResp, fastestSource)
	if err != nil {
		return err
	}

	log.Println("安装文件和转换文件并行下载完成")
	log.Println("更新完成即将退出更新程序")

	return nil
}

func findFastestSource(urls []string) (string, error) {
	log.Println("开始并发测试各源下载速度...")

	// 并发测试每个URL的test.bin下载速度
	resultsChan := make(chan DownloadSpeedResult, len(urls))

	// 为每个URL启动一个goroutine来测试下载速度
	for _, baseURL := range urls {
		go func(url string) {
			result := testDownloadSpeed(url)
			resultsChan <- result
		}(baseURL)
	}

	// 收集结果 - 给更多时间来处理各个超时
	speedResults := make([]DownloadSpeedResult, 0, len(urls))
	timeout := time.After(15 * time.Second) // 总体超时15秒，以处理各个10秒超时
	completedSources := 0

	// 等待所有goroutine完成或总体超时
	for completedSources < len(urls) {
		select {
		case result := <-resultsChan:
			speedResults = append(speedResults, result)
			completedSources++
			if result.Error != nil {
				log.Printf("源 %s 测试结果: 失败 - %v", result.URL, result.Error)
			}
		case <-timeout:
			log.Println("达到总体超时，停止测试")
			// 收集可能仍会传入的剩余结果
			remainingTimeout := time.After(2 * time.Second) // 再给2秒时间处理剩余结果
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
	// 打印所有下载速度用于调试
	log.Println("--- 所有源下载速度汇总 ---")
	for _, result := range speedResults {
		if result.Error != nil {
			log.Printf("源 %s: 失败 - %v", result.URL, result.Error)
		} else {
			log.Printf("源 %s: %.2f bytes/sec", result.URL, result.Speed)
		}
	}
	log.Println("------------------------")

	// 在成功下载中找到最快的源
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
		fastestSource = urls[0]
	} else {
		log.Printf("最快源为: %s, 速度: %.2f bytes/sec", fastestSource, maxSpeed)
	}

	return fastestSource, nil
}

func downloadAndExecuteFiles(updateResp *UpdateResponse, fastestSource string) error {
	// Download both setup and convert files from the fastest source in parallel
	setupLink := fmt.Sprintf("%s%s", fastestSource, updateResp.Setup)
	convertLink := fmt.Sprintf("%s%s", fastestSource, updateResp.Convert)

	log.Printf("从最快源并行下载安装文件: %s", setupLink)
	log.Printf("从最快源并行下载转换文件: %s", convertLink)

	// Create channels to receive results
	setupResultChan := make(chan error, 1)
	convertResultChan := make(chan error, 1)

	// Download and execute setup file in a goroutine
	go func() {
		err := downloadAndExecuteSetupFile(updateResp, setupLink)
		setupResultChan <- err
	}()

	// Download and execute convert file in a goroutine
	go func() {
		err := downloadAndExecuteConvertFile(updateResp, convertLink)
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
		setupSuccess := false
		for _, baseURL := range updateResp.URL {
			if baseURL == fastestSource {
				continue // Skip the one we already tried
			}

			setupLink = fmt.Sprintf("%s%s", baseURL, updateResp.Setup)
			log.Printf("尝试备用源下载安装文件: %s", setupLink)

			err := downloadAndExecuteSetupFile(updateResp, setupLink)
			if err == nil {
				log.Println("备用源下载安装文件成功")
				setupSuccess = true
				break
			} else {
				log.Printf("备用源尝试失败: %v", err)
				// Continue to next source
			}
		}

		if !setupSuccess {
			log.Println("所有下载源都未能下载安装文件")
			log.Println("Error:", setupErr)
			return setupErr
		}
	}

	// Handle convert file download error
	if convertErr != nil {
		log.Println("从最快源下载转换文件失败")
		log.Println("Error:", convertErr)

		// Try other sources if the fastest one failed
		convertSuccess := false
		for _, baseURL := range updateResp.URL {
			if baseURL == fastestSource {
				continue // Skip the one we already tried
			}

			convertLink = fmt.Sprintf("%s%s", baseURL, updateResp.Convert)
			log.Printf("尝试备用源下载转换文件: %s", convertLink)

			err := downloadAndExecuteConvertFile(updateResp, convertLink)
			if err == nil {
				log.Println("备用源下载转换文件成功")
				convertSuccess = true
				break
			} else {
				log.Printf("备用源尝试失败: %v", err)
				// Continue to next source
			}
		}

		if !convertSuccess {
			log.Println("所有下载源都未能下载转换文件")
			log.Println("Error:", convertErr)
			return convertErr
		}
	}

	return nil
}

func downloadAndExecuteSetupFile(updateResp *UpdateResponse, setupLink string) error {
	setupFileName := filepath.Base(updateResp.Setup)
	err := downloadFileWithProgress(setupLink, setupFileName, true) // Show progress
	if err != nil {
		return err
	}

	// 获取当前工作目录作为安装路径
	currentDir, err := os.Getwd()
	if err != nil {
		log.Printf("获取当前目录失败: %v", err)
		// 如果无法获取当前目录，则使用默认路径
		currentDir = "."
	}

	// 使用InstallPath参数执行下载的安装文件
	log.Printf("正在执行安装程序: %s /InstallPath=%s", setupFileName, currentDir)

	cmd := exec.Command(fmt.Sprintf("./%s", setupFileName))
	cmd.Args = append(cmd.Args, fmt.Sprintf("/InstallPath=%s", currentDir))
	if err := cmd.Run(); err != nil {
		log.Printf("执行安装程序失败: %v", err)

		// Check if this was a user cancellation
		if isUserCancellation(err) {
			log.Println("检测到用户取消操作，程序将正常退出")
			// Exit normally when user cancels
			os.Exit(0)
		}

		// Always return the execution error, regardless of isBackup
		// The calling code will handle whether to continue or break
		return err
	} else {
		log.Printf("安装程序执行完成: %s", setupFileName)
	}

	return nil
}

func downloadAndExecuteConvertFile(updateResp *UpdateResponse, convertLink string) error {
	convertFileName := filepath.Base(updateResp.Convert)
	err := downloadFileWithProgress(convertLink, convertFileName, true) // Show progress
	if err != nil {
		return err
	}

	// 执行下载的转换文件
	log.Printf("正在执行转换文件: %s", convertFileName)

	cmd := exec.Command(fmt.Sprintf("./%s", convertFileName))
	if err := cmd.Run(); err != nil {
		log.Printf("执行转换文件失败: %v", err)

		// Check if this was a user cancellation
		if isUserCancellation(err) {
			log.Println("检测到用户取消操作，程序将正常退出")
			// Exit normally when user cancels
			os.Exit(0)
		}

		// Always return the execution error
		return err
	} else {
		log.Printf("转换文件执行完成: %s", convertFileName)
	}

	return nil
}

func isUserCancellation(err error) bool {
	if exitError, ok := err.(*exec.ExitError); ok {
		// Check if the process was terminated by a signal (like SIGINT/Ctrl+C)
		if status, ok := exitError.Sys().(syscall.WaitStatus); ok {
			// SIGINT (2) is typically sent by Ctrl+C
			// SIGTERM (15) is another common termination signal
			if status.Signal() == syscall.SIGINT || status.Signal() == syscall.SIGTERM {
				return true
			}
		}
		// Check if exit code is 2, which is commonly used for user cancellation
		return exitError.ExitCode() == 2
	}
	return false
}

func getUpdateInfo() (*UpdateResponse, error) {
	// 先尝试主URL，然后回退到备用URL
	primaryURL := "https://gitee.com/hqu_little_boy/danmu-version/raw/master/BilibiliDanmuRobot2BiliBiliLiveRobot.json"
	secondaryURL := "https://bilibililiverobot.21645851.xyz/BilibiliDanmuRobot2BiliBiliLiveRobot.json"

	var resp *http.Response
	var err error

	resp, err = http.Get(primaryURL)
	if err != nil {
		log.Println("连接主版本服务器错误，尝试备用服务器")
		log.Println("Error:", err)

		// 尝试备用URL
		resp, err = http.Get(secondaryURL)
		if err != nil {
			log.Println("连接备用版本服务器也失败")
			log.Println("Error:", err)
			return nil, err
		}
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Printf("关闭响应体时出错: %v", err)
		}
	}(resp.Body)

	if resp.StatusCode == http.StatusOK {
		updateResp := &UpdateResponse{}
		err := json.NewDecoder(resp.Body).Decode(updateResp)
		if err != nil {
			log.Println("版本信息解析失败")
			log.Println("Error decoding JSON response:", err)
			return nil, err
		}
		return updateResp, nil
	} else {
		log.Println("更新服务器链接失败")
		log.Printf("Request failed with status code: %d\n", resp.StatusCode)
		return nil, fmt.Errorf("request failed with status code: %d", resp.StatusCode)
	}
}
