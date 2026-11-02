package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	pairdropDefaultAdminURL  = "http://127.0.0.1:3000"
	pairdropAdminSettingsAPI = "/mailbox/api/admin/settings"
	pairdropAdminHeader      = "x-router-mailbox-admin"
	pairdropAdminTimeout     = 3 * time.Second
	pairdropMaxBodyBytes     = 64 * 1024

	pairdropMinRetention  = 60           // 1 minute
	pairdropMaxRetention  = 604800       // 7 days
	pairdropMinFileBytes  = 1            // 1 byte
	pairdropMaxFileBytes  = 10737418240  // 10 GiB
	pairdropMinTotalBytes = 1            // 1 byte
	pairdropMaxTotalBytes = 107374182400 // 100 GiB
)

// PairdropSettings represents the public configuration and storage state of the mailbox.
type PairdropSettings struct {
	RetentionSeconds int64 `json:"retentionSeconds"`
	MaxFileBytes     int64 `json:"maxFileBytes"`
	MaxTotalBytes    int64 `json:"maxTotalBytes"`
	MaxFiles         int64 `json:"maxFiles"`
	StoredBytes      int64 `json:"storedBytes"`
}

// Reject quoted numbers instead of silently coercing them into configuration values.
type pairdropInteger json.Number

func (n *pairdropInteger) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] == '"' {
		return errors.New("expected JSON number")
	}
	var value json.Number
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*n = pairdropInteger(value)
	return nil
}

type pairdropSettingsUpdateRequest struct {
	RetentionSeconds *pairdropInteger `json:"retentionSeconds"`
	MaxFileBytes     *pairdropInteger `json:"maxFileBytes"`
	MaxTotalBytes    *pairdropInteger `json:"maxTotalBytes"`
}

type pairdropUpstreamResponse struct {
	RetentionSeconds *pairdropInteger `json:"retentionSeconds"`
	MaxFileBytes     *pairdropInteger `json:"maxFileBytes"`
	MaxTotalBytes    *pairdropInteger `json:"maxTotalBytes"`
	MaxFiles         *pairdropInteger `json:"maxFiles"`
	StoredBytes      *pairdropInteger `json:"storedBytes"`
}

var pairdropDefaultTransport = &http.Transport{
	Proxy:                 nil,
	DialContext:           (&net.Dialer{Timeout: pairdropAdminTimeout, KeepAlive: 30 * time.Second}).DialContext,
	ResponseHeaderTimeout: pairdropAdminTimeout,
	IdleConnTimeout:       30 * time.Second,
	MaxIdleConns:          2,
	MaxIdleConnsPerHost:   2,
	MaxConnsPerHost:       4,
}

func (s *Server) pairdropSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handlePairdropSettingsGet(w, r)
	case http.MethodPut:
		s.handlePairdropSettingsPut(w, r)
	default:
		w.Header().Set("Allow", "GET, PUT")
		jsonOut(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) pairdropAdminKeyPath() string {
	return filepath.Join(s.home, "router-data", "pairdrop", "mailbox", "admin.key")
}

func (s *Server) readPairdropAdminKey() (string, error) {
	raw, err := os.ReadFile(s.pairdropAdminKeyPath())
	if err != nil {
		return "", errors.New("admin key unreadable")
	}
	key := strings.TrimSpace(string(raw))
	if len(key) != 64 {
		return "", errors.New("invalid key length")
	}
	if _, err := hex.DecodeString(key); err != nil {
		return "", errors.New("invalid key hex")
	}
	return key, nil
}

func (s *Server) pairdropClientAndURL() (*http.Client, string) {
	baseURL := s.pairdropAdminURL
	if baseURL == "" {
		baseURL = pairdropDefaultAdminURL
	}
	transport := s.pairdropAdminTransport
	if transport == nil {
		transport = pairdropDefaultTransport
	}
	client := &http.Client{
		Transport:     transport,
		Timeout:       pairdropAdminTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	return client, baseURL
}

func parseStrictIntParam(num *pairdropInteger, minVal, maxVal int64, label string) (int64, string) {
	if num == nil {
		return 0, fmt.Sprintf("缺少%s参数", label)
	}
	raw := string(*num)
	if strings.ContainsAny(raw, ".eE") {
		return 0, fmt.Sprintf("%s必须为整数", label)
	}
	val, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Sprintf("%s格式错误", label)
	}
	if val < minVal || val > maxVal {
		return 0, fmt.Sprintf("%s超出有效范围", label)
	}
	return val, ""
}

