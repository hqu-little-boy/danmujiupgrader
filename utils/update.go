package utils

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
)

func PerformUpdate(updateResp *UpdateResponse) error {
	log.Printf("获取到版本信息：版本 %s, 发布日期 %s", updateResp.Version, updateResp.Date)
	log.Printf("更新内容: %v", updateResp.Changes)

	// Use the first source as default
	if len(updateResp.URL) == 0 {
		return fmt.Errorf("no download sources available")
	}
	firstSource := updateResp.URL[0]

	err := downloadAndExecuteFiles(updateResp, firstSource)
	if err != nil {
		return err
	}

	log.Println("安装文件和转换文件并行下载完成")
	log.Println("更新完成即将退出更新程序")

	return nil
}

func downloadAndExecuteFiles(updateResp *UpdateResponse, firstSource string) error {
	// Download both setup and convert files from the first source in parallel
	setupLink := fmt.Sprintf("%s%s", firstSource, updateResp.Setup)
	convertLink := fmt.Sprintf("%s%s", firstSource, updateResp.Convert)

	log.Printf("从首个源并行下载安装文件: %s", setupLink)
	log.Printf("从首个源并行下载转换文件: %s", convertLink)

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
		log.Println("从首个源下载安装文件失败")
		log.Println("Error:", setupErr)

		// Try other sources if the first one failed
		setupSuccess := false
		for _, baseURL := range updateResp.URL {
			if baseURL == firstSource {
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
		log.Println("从首个源下载转换文件失败")
		log.Println("Error:", convertErr)

		// Try other sources if the first one failed
		convertSuccess := false
		for _, baseURL := range updateResp.URL {
			if baseURL == firstSource {
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

func GetUpdateInfo() (*UpdateResponse, error) {
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
