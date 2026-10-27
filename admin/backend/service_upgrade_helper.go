package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type serviceUpgradeHelperResult struct {
	OK         bool   `json:"ok"`
	Version    string `json:"version,omitempty"`
	BackupDir  string `json:"backupDir,omitempty"`
	Message    string `json:"message,omitempty"`
	RolledBack bool   `json:"rolledBack"`
	Error      string `json:"error,omitempty"`
}

func parseServiceUpgradeHelperResult(output []byte) (serviceUpgradeHelperResult, error) {
	var result serviceUpgradeHelperResult
	if err := json.Unmarshal(output, &result); err != nil {
		result.Error = "升级助手未返回有效结果，请检查服务部署"
		return result, fmt.Errorf("%s", result.Error)
	}
	if !result.OK {
		if strings.TrimSpace(result.Error) == "" {
			result.Error = "升级助手执行失败"
		}
		return result, fmt.Errorf("%s", result.Error)
	}
	if strings.TrimSpace(result.Version) == "" {
		result.OK = false
		result.Error = "升级助手未确认实际程序版本"
		return result, fmt.Errorf("%s", result.Error)
	}
	return result, nil
}

func (s *Server) runServiceUpgradeHelper(spec serviceUpdaterSpec, args ...string) (serviceUpgradeHelperResult, error) {
	helper := filepath.Join(s.prefix, "bin", spec.UpgradeHelper)
	if _, err := os.Stat(helper); err != nil {
		result := serviceUpgradeHelperResult{Error: "软件包升级助手尚未安装，请先部署服务升级组件"}
		return result, fmt.Errorf("%s", result.Error)
	}
	cmd := exec.Command(filepath.Join(s.prefix, "bin", "python"), append([]string{helper}, args...)...)
	cmd.Env = append(os.Environ(), "HOME="+s.home, "PREFIX="+s.prefix, "PATH="+s.prefix+"/bin:/system/bin", "DEBIAN_FRONTEND=noninteractive")
	output, runErr := cmd.Output()
	result, err := parseServiceUpgradeHelperResult(output)
	if err != nil {
		return result, err
	}
	if runErr != nil {
		result.OK = false
		result.Error = "升级助手异常退出，无法确认升级完成"
		return result, fmt.Errorf("%s", result.Error)
	}
	return result, nil
}
