package rl

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// maxRedirectHops 是 30x 链的跳数上界，与标准库默认一致（"stop after 10 consecutive
// requests"）。自定义 CheckRedirect 会整体替换 net/http 的默认策略、连带丢掉它自带的
// 环路上界，故该上界必须在此显式重新施加。
const maxRedirectHops = 10

// EndpointRedirectPolicy 构造 http.Client 的 CheckRedirect，对 30x 链按跳校验目标
// host 是否在 allowlist 内。匹配粒度、空 allowlist 的部署语义、跳数上界（见
// maxRedirectHops）与主机名归一，均以文档为唯一真源。判定留在 rl 包内、不引入
// provider SDK 依赖，宿主经传输层注入口装上守卫。
//
// 契约: docs/wiki/rl/rl-architecture.md#redirect-policy
func EndpointRedirectPolicy(allowedHosts []string) func(*http.Request, []*http.Request) error {
	allowlist := make(map[string]bool, len(allowedHosts))
	for _, hst := range allowedHosts {
		if hst = strings.ToLower(strings.TrimSpace(hst)); hst != "" {
			allowlist[hst] = true
		}
	}
	return func(req *http.Request, via []*http.Request) error {
		host := normalizeRedirectHost(req.URL.Host)
		if host == "" {
			return fmt.Errorf("endpoint redirect policy: unparseable redirect target %q", req.URL.Host)
		}
		if len(via) >= maxRedirectHops {
			return fmt.Errorf("endpoint redirect policy: hop cap exceeded after %d redirects (last target host %q)",
				len(via), host)
		}
		if !allowlist[host] {
			return fmt.Errorf("endpoint redirect policy: hop %d target host %q not in endpoint allowlist (initial URL %q)",
				len(via), host, via[0].URL.String())
		}
		return nil
	}
}

// normalizeRedirectHost 小写化主机并去端口（allowlist 语义是精确 host、任意端口）；
// 带方括号的 IPv6 字面量保留方括号。
func normalizeRedirectHost(hostPort string) string {
	if host, _, err := net.SplitHostPort(hostPort); err == nil {
		return strings.ToLower(host)
	}
	return strings.ToLower(strings.Trim(hostPort, "[]"))
}

// NewEndpointGuardedClient 返回可直接用作 openai 式 SDK 传输层的 http.Client：默认代理
// 传输 ＋ 按跳的allowlist 守卫。allowlist 为空时得到"拒绝所有重定向"的客户端。
func NewEndpointGuardedClient(allowedHosts []string) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &http.Client{
		Transport:     transport,
		CheckRedirect: EndpointRedirectPolicy(allowedHosts),
	}
}