func parseAndValidateUpstreamResponse(reader io.Reader) (*PairdropSettings, error) {
	var raw pairdropUpstreamResponse
	dec := json.NewDecoder(reader)
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return nil, errors.New("invalid upstream json")
	}

	retention, errStr := parseStrictIntParam(raw.RetentionSeconds, pairdropMinRetention, pairdropMaxRetention, "保留时长")
	if errStr != "" {
		return nil, errors.New(errStr)
	}
	maxFile, errStr := parseStrictIntParam(raw.MaxFileBytes, pairdropMinFileBytes, pairdropMaxFileBytes, "单文件上限")
	if errStr != "" {
		return nil, errors.New(errStr)
	}
	maxTotal, errStr := parseStrictIntParam(raw.MaxTotalBytes, pairdropMinTotalBytes, pairdropMaxTotalBytes, "暂存总容量")
	if errStr != "" {
		return nil, errors.New(errStr)
	}
	maxFiles, errStr := parseStrictIntParam(raw.MaxFiles, 0, 10000000, "文件数上限")
	if errStr != "" {
		return nil, errors.New(errStr)
	}
	stored, errStr := parseStrictIntParam(raw.StoredBytes, 0, pairdropMaxTotalBytes, "已用容量")
	if errStr != "" {
		return nil, errors.New(errStr)
	}
	if maxFile > maxTotal {
		return nil, errors.New("maxFile exceeds maxTotal")
	}

	return &PairdropSettings{
		RetentionSeconds: retention,
		MaxFileBytes:     maxFile,
		MaxTotalBytes:    maxTotal,
		MaxFiles:         maxFiles,
		StoredBytes:      stored,
	}, nil
}

func (s *Server) handlePairdropSettingsGet(w http.ResponseWriter, r *http.Request) {
	key, err := s.readPairdropAdminKey()
	if err != nil {
		jsonOut(w, http.StatusServiceUnavailable, map[string]string{"error": "传输站管理密钥未就绪或格式无效"})
		return
	}

	client, baseURL := s.pairdropClientAndURL()
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, baseURL+pairdropAdminSettingsAPI, nil)
	if err != nil {
		jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "创建请求失败"})
		return
	}
	req.Header.Set(pairdropAdminHeader, key)

	resp, err := client.Do(req)
	if err != nil {
		jsonOut(w, http.StatusBadGateway, map[string]string{"error": "无法连接设备传输站服务"})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		jsonOut(w, http.StatusBadGateway, map[string]string{"error": "传输站管理认证失败"})
		return
	}
	if resp.StatusCode != http.StatusOK {
		jsonOut(w, http.StatusBadGateway, map[string]string{"error": "传输站服务响应异常"})
		return
	}

	settings, err := parseAndValidateUpstreamResponse(io.LimitReader(resp.Body, pairdropMaxBodyBytes))
	if err != nil {
		jsonOut(w, http.StatusBadGateway, map[string]string{"error": "传输站返回数据格式无效"})
		return
	}

	jsonOut(w, http.StatusOK, settings)
}

func (s *Server) handlePairdropSettingsPut(w http.ResponseWriter, r *http.Request) {
	key, err := s.readPairdropAdminKey()
	if err != nil {
		jsonOut(w, http.StatusServiceUnavailable, map[string]string{"error": "传输站管理密钥未就绪或格式无效"})
		return
	}

	var reqBody pairdropSettingsUpdateRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, pairdropMaxBodyBytes))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	if err := dec.Decode(&reqBody); err != nil {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误或包含未知字段"})
		return
	}
	var trailing json.RawMessage
	if dec.Decode(&trailing) != io.EOF {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "请求包含多余数据"})
		return
	}

	retention, errStr := parseStrictIntParam(reqBody.RetentionSeconds, pairdropMinRetention, pairdropMaxRetention, "保留时长")
	if errStr != "" {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": errStr})
		return
	}
	maxFile, errStr := parseStrictIntParam(reqBody.MaxFileBytes, pairdropMinFileBytes, pairdropMaxFileBytes, "单文件上限")
	if errStr != "" {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": errStr})
		return
	}
	maxTotal, errStr := parseStrictIntParam(reqBody.MaxTotalBytes, pairdropMinTotalBytes, pairdropMaxTotalBytes, "暂存总容量")
	if errStr != "" {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": errStr})
		return
	}
	if maxFile > maxTotal {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "单文件上限不能大于暂存总容量"})
		return
	}

	upstreamPayload, _ := json.Marshal(map[string]int64{
		"retentionSeconds": retention,
		"maxFileBytes":     maxFile,
		"maxTotalBytes":    maxTotal,
	})

	client, baseURL := s.pairdropClientAndURL()
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPut, baseURL+pairdropAdminSettingsAPI, bytes.NewReader(upstreamPayload))
	if err != nil {
		jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "创建请求失败"})
		return
	}
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set(pairdropAdminHeader, key)

	resp, err := client.Do(upstreamReq)
	if err != nil {
		jsonOut(w, http.StatusBadGateway, map[string]string{"error": "无法连接设备传输站服务"})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		jsonOut(w, http.StatusBadGateway, map[string]string{"error": "传输站管理认证失败"})
		return
	}
	if resp.StatusCode == http.StatusBadRequest {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "传输站配置参数无效"})
		return
	}
	if resp.StatusCode != http.StatusOK {
		jsonOut(w, http.StatusBadGateway, map[string]string{"error": "传输站服务响应异常"})
		return
	}

	settings, err := parseAndValidateUpstreamResponse(io.LimitReader(resp.Body, pairdropMaxBodyBytes))
	if err != nil {
		jsonOut(w, http.StatusBadGateway, map[string]string{"error": "传输站返回数据格式无效"})
		return
	}

	jsonOut(w, http.StatusOK, settings)
}
