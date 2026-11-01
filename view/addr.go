// Package view 是只读的看链页面。它不导入 chain，只校验节点地址并转发 GET。
package view

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"unicode"
)

// CanonicalNode 接受 host:port，也接受 http:// 或 https:// 加上 host:port。
// 协议和末尾的斜线会去掉。拒绝路径、查询和用户信息，避免把请求转到任意 URL。
// IPv6 主机名会补上方括号。端口必须在 1 到 65535。
func CanonicalNode(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	raw = stripHTTPScheme(raw)
	raw = strings.TrimSuffix(raw, "/")
	host, port, err := net.SplitHostPort(raw)
	if err != nil || host == "" {
		return "", errors.New("bad node")
	}
	for _, r := range host {
		if unicode.IsSpace(r) || r == '@' || r == '/' || r == '\\' || r == '?' || r == '#' || r == '%' {
			return "", errors.New("bad node")
		}
	}
	n, err := strconv.ParseUint(port, 10, 16)
	if err != nil || n == 0 {
		return "", errors.New("bad node")
	}
	if strings.Contains(host, ":") {
		return "[" + host + "]:" + strconv.FormatUint(n, 10), nil
	}
	return host + ":" + strconv.FormatUint(n, 10), nil
}

// stripHTTPScheme 去掉 http:// 或 https://。其他写法原样返回，交给后面的检查拒绝。
func stripHTTPScheme(raw string) string {
	for _, scheme := range []string{"http://", "https://"} {
		if len(raw) >= len(scheme) && strings.EqualFold(raw[:len(scheme)], scheme) {
			return raw[len(scheme):]
		}
	}
	return raw
}

// CanonicalAddress 只接受 20 字节的十六进制地址，可以带 0x。返回小写的 0x 形式。
func CanonicalAddress(raw string) (string, error) {
	body := strings.TrimSpace(raw)
	if len(body) >= 2 && (strings.HasPrefix(body, "0x") || strings.HasPrefix(body, "0X")) {
		body = body[2:]
	}
	if len(body) != 40 {
		return "", errors.New("bad address")
	}
	for _, r := range body {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return "", errors.New("bad address")
		}
	}
	return "0x" + strings.ToLower(body), nil
}
