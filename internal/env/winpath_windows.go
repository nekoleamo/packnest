//go:build windows

package env

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

// Windows 的"rc 文件"其实是注册表里的用户环境变量：
//   HKCU\Environment\Path
// 写它比写 PowerShell profile 可靠得多 —— 不受 ExecutionPolicy 限制，cmd / PowerShell /
// Git Bash 都继承（用户实测：profile 里放 PATH，Restricted 策略下每个 shell 都报错）。
//
// 只碰 Path 这一项：其它变量（npm_config_registry / GOPROXY …）仍然走 profile 的托管块，
// 避免覆盖用户自己设过的同名变量。

const (
	regKeyUserEnv   = `Environment`
	regValuePathKey = `Path`
	keyRead         = 0x20019 // KEY_READ
	keyWrite        = 0x20006 // KEY_WRITE
	regSZ           = 1
	regExpandSZ     = 2
	wmSettingChange = 0x001A
	smtoAbortIfHung = 0x0002
)

var (
	advapi32               = syscall.NewLazyDLL("advapi32.dll")
	user32                 = syscall.NewLazyDLL("user32.dll")
	procRegOpenKeyEx       = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueEx    = advapi32.NewProc("RegQueryValueExW")
	procRegSetValueEx      = advapi32.NewProc("RegSetValueExW")
	procRegCloseKey        = advapi32.NewProc("RegCloseKey")
	procSendMessageTimeout = user32.NewProc("SendMessageTimeoutW")
)

// hkeyCurrentUser 是预定义句柄 HKEY_CURRENT_USER：Windows 上的真值是
// (HKEY)(ULONG_PTR)((LONG)0x80000001)，在 64 位下要**符号扩展**成 0xFFFFFFFF80000001，
// 直接传 0x80000001 会被当成别的句柄（ERROR_INVALID_HANDLE）。
const hkeyCurrentUser64 = 0xFFFFFFFF80000001

func regOpenUserEnv(access uint32) (syscall.Handle, error) {
	name, err := syscall.UTF16PtrFromString(regKeyUserEnv)
	if err != nil {
		return 0, err
	}
	var h syscall.Handle
	ret, _, _ := procRegOpenKeyEx.Call(uintptr(hkeyCurrentUser64), uintptr(unsafe.Pointer(name)), 0, uintptr(access), uintptr(unsafe.Pointer(&h)))
	if ret != 0 {
		return 0, fmt.Errorf("打开注册表 HKCU\\%s 失败（错误码 %d）", regKeyUserEnv, ret)
	}
	return h, nil
}

func regQueryString(h syscall.Handle, key string) (string, uint32, bool) {
	name, err := syscall.UTF16PtrFromString(key)
	if err != nil {
		return "", 0, false
	}
	var typ, size uint32
	// 先问长度
	ret, _, _ := procRegQueryValueEx.Call(uintptr(h), uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&typ)), 0, uintptr(unsafe.Pointer(&size)))
	if ret != 0 || size == 0 {
		return "", 0, false
	}
	buf := make([]uint16, size/2+1)
	ret, _, _ = procRegQueryValueEx.Call(uintptr(h), uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if ret != 0 {
		return "", 0, false
	}
	return syscall.UTF16ToString(buf), typ, true
}

func regSetString(h syscall.Handle, key, value string, typ uint32) error {
	name, err := syscall.UTF16PtrFromString(key)
	if err != nil {
		return err
	}
	data, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}
	ret, _, _ := procRegSetValueEx.Call(uintptr(h), uintptr(unsafe.Pointer(name)), 0, uintptr(typ),
		uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)*2))
	if ret != 0 {
		return fmt.Errorf("写注册表 HKCU\\%s\\%s 失败（错误码 %d）", regKeyUserEnv, key, ret)
	}
	return nil
}

// broadcastEnvChange 通知 Explorer「环境变量变了」，这样**新开**的窗口立刻拿到新 Path。
// 不通知也能生效（新进程直接读注册表），只是已打开的 Explorer 缓存的旧环境会带进新窗口。
func broadcastEnvChange() {
	const timeoutMs = 1000
	param, err := syscall.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	var result uintptr
	_, _, _ = procSendMessageTimeout.Call(uintptr(0xFFFF), uintptr(wmSettingChange), 0,
		uintptr(smtoAbortIfHung), uintptr(unsafe.Pointer(param)), timeoutMs, uintptr(unsafe.Pointer(&result)))
}

// AddUserPath 把 segments 追加到用户 Path（已存在的跳过）。
// 返回（是否有改动, 改动前的 Path, 改动后的 Path）。
func AddUserPath(segments []string) (bool, string, string, error) {
	h, err := regOpenUserEnv(keyRead | keyWrite)
	if err != nil {
		return false, "", "", err
	}
	defer procRegCloseKey.Call(uintptr(h))

	cur, typ, ok := regQueryString(h, regValuePathKey)
	if !ok || typ == 0 {
		typ = regExpandSZ
	}
	existing := splitPathList(cur)
	added := make([]string, 0, len(segments))
	for _, seg := range segments {
		if seg == "" {
			continue
		}
		dup := false
		for _, e := range existing {
			if samePath(e, seg) {
				dup = true
				break
			}
		}
		if !dup {
			added = append(added, seg)
		}
	}
	if len(added) == 0 {
		return false, cur, cur, nil
	}
	next := strings.Join(append(existing, added...), ";")
	if err := regSetString(h, regValuePathKey, next, typ); err != nil {
		return false, cur, cur, err
	}
	broadcastEnvChange()
	return true, cur, next, nil
}

// RemoveUserPath 从用户 Path 里删掉 segments（大小写与分隔符不敏感）。
// 返回（是否有改动, 改动前的 Path, 改动后的 Path）。
func RemoveUserPath(segments []string) (bool, string, string, error) {
	h, err := regOpenUserEnv(keyRead | keyWrite)
	if err != nil {
		return false, "", "", err
	}
	defer procRegCloseKey.Call(uintptr(h))

	cur, typ, ok := regQueryString(h, regValuePathKey)
	if !ok || typ == 0 {
		return false, "", "", nil
	}
	existing := splitPathList(cur)
	kept := make([]string, 0, len(existing))
	removed := 0
	for _, e := range existing {
		drop := false
		for _, seg := range segments {
			if seg != "" && samePath(e, seg) {
				drop = true
				break
			}
		}
		if drop {
			removed++
			continue
		}
		kept = append(kept, e)
	}
	if removed == 0 {
		return false, cur, cur, nil
	}
	next := strings.Join(kept, ";")
	if err := regSetString(h, regValuePathKey, next, typ); err != nil {
		return false, cur, cur, err
	}
	broadcastEnvChange()
	return true, cur, next, nil
}

// PowerShellScriptsBlocked 报告当前用户的 PowerShell 执行策略是否会拒绝加载 profile 脚本。
//
// 判定依据：Restricted / AllSigned 会拒绝**未签名**的本地 .ps1（我们写的就是这种）。
// 查询失败（没有 powershell.exe 等）时返回 false —— 别因为查不到就不写。
func PowerShellScriptsBlocked() bool {
	out, err := runPowerShell("Get-ExecutionPolicy")
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(out)) {
	case "restricted", "allsigned":
		return true
	}
	return false
}

// runPowerShell 以 -NoProfile 调用系统自带的 Windows PowerShell（不加载任何 profile）。
func runPowerShell(script string) (string, error) {
	ps, err := exec.LookPath("powershell.exe")
	if err != nil {
		return "", err
	}
	cmd := exec.Command(ps, "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
