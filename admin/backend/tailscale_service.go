package main

import (
	"regexp"
	"strconv"
	"strings"
)

func parseTailscaleService(text string) ServiceInfo {
	values := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			values[key] = value
		}
	}
	info := ServiceInfo{Name: "tailscale", Label: allowedServices["tailscale"].Label, State: "未安装"}
	if values["installed"] != "yes" {
		return info
	}
	info.State = "已禁用"
	if values["enabled"] == "yes" {
		info.State = "停止"
	}
	if values["daemon"] == "running" {
		info.State = "运行中"
		info.PID, _ = strconv.Atoi(values["pid"])
		info.UptimeSec, _ = strconv.Atoi(values["uptime"])
		info.PID = max(0, info.PID)
		info.UptimeSec = max(0, info.UptimeSec)
	}
	return info
}

func (s *Server) readTailscaleService() ServiceInfo {
	text, err := s.tailscaleCommand("service-status")
	if err != nil {
		return ServiceInfo{Name: "tailscale", Label: allowedServices["tailscale"].Label, State: "状态未知"}
	}
	return parseTailscaleService(text)
}

var tailscaleVersionLine = regexp.MustCompile(`(?m)^v?([0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?)\s*$`)

func parseTailscaleVersion(text string) string {
	if match := tailscaleVersionLine.FindStringSubmatch(strings.TrimSpace(text)); len(match) == 2 {
		return "v" + match[1]
	}
	return ""
}
