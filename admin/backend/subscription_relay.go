package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
	"gopkg.in/yaml.v3"
)

const subscriptionRelayPrefix = "/sub/"

func relayPath(item SubscriptionRecord) string {
	return subscriptionRelayPrefix + item.ID + "/" + item.RelayToken + "/" + url.PathEscape(relayFilename(item.Name))
}

func relayTitle(name string) string {
	return strings.TrimSpace(name) + "（中转）"
}

func relayFilename(name string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, relayTitle(name)) + ".yaml"
}

// Management is authenticated; the subscription URL itself is a revocable,
// random capability usable by clients that cannot send admin session cookies.
func (s *Server) mihomoSubscriptionRelay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		jsonOut(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	id, action := r.URL.Query().Get("id"), ""
	if r.Method == http.MethodPost {
		var body struct {
			ID     string `json:"id"`
			Action string `json:"action"`
		}
		if decodeJSON(r, &body) != nil || (body.Action != "enable" && body.Action != "disable" && body.Action != "rotate") {
			jsonOut(w, http.StatusBadRequest, map[string]string{"error": "无效的中转操作"})
			return
		}
		id, action = body.ID, body.Action
	}
	s.subscriptionMu.Lock()
	defer s.subscriptionMu.Unlock()
	store, err := s.loadSubscriptionStore()
	if err != nil {
		jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "读取订阅列表失败"})
		return
	}
	for i := range store.Items {
		item := &store.Items[i]
		if item.ID != id {
			continue
		}
		if action == "enable" || action == "rotate" {
			// Enabling never downloads anything, including when the cache is absent.
			if item.CachedURL != item.URL || !fileExistsNonEmpty(s.subscriptionCachePath(id)) {
				jsonOut(w, http.StatusConflict, map[string]string{"error": "没有与当前订阅匹配的节点缓存，未请求订阅源"})
				return
			}
			if _, err := relayConfig(s.subscriptionCachePath(id)); err != nil {
				jsonOut(w, http.StatusConflict, map[string]string{"error": "本地节点缓存无法用于中转，未请求订阅源"})
				return
			}
			if item.RelayToken == "" || action == "rotate" {
				raw := make([]byte, 32)
				if _, err := rand.Read(raw); err != nil {
					jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "无法生成中转地址"})
					return
				}
				item.RelayToken = base64.RawURLEncoding.EncodeToString(raw)
			}
		} else if action == "disable" {
			item.RelayToken = ""
		}
		if action != "" {
			if err := s.saveSubscriptionStore(store); err != nil {
				jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "保存中转设置失败"})
				return
			}
		}
		path := ""
		if item.RelayToken != "" {
			path = relayPath(*item)
		}
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == http.MethodGet && r.URL.Query().Get("qr") == "1" {
			if path == "" {
				jsonOut(w, http.StatusConflict, map[string]string{"error": "请先开启订阅中转"})
				return
			}
			scheme := "http"
			if r.TLS != nil || r.URL.Query().Get("scheme") == "https" {
				scheme = "https"
			}
			png, err := qrcode.Encode(scheme+"://"+r.Host+path, qrcode.Medium, 320)
			if err != nil {
				jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "二维码生成失败"})
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
			return
		}
		jsonOut(w, http.StatusOK, map[string]any{"enabled": path != "", "path": path, "name": item.Name, "cached": item.CachedURL == item.URL && fileExistsNonEmpty(s.subscriptionCachePath(id))})
		return
	}
	jsonOut(w, http.StatusNotFound, map[string]string{"error": "订阅不存在"})
}

// This handler only reads a local snapshot. It has no upstream request or
// updater path, even for missing/invalid caches or an expired provider URL.
func (s *Server) subscriptionRelay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, subscriptionRelayPrefix), "/")
	if (len(parts) != 2 && len(parts) != 3) || !safeSubscriptionIDRegex.MatchString(parts[0]) || !strings.HasSuffix(parts[len(parts)-1], ".yaml") {
		http.NotFound(w, r)
		return
	}
	token := parts[1]
	if len(parts) == 2 {
		// Previously copied URLs remain valid without rotating their capability.
		token = strings.TrimSuffix(token, ".yaml")
	}
	s.subscriptionMu.Lock()
	store, err := s.loadSubscriptionStore()
	if err != nil {
		s.subscriptionMu.Unlock()
		http.Error(w, "local subscription unavailable", http.StatusServiceUnavailable)
		return
	}
	var selected *SubscriptionRecord
	for i := range store.Items {
		item := &store.Items[i]
		if item.ID == parts[0] && item.RelayToken != "" && subtle.ConstantTimeCompare([]byte(item.RelayToken), []byte(token)) == 1 {
			selected = item
			break
		}
	}
	if selected == nil {
		s.subscriptionMu.Unlock()
		http.NotFound(w, r)
		return
	}
	if selected.CachedURL != selected.URL {
		s.subscriptionMu.Unlock()
		http.Error(w, "local cache does not match current subscription; upstream was not requested", http.StatusServiceUnavailable)
		return
	}
	config, err := relayConfig(s.subscriptionCachePath(selected.ID))
	name := selected.Name
	s.subscriptionMu.Unlock()
	if err != nil {
		http.Error(w, "local cache unavailable; upstream was not requested", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": relayFilename(name)}))
	w.Header().Set("Profile-Title", "base64:"+base64.StdEncoding.EncodeToString([]byte(relayTitle(name))))
	w.Header().Set("Profile-Update-Interval", "6")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodHead {
		_, _ = w.Write(config)
	}
}

// Return the original cached document byte for byte, including its rules,
// groups, DNS, comments and provider definitions. Do not rebuild client config
// from just the nodes: that would discard the provider's routing behavior.
func relayConfig(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxSubscriptionCacheBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxSubscriptionCacheBytes {
		return nil, errors.New("cache too large")
	}
	var source map[string]any
	if err := yaml.Unmarshal(raw, &source); err != nil {
		return nil, err
	}
	if len(source) == 0 {
		return nil, errors.New("empty subscription document")
	}
	return raw, nil
}
