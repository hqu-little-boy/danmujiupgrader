package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

func testDownloadSpeed(url string) DownloadSpeedResult {
	testURL := fmt.Sprintf("%stest.bin", url)

	// 重试2次以避免短期网络波动
	maxRetries := 2
	var result DownloadSpeedResult

	for attempt := 0; attempt <= maxRetries; attempt++ {
		log.Printf("Testing download speed for URL: %s (attempt %d/%d)", testURL, attempt+1, maxRetries+1)

		result = DownloadSpeedResult{URL: url} // 为每次尝试重置结果

		// 为整个操作创建一个10秒超时的上下文
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

		// 创建具有适当配置的HTTP客户端
		client := createHTTPClient()

		// 使用适当的头和上下文创建请求
		req, err := http.NewRequestWithContext(ctx, "GET", testURL, nil)
		if err != nil {
			cancel() // 取消上下文
			log.Printf("Failed to create request for %s: %v", testURL, err)
			result.Error = err
			if attempt == maxRetries {
				return result // 在重试结束后返回错误
			}
			// 稍等片刻后重试
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// 添加头以使请求看起来更像常规浏览器请求
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Go-http-client/1.1)")
		req.Header.Set("Accept", "*/*")

		// 发起HTTP请求
		resp, err := client.Do(req)
		if err != nil {
			cancel() // 取消上下文
			log.Printf("Failed to make request to %s: %v (attempt %d/%d)", testURL, err, attempt+1, maxRetries+1)
			result.Error = err
			if attempt == maxRetries {
				return result // 在重试结束后返回错误
			}
			// 稍等片刻后重试
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// 正确处理响应体的关闭和上下文取消
		func() {
			defer cancel() // 完成时取消上下文
			defer func(Body io.ReadCloser) {
				err := Body.Close()
				if err != nil {
					log.Printf("关闭响应体时出错: %v", err)
				}
			}(resp.Body)

			log.Printf("Successfully connected to %s, status: %d", testURL, resp.StatusCode)

			if resp.StatusCode != http.StatusOK {
				err := fmt.Errorf("HTTP request failed with status: %d", resp.StatusCode)
				log.Printf("Error: %v (attempt %d/%d)", err, attempt+1, maxRetries+1)
				result.Error = err
				if attempt == maxRetries {
					return // 在重试结束后返回错误
				}
				// 稍等片刻后重试
				time.Sleep(500 * time.Millisecond)
				return
			}

			// 测量下载速度
			result = measureDownloadSpeed(resp, testURL, result, ctx)

			log.Printf("Received result for %s: speed=%.2f, error=%v", url, result.Speed, result.Error)
		}()

		// 根据结果检查是否应该重试
		if result.Error != nil && attempt < maxRetries {
			// 如果在measureDownloadSpeed期间发生错误且可以重试
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// 如果到达这里，说明成功或最终失败
		return result
	}

	return result
}

func createHTTPClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// 允许最多10次重定向
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			// 为重定向更新User-Agent
			req.Header.Add("User-Agent", "Mozilla/5.0 (compatible; Go-http-client/1.1)")
			return nil
		},
	}
}

func measureDownloadSpeed(resp *http.Response, testURL string, result DownloadSpeedResult, ctx context.Context) DownloadSpeedResult {
	// 开始测量时间
	startTime := time.Now()
	bytesDownloaded := int64(0)

	// 用于信号完成的通道
	done := make(chan error, 1)

	// 启动一个goroutine来读取响应体
	go func() {
		buffer := make([]byte, 32*1024) // 32KB缓冲区
		for {
			select {
			case <-ctx.Done():
				// 上下文被取消（达到超时）
				done <- nil
				return
			default:
				// 尝试读取更多数据
				n, err := resp.Body.Read(buffer)
				if n > 0 {
					bytesDownloaded += int64(n)
				}
				if err != nil {
					if err == io.EOF {
						// 完成整个文件的读取
						done <- err
						return
					} else {
						// 发生其他错误
						log.Printf("从 %s 读取时出错: %v", testURL, err)
						done <- err
						return
					}
				}
			}
		}
	}()

	// 等待超时或完成
	select {
	case <-ctx.Done():
		// 上下文被取消（达到超时）
		elapsed := time.Since(startTime)
		seconds := elapsed.Seconds()
		if seconds <= 0 {
			seconds = 0.001 // 避免除零
		}
		speed := float64(bytesDownloaded) / seconds
		log.Printf("达到 %s 的超时，下载了 %d 字节，耗时 %.2f 秒，速度: %.2f 字节/秒",
			testURL, bytesDownloaded, seconds, speed)
		result.Speed = speed
	case err := <-done:
		if err == io.EOF {
			// 完成整个文件的读取
			elapsed := time.Since(startTime)
			seconds := elapsed.Seconds()
			if seconds <= 0 {
				seconds = 0.001 // 避免除零
			}
			speed := float64(bytesDownloaded) / seconds
			log.Printf("完成从 %s 的下载，总共 %d 字节，耗时 %.2f 秒，速度: %.2f 字节/秒",
				testURL, bytesDownloaded, seconds, speed)
			result.Speed = speed
		} else if err != nil {
			log.Printf("从 %s 下载时出错: %v", testURL, err)
			result.Error = err
		}
		// 如果err为nil，则表示上下文被取消（超时情况已在上面处理）
	}

	return result
}
