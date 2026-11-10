package main

// selfRemoveScript 生成延时删除脚本的内容（纯函数，便于单测）。
//
// 只删自己的两个 exe，再尝试删**空**目录：用户可能用 -Dir 指到共享目录（C:\Tools\bin），
// 绝不能 rd /s 把别人的东西一起带走。
func selfRemoveScript(dir, batPath string) string {
	logFile := `%TEMP%\packnest-self-remove.log`
	return "@echo off\r\n" +
		"setlocal\r\n" +
		`set "DIR=` + dir + `"` + "\r\n" +
		// 等当前进程退出、文件句柄释放（ping 空转约 3 秒；ICMP 被拦也一样会等）
		"ping -n 4 127.0.0.1 >nul 2>&1\r\n" +
		`del /f /q "%DIR%\pk.exe" >nul 2>&1` + "\r\n" +
		`del /f /q "%DIR%\packnest.exe" >nul 2>&1` + "\r\n" +
		`rd "%DIR%" >nul 2>&1` + "\r\n" +
		`if exist "%DIR%\pk.exe" (echo [%DATE% %TIME%] FAILED %DIR% >> "` + logFile + `") else (echo [%DATE% %TIME%] REMOVED %DIR% >> "` + logFile + `")` + "\r\n" +
		// 脚本自己也要清掉（%~f0 是自身完整路径）
		`del /f /q "%~f0" >nul 2>&1` + "\r\n" +
		"endlocal\r\n"
}
