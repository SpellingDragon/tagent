// Package testutil 提供真实 API 侧测试的装配助手：从环境（或 shell 配置）读取凭据与
// 端点，以及带退避的重试。**仅限测试使用**——它把本机 shell 配置当凭据来源，不是
// 生产配置通路。
package testutil
