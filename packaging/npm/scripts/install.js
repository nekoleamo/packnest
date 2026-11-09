#!/usr/bin/env node
'use strict';
// 解析平台二进制路径：优先用 optionalDependencies 装上的平台子包。
//
// 设计要点（方案 §13.5）：npm 分包让我们只发一次小包，二进制走子包；
// 国内用户由 npmmirror 加速，不访问 GitHub。
const path = require('node:path');

const PLATFORM_PACKAGES = {
  'darwin-arm64': '@packnest/darwin-arm64',
  'darwin-x64': '@packnest/darwin-x64',
  'linux-arm64': '@packnest/linux-arm64',
  'linux-x64': '@packnest/linux-x64',
  'win32-arm64': '@packnest/win32-arm64',
  'win32-x64': '@packnest/win32-x64',
};

function platformKey() {
  return process.platform + '-' + process.arch;
}

function binaryPath() {
  const key = platformKey();
  const pkg = PLATFORM_PACKAGES[key];
  if (!pkg) {
    throw new Error('不支持的平台：' + key);
  }
  const exe = process.platform === 'win32' ? 'packnest.exe' : 'packnest';
  // 子包布局：node_modules/@packnest/<platform>/bin/<exe>
  try {
    return path.join(path.dirname(require.resolve(pkg + '/package.json')), 'bin', exe);
  } catch (err) {
    throw new Error(
      '缺少平台子包 ' + pkg + '（可能被 --no-optional 跳过）。\n' +
      '国内可用：npm i -g packnest --registry=https://registry.npmmirror.com\n' +
      '或改用 install.sh / install.ps1（内置 --mirror cn）'
    );
  }
}

module.exports = { binaryPath, platformKey, PLATFORM_PACKAGES };

if (require.main === module) {
  // postinstall：只做校验，不下载（二进制本来就随子包一起装好）
  try {
    const p = binaryPath();
    console.log('packnest: 二进制就位 → ' + p);
  } catch (err) {
    console.error('packnest: ' + err.message);
    process.exit(1);
  }
}
