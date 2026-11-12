package env

import "strings"

// OnSyncDir 判断路径是否落在常见的云同步目录里（OneDrive / Dropbox / iCloud / Google Drive / 坚果云）。
//
// 为什么值得单独报警：同步客户端会在后台改写/回滚文件，`flock` 在同步盘上常常形同虚设、
// 原子 rename 也可能被同步进程抢成半成品 —— 把 PK_ROOT 放进去会偶发"锁不住""台账回滚成旧版本"。
func OnSyncDir(path string) bool {
	// 按路径段比对，避免 "onedrive-backup-local" 这种自家目录被误判
	segs := strings.Split(strings.ToLower(strings.ReplaceAll(path, `\`, "/")), "/")
	for _, seg := range segs {
		for _, name := range []string{"onedrive", "dropbox", "icloud", "mobile documents",
			"google drive", "googledrive", "nutstore", "坚果云", "百度网盘"} {
			if seg == name || strings.HasPrefix(seg, name+" ") { // 真实目录常带后缀，如 "OneDrive - Contoso"
				return true
			}
		}
	}
	return false
}
