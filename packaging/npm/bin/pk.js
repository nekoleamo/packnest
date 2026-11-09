#!/usr/bin/env node
// packnest 的 JS 入口：把调用转发给平台子包里的真实二进制（不加任何逻辑）。
//
// 之所以用 JS 壳而不是直接 bin 指向二进制：npm 的 bin 必须是包内文件，
// 而真正的二进制在 @packnest/<platform> 子包里（方案 §13.5 的"npm 分包"）。
const { spawnSync } = require('node:child_process');
const { binaryPath } = require('../scripts/install.js');

const target = binaryPath();
const result = spawnSync(target, process.argv.slice(2), { stdio: 'inherit' });
if (result.error) {
  console.error('packnest: 无法执行 ' + target + '：' + result.error.message);
  process.exit(1);
}
process.exit(result.status === null ? 1 : result.status);
