package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const maxSubscriptionCacheBytes = 16 << 20

var safeSubscriptionIDRegex = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type SubscriptionNodeInfo struct {
	Name   string `json:"name"`
	Type   string `json:"type,omitempty"`
	Server string `json:"server,omitempty"`
	Port   string `json:"port,omitempty"`
}

func fileExistsNonEmpty(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

func (s *Server) subscriptionCacheDir() string {
	return filepath.Join(s.data, "mihomo", "subscription-cache")
}

func (s *Server) subscriptionCachePath(id string) string {
	if !safeSubscriptionIDRegex.MatchString(id) {
		return ""
	}
	return filepath.Join(s.subscriptionCacheDir(), id+".yaml")
}

func parseSubscriptionNodes(path string) ([]SubscriptionNodeInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxSubscriptionCacheBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSubscriptionCacheBytes {
		return nil, errors.New("subscription cache exceeds 16 MiB")
	}
	var document struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parse subscription YAML: %w", err)
	}
	nodes := make([]SubscriptionNodeInfo, 0, len(document.Proxies))
	for _, proxy := range document.Proxies {
		name := scalarText(proxy["name"])
		if name == "" || isBuiltinGroup(name) {
			continue
		}
		nodes = append(nodes, SubscriptionNodeInfo{
			Name:   name,
			Type:   strings.ToLower(scalarText(proxy["type"])),
			Server: scalarText(proxy["server"]),
			Port:   scalarText(proxy["port"]),
		})
		if len(nodes) >= 1000 {
			break
		}
	}
	if len(nodes) == 0 {
		return nil, errors.New("subscription contains no proxy nodes")
	}
	return nodes, nil
}

