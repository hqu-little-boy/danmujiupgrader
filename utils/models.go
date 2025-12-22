package utils

type UpdateResponse struct {
	Version string   `json:"version"`
	Date    string   `json:"date"`
	Changes []string `json:"changes"`
	URL     []string `json:"url"`
	Setup   string   `json:"setup"`
	Convert string   `json:"convert"`
}

type DownloadSpeedResult struct {
	URL   string
	Speed float64 // 字节每秒
	Error error
}
