package store

import (
	"encoding/json"
	"fmt"
	"os"
)

// InstallInfo 记录"pk 自己是怎么装进来的"（由 install.sh / install.ps1 写入
// state/install.json）。`pk uninstall --all` 用它给出**精确**的自我删除命令，
// 而不是猜 ~/.local/bin 之类。
type InstallInfo struct {
	Schema   int    `json:"schema"`
	Channel  string `json:"channel"`  // script / manual
	Platform string `json:"platform"` // darwin-arm64 …
	Version  string `json:"version"`
	Dir      string `json:"dir"` // 二进制所在目录
}

// LoadInstallInfo 读取安装来源记录；没有或损坏时返回 nil（不是错误）。
func LoadInstallInfo(path string) *InstallInfo {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var info InstallInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil
	}
	return &info
}

// Save 原子写记录。
func (i *InstallInfo) Save(path string) error {
	if i.Schema == 0 {
		i.Schema = 1
	}
	data, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化安装记录失败: %w", err)
	}
	return WriteAtomic(path, data)
}