func scalarText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case uint64:
		return strconv.FormatUint(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

func atomicCopyFile(source, target string, mode os.FileMode) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	temporary := target + ".tmp"
	if err := os.WriteFile(temporary, data, mode); err != nil {
		return err
	}
	if err := os.Rename(temporary, target); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

// migrateActiveSubscriptionCache bootstraps the per-subscription cache and
// imports newer raw data written by the background updater. The URL check is
// important: after editing an active subscription URL, its old running config
// must not be mistaken for a cache of the new URL.
func (s *Server) migrateActiveSubscriptionCache(store *SubscriptionStore) bool {
	if store == nil || store.ActiveID == "" {
		return false
	}
	for i := range store.Items {
		item := &store.Items[i]
		if item.ID != store.ActiveID || item.URL == "" {
			continue
		}
		activeURL := strings.TrimSpace(readText(filepath.Join(s.data, "mihomo", "subscription.url")))
		if activeURL != item.URL {
			return false
		}
		cachePath := s.subscriptionCachePath(item.ID)
		if cachePath == "" {
			return false
		}

		candidate := ""
		backgroundRaw := filepath.Join(s.data, "mihomo", "active-subscription.raw.yaml")
		if rawInfo, err := os.Stat(backgroundRaw); err == nil && rawInfo.Mode().IsRegular() && rawInfo.Size() > 0 {
			cacheInfo, cacheErr := os.Stat(cachePath)
			if cacheErr != nil || rawInfo.ModTime().After(cacheInfo.ModTime()) {
				candidate = backgroundRaw
			}
		}
		if candidate == "" && !fileExistsNonEmpty(cachePath) {
			candidate = filepath.Join(s.data, "mihomo", "config.yaml")
		}
		if candidate == "" {
			if item.CachedURL == item.URL && item.NodeCount == 0 {
				nodes, err := parseSubscriptionNodes(cachePath)
				if err == nil {
					item.NodeCount = len(nodes)
					return true
				}
			}
			return false
		}
		nodes, err := parseSubscriptionNodes(candidate)
		if err != nil {
			return false
		}
		if err := atomicCopyFile(candidate, cachePath, 0600); err != nil {
			return false
		}
		item.CachedURL = item.URL
		item.NodeCount = len(nodes)
		if info, err := os.Stat(candidate); err == nil {
			item.UpdatedAt = info.ModTime()
		}
		return true
	}
	return false
}

func (s *Server) runSubscriptionUpdater(item SubscriptionRecord, activate, useCache bool) ([]byte, error) {
	if s.subscriptionUpdater != nil {
		return s.subscriptionUpdater(item, activate, useCache)
	}
	cachePath := s.subscriptionCachePath(item.ID)
	if cachePath == "" {
		return nil, errors.New("invalid subscription ID")
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0700); err != nil {
		return nil, err
	}
	updater := filepath.Join(s.stack, "bin", "mihomo-update-subscription")
	command := exec.Command(updater)
	command.Env = append(os.Environ(),
		"MIHOMO_SUBSCRIPTION_URL="+item.URL,
		"MIHOMO_RAW_CACHE_OUTPUT="+cachePath,
	)
	if !activate {
		command.Env = append(command.Env, "MIHOMO_UPDATE_VALIDATE_ONLY=1")
	}
	if useCache {
		if item.CachedURL != item.URL || !fileExistsNonEmpty(cachePath) {
			return nil, errors.New("订阅尚未更新，请先更新后再启用")
		}
		command.Env = append(command.Env, "MIHOMO_RAW_SUBSCRIPTION_FILE="+cachePath)
	}
	return command.CombinedOutput()
}

func (s *Server) refreshSubscription(item *SubscriptionRecord, activate bool) error {
	if item == nil {
		return errors.New("订阅不存在")
	}
	var previousURL string
	if activate {
		previousURL = strings.TrimSpace(readText(filepath.Join(s.data, "mihomo", "subscription.url")))
		if err := s.writeActiveSubscriptionURL(item.URL); err != nil {
			return err
		}
	}
	out, err := s.runSubscriptionUpdater(*item, activate, false)
	if err != nil {
		if activate {
			_ = s.writeActiveSubscriptionURL(previousURL)
		}
		log.Printf("mihomo subscription refresh failed: %s", sanitizeLogSecrets(tailLines(string(out), 30)))
		return errors.New("订阅更新或校验失败，原配置已保留")
	}
	nodes, err := parseSubscriptionNodes(s.subscriptionCachePath(item.ID))
	if err != nil {
		return fmt.Errorf("读取订阅节点失败: %w", err)
	}
	item.CachedURL = item.URL
	item.NodeCount = len(nodes)
	item.UpdatedAt = time.Now()
	return nil
}

func (s *Server) activateCachedSubscription(item *SubscriptionRecord) error {
	if item == nil {
		return errors.New("订阅不存在")
	}
	previousURL := strings.TrimSpace(readText(filepath.Join(s.data, "mihomo", "subscription.url")))
	if err := s.writeActiveSubscriptionURL(item.URL); err != nil {
		return err
	}
	out, err := s.runSubscriptionUpdater(*item, true, true)
	if err != nil {
		_ = s.writeActiveSubscriptionURL(previousURL)
		log.Printf("mihomo cached subscription activation failed: %s", sanitizeLogSecrets(tailLines(string(out), 30)))
		return errors.New("启用订阅失败，当前配置未改变")
	}
	nodes, err := parseSubscriptionNodes(s.subscriptionCachePath(item.ID))
	if err == nil {
		item.NodeCount = len(nodes)
	}
	return nil
}

func (s *Server) mihomoSubscriptionNodes(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	s.subscriptionMu.Lock()
	defer s.subscriptionMu.Unlock()
	store, err := s.loadSubscriptionStore()
	if err != nil {
		jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "读取订阅列表失败"})
		return
	}
	for _, item := range store.Items {
		if item.ID != id {
			continue
		}
		path := s.subscriptionCachePath(item.ID)
		if item.CachedURL != item.URL || !fileExistsNonEmpty(path) {
			jsonOut(w, http.StatusConflict, map[string]string{"error": "订阅尚未更新，没有可查看的节点缓存"})
			return
		}
		nodes, err := parseSubscriptionNodes(path)
		if err != nil {
			jsonOut(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
		jsonOut(w, http.StatusOK, map[string]any{"id": item.ID, "name": item.Name, "nodes": nodes})
		return
	}
	jsonOut(w, http.StatusNotFound, map[string]string{"error": "订阅不存在"})
}

func validateSubscriptionName(raw, fallback string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		name = fallback
	}
	if name == "" || len([]rune(name)) > 128 {
		return "", errors.New("订阅名称为空或过长")
	}
	return name, nil
}

