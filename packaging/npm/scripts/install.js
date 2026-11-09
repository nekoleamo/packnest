#!/usr/bin/env node
'use strict';
// 解析平台二进制：从本包 package.json 的 optionalDependencies 里找**能被解析到的**那个
// （npm 按 os/cpu 字段只装匹配的平台包，所以正常情况下恰好一个）。
//
// 这样平台包的作用域（@packnest 还是 @<你的用户名>）不用写死在这里。
const path = require('node:path');

function platformKey() {
  return process.platform + '-' + process.arch;
}

function binaryPath() {
  const pkg = require('../package.json');
  const deps = Object.keys(pkg.optionalDependencies || {});
  if (deps.length === 0) {
    throw new Error('package.json 里没有 optionalDependencies（打包脚本没正确生成？）');
  }
  for (const name of deps) {
    let pkgJSON;
    try {
      pkgJSON = require.resolve(name + '/package.json');
    } catch {
      continue; // 这个平台的包没装（正常）
    }
    const dir = path.dirname(pkgJSON);
    const meta = require(pkgJSON);
    const exe = process.platform === 'win32' ? 'packnest.exe' : 'packnest';
    return path.join(dir, 'bin', exe);
  }
  throw new Error(
    '没有找到匹配当前平台（' + platformKey() + '）的二进制包。\n' +
    '国内可换镜像重装：npm i -g packnest --registry=https://registry.npmmirror.com\n' +
    '或用 install.sh / install.ps1（内置 --mirror cn）'
  );
}

module.exports = { binaryPath, platformKey };

if (require.main === module) {
  try {
    console.log('packnest: 二进制就位 → ' + binaryPath());
  } catch (err) {
    console.error('packnest: ' + err.message);
    process.exit(1);
  }
}