func (s *Server) mihomoSubscriptionsV2(w http.ResponseWriter, r *http.Request) {
	s.subscriptionMu.Lock()
	defer s.subscriptionMu.Unlock()
	store, err := s.loadSubscriptionStore()
	if err != nil {
		jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "读取订阅列表失败"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		jsonOut(w, http.StatusOK, s.publicSubscriptions(store))
	case http.MethodPost:
		var body struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		}
		if decodeJSON(r, &body) != nil {
			jsonOut(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
			return
		}
		rawURL, err := validateSubscriptionURL(body.URL)
		if err != nil {
			jsonOut(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		name, err := validateSubscriptionName(body.Name, maskedSubscriptionSource(rawURL))
		if err != nil {
			jsonOut(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		id, err := newSubscriptionID()
		if err != nil {
			jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "无法生成订阅 ID"})
			return
		}
		now := time.Now()
		item := SubscriptionRecord{ID: id, Name: name, URL: rawURL, CreatedAt: now, UpdatedAt: now}
		if err := s.refreshSubscription(&item, false); err != nil {
			jsonOut(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		if store.ActiveID == "" {
			if err := s.activateCachedSubscription(&item); err != nil {
				_ = os.Remove(s.subscriptionCachePath(item.ID))
				jsonOut(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
				return
			}
			store.ActiveID = item.ID
		}
		store.Items = append(store.Items, item)
		if err := s.saveSubscriptionStore(store); err != nil {
			jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "订阅已获取，但列表保存失败"})
			return
		}
		jsonOut(w, http.StatusOK, s.publicSubscriptions(store))
	case http.MethodPatch:
		var body struct {
			ID     string `json:"id"`
			Action string `json:"action"`
			Name   string `json:"name,omitempty"`
			URL    string `json:"url,omitempty"`
		}
		if decodeJSON(r, &body) != nil || (body.Action != "activate" && body.Action != "update" && body.Action != "edit") {
			jsonOut(w, http.StatusBadRequest, map[string]string{"error": "无效订阅操作"})
			return
		}
		index := -1
		for i := range store.Items {
			if store.Items[i].ID == body.ID {
				index = i
				break
			}
		}
		if index < 0 {
			jsonOut(w, http.StatusNotFound, map[string]string{"error": "订阅不存在"})
			return
		}
		item := &store.Items[index]
		switch body.Action {
		case "edit":
			name, err := validateSubscriptionName(body.Name, item.Name)
			if err != nil {
				jsonOut(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			item.Name = name
			if strings.TrimSpace(body.URL) != "" {
				nextURL, err := validateSubscriptionURL(body.URL)
				if err != nil {
					jsonOut(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
					return
				}
				item.URL = nextURL
			}
		case "update":
			if err := s.refreshSubscription(item, item.ID == store.ActiveID); err != nil {
				jsonOut(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
				return
			}
		case "activate":
			if item.ID != store.ActiveID {
				if err := s.activateCachedSubscription(item); err != nil {
					jsonOut(w, http.StatusConflict, map[string]string{"error": err.Error()})
					return
				}
				store.ActiveID = item.ID
			}
		}
		if err := s.saveSubscriptionStore(store); err != nil {
			jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "保存订阅状态失败"})
			return
		}
		if body.Action == "activate" || body.Action == "update" {
			go func(subscriptionID string) { _ = s.syncSubscriptionPricing(subscriptionID) }(item.ID)
		}
		jsonOut(w, http.StatusOK, s.publicSubscriptions(store))
	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		index := -1
		for i := range store.Items {
			if store.Items[i].ID == id {
				index = i
				break
			}
		}
		if index < 0 {
			jsonOut(w, http.StatusNotFound, map[string]string{"error": "订阅不存在"})
			return
		}
		if store.ActiveID == id {
			next := -1
			for i := range store.Items {
				candidate := &store.Items[i]
				if i != index && candidate.CachedURL == candidate.URL && fileExistsNonEmpty(s.subscriptionCachePath(candidate.ID)) {
					next = i
					break
				}
			}
			if next >= 0 {
				if err := s.activateCachedSubscription(&store.Items[next]); err != nil {
					jsonOut(w, http.StatusBadGateway, map[string]string{"error": "无法切换到其他已更新订阅，未执行删除"})
					return
				}
				store.ActiveID = store.Items[next].ID
			} else if len(store.Items) > 1 {
				jsonOut(w, http.StatusConflict, map[string]string{"error": "其他订阅尚未更新，请先更新并启用其他订阅"})
				return
			} else {
				store.ActiveID = ""
				_ = s.writeActiveSubscriptionURL("")
			}
		}
		cachePath := s.subscriptionCachePath(store.Items[index].ID)
		store.Items = append(store.Items[:index], store.Items[index+1:]...)
		if err := s.saveSubscriptionStore(store); err != nil {
			jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "删除订阅失败"})
			return
		}
		if cachePath != "" {
			_ = os.Remove(cachePath)
		}
		jsonOut(w, http.StatusOK, s.publicSubscriptions(store))
	default:
		w.Header().Set("Allow", "GET, POST, PATCH, DELETE")
		jsonOut(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}
