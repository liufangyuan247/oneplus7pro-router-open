package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	pricingSourcePanelJSON       = "panel-json"
	pricingResponseLimit         = 2 << 20
	pricingRequestTimeout        = 15 * time.Second
	pricingAutomaticSyncInterval = 6 * time.Hour
)

type trafficPricingAdapter interface {
	Fetch(context.Context, SubscriptionPricingSource) (map[string]uint32, error)
}

type panelJSONPricingAdapter struct {
	client *http.Client
}

func pricingAdapter(kind string) (trafficPricingAdapter, error) {
	switch strings.TrimSpace(kind) {
	case pricingSourcePanelJSON:
		return &panelJSONPricingAdapter{client: pricingHTTPClient()}, nil
	default:
		return nil, fmt.Errorf("unsupported pricing source kind %q", kind)
	}
}

func pricingHTTPClient() *http.Client {
	return &http.Client{
		Timeout: pricingRequestTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			origin := via[0].URL
			if !strings.EqualFold(req.URL.Scheme, origin.Scheme) || !strings.EqualFold(req.URL.Host, origin.Host) {
				return errors.New("pricing endpoint redirected to another origin")
			}
			return nil
		},
	}
}

func validatePricingEndpoint(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) == 0 || len(trimmed) > 2048 {
		return "", errors.New("pricing endpoint is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", errors.New("pricing endpoint must be an HTTP or HTTPS URL")
	}
	if parsed.User != nil {
		return "", errors.New("pricing endpoint must not contain URL credentials")
	}
	return parsed.String(), nil
}

func publicPricingEndpoint(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func (a *panelJSONPricingAdapter) Fetch(ctx context.Context, source SubscriptionPricingSource) (map[string]uint32, error) {
	endpoint, err := validatePricingEndpoint(source.Endpoint)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if auth := strings.TrimSpace(source.Authorization); auth != "" {
		req.Header.Set("Authorization", auth)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request pricing endpoint: %w", err)
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, pricingResponseLimit+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read pricing response: %w", err)
	}
	if len(body) > pricingResponseLimit {
		return nil, errors.New("pricing response exceeds 2 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("pricing endpoint returned HTTP %d", resp.StatusCode)
	}

	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode pricing response: %w", err)
	}
	items := findPricingItems(payload, 0)
	if len(items) == 0 {
		return nil, errors.New("pricing response contains no node list")
	}

	result := make(map[string]uint32, len(items))
	for _, item := range items {
		name, ok := firstStringField(item, "name", "node", "remarks", "tag")
		if !ok {
			continue
		}
		canonical := normalizeNodeName(name)
		if canonical == "" || isBuiltinGroup(canonical) {
			continue
		}
		rawRate, ok := firstField(item, "rate", "multiplier", "ratio", "traffic_rate")
		if !ok {
			continue
		}
		multiplier, err := parsePricingMultiplier(rawRate)
		if err != nil {
			return nil, fmt.Errorf("node %q has invalid rate: %w", canonical, err)
		}
		if previous, exists := result[canonical]; exists && previous != multiplier {
			return nil, fmt.Errorf("node %q has conflicting rates", canonical)
		}
		result[canonical] = multiplier
	}
	if len(result) == 0 {
		return nil, errors.New("pricing response contains no valid node rates")
	}
	return result, nil
}

func findPricingItems(value any, depth int) []map[string]any {
	if depth > 5 {
		return nil
	}
	switch typed := value.(type) {
	case []any:
		items := make([]map[string]any, 0, len(typed))
		for _, entry := range typed {
			if item, ok := entry.(map[string]any); ok {
				items = append(items, item)
			}
		}
		return items
	case map[string]any:
		for _, key := range []string{"data", "nodes", "servers", "items"} {
			for actual, nested := range typed {
				if strings.EqualFold(actual, key) {
					if items := findPricingItems(nested, depth+1); len(items) > 0 {
						return items
					}
				}
			}
		}
		for _, nested := range typed {
			if items := findPricingItems(nested, depth+1); len(items) > 0 {
				return items
			}
		}
	}
	return nil
}

func firstField(item map[string]any, names ...string) (any, bool) {
	for _, wanted := range names {
		for actual, value := range item {
			if strings.EqualFold(actual, wanted) {
				return value, true
			}
		}
	}
	return nil, false
}

func firstStringField(item map[string]any, names ...string) (string, bool) {
	value, ok := firstField(item, names...)
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	return strings.TrimSpace(text), ok && strings.TrimSpace(text) != ""
}

func parsePricingMultiplier(value any) (uint32, error) {
	var number float64
	var err error
	switch typed := value.(type) {
	case json.Number:
		number, err = typed.Float64()
	case float64:
		number = typed
	case string:
		text := strings.TrimSpace(typed)
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(text, "x"), "X"))
		number, err = strconv.ParseFloat(text, 64)
	default:
		return 0, errors.New("rate must be a number or numeric string")
	}
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, errors.New("rate is not numeric")
	}
	milli := math.Round(number * 1000)
	if milli < minMultiplierMilli || milli > maxMultiplierMilli {
		return 0, errors.New("rate must be between 0.1x and 100x")
	}
	return uint32(milli), nil
}

func samePricingSource(a, b *SubscriptionPricingSource) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Kind == b.Kind && a.Endpoint == b.Endpoint && a.Authorization == b.Authorization && a.Enabled == b.Enabled
}

func (s *Server) updatePricingSyncStatus(id string, expected SubscriptionPricingSource, syncErr error, count int) {
	s.subscriptionMu.Lock()
	defer s.subscriptionMu.Unlock()
	store, err := s.loadSubscriptionStore()
	if err != nil {
		return
	}
	for i := range store.Items {
		if store.Items[i].ID != id || !samePricingSource(store.Items[i].PricingSource, &expected) {
			continue
		}
		if syncErr != nil {
			store.Items[i].PricingSource.LastError = sanitizeLogSecrets(syncErr.Error())
		} else {
			store.Items[i].PricingSource.LastError = ""
			store.Items[i].PricingSource.LastSyncedAt = time.Now().Format(time.RFC3339)
			store.Items[i].PricingSource.MatchedNodeCount = count
		}
		_ = s.saveSubscriptionStore(store)
		return
	}
}

func (s *Server) syncSubscriptionPricing(id string) error {
	s.pricingSyncMu.Lock()
	defer s.pricingSyncMu.Unlock()

	s.subscriptionMu.Lock()
	store, err := s.loadSubscriptionStore()
	if err != nil {
		s.subscriptionMu.Unlock()
		return err
	}
	var source *SubscriptionPricingSource
	for i := range store.Items {
		if store.Items[i].ID == id && store.Items[i].PricingSource != nil {
			copy := *store.Items[i].PricingSource
			source = &copy
			break
		}
	}
	s.subscriptionMu.Unlock()
	if source == nil {
		return errors.New("subscription has no pricing source")
	}
	if !source.Enabled {
		return errors.New("subscription pricing source is disabled")
	}

	adapter, err := pricingAdapter(source.Kind)
	if err != nil {
		s.updatePricingSyncStatus(id, *source, err, 0)
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), pricingRequestTimeout)
	values, err := adapter.Fetch(ctx, *source)
	cancel()
	if err != nil {
		s.updatePricingSyncStatus(id, *source, err, 0)
		return err
	}

	// Re-check the source after the network request so an obsolete response can
	// never overwrite pricing after the user edits or disables the source.
	s.subscriptionMu.Lock()
	latest, loadErr := s.loadSubscriptionStore()
	unchanged := false
	for i := range latest.Items {
		if latest.Items[i].ID == id && samePricingSource(latest.Items[i].PricingSource, source) {
			unchanged = true
			break
		}
	}
	if loadErr != nil || !unchanged {
		s.subscriptionMu.Unlock()
		if loadErr != nil {
			return loadErr
		}
		return errors.New("pricing source changed while synchronization was running")
	}
	if s.trafficAccounting == nil {
		s.subscriptionMu.Unlock()
		return errors.New("traffic accounting is not initialized")
	}
	s.subscriptionMu.Unlock()
	_, applyErr := s.trafficAccounting.UpdateAutomaticMultipliers(id, values, time.Now().Format(time.RFC3339))
	if applyErr != nil {
		s.updatePricingSyncStatus(id, *source, applyErr, 0)
		return applyErr
	}
	s.updatePricingSyncStatus(id, *source, nil, len(values))
	return nil
}

func (s *Server) enabledPricingSubscriptionIDs() []string {
	s.subscriptionMu.Lock()
	defer s.subscriptionMu.Unlock()
	store, err := s.loadSubscriptionStore()
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(store.Items))
	for _, item := range store.Items {
		if item.PricingSource != nil && item.PricingSource.Enabled {
			ids = append(ids, item.ID)
		}
	}
	return ids
}

func (s *Server) syncAllSubscriptionPricing() {
	for _, id := range s.enabledPricingSubscriptionIDs() {
		if err := s.syncSubscriptionPricing(id); err != nil {
			logMessage := sanitizeLogSecrets(err.Error())
			log.Printf("subscription pricing sync %s failed: %s", id, logMessage)
		}
	}
}

func (s *Server) startSubscriptionPricingSync() {
	go func() {
		timer := time.NewTimer(45 * time.Second)
		defer timer.Stop()
		<-timer.C
		s.syncAllSubscriptionPricing()
		ticker := time.NewTicker(pricingAutomaticSyncInterval)
		defer ticker.Stop()
		for range ticker.C {
			s.syncAllSubscriptionPricing()
		}
	}()
}

func (s *Server) mihomoSubscriptionPricing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		w.Header().Set("Allow", "PUT, POST")
		jsonOut(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var body struct {
		ID                 string `json:"id"`
		Kind               string `json:"kind"`
		Endpoint           string `json:"endpoint"`
		Authorization      string `json:"authorization"`
		Enabled            bool   `json:"enabled"`
		ClearAuthorization bool   `json:"clearAuthorization"`
	}
	if decodeJSON(r, &body) != nil || strings.TrimSpace(body.ID) == "" {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "invalid pricing request"})
		return
	}

	switch r.Method {
	case http.MethodPut:
		kind := strings.TrimSpace(body.Kind)
		if _, err := pricingAdapter(kind); err != nil {
			jsonOut(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		endpoint := ""
		if strings.TrimSpace(body.Endpoint) != "" {
			var err error
			endpoint, err = validatePricingEndpoint(body.Endpoint)
			if err != nil {
				jsonOut(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
		}
		s.pricingSyncMu.Lock()
		s.subscriptionMu.Lock()
		store, err := s.loadSubscriptionStore()
		if err != nil {
			s.subscriptionMu.Unlock()
			s.pricingSyncMu.Unlock()
			jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "failed to load subscriptions"})
			return
		}
		found := false
		for i := range store.Items {
			if store.Items[i].ID != body.ID {
				continue
			}
			found = true
			old := store.Items[i].PricingSource
			if endpoint == "" && old != nil {
				endpoint = old.Endpoint
			}
			if endpoint == "" {
				s.subscriptionMu.Unlock()
				s.pricingSyncMu.Unlock()
				jsonOut(w, http.StatusBadRequest, map[string]string{"error": "pricing endpoint is required"})
				return
			}
			auth := strings.TrimSpace(body.Authorization)
			if auth == "" && !body.ClearAuthorization && old != nil {
				auth = old.Authorization
			}
			store.Items[i].PricingSource = &SubscriptionPricingSource{
				Kind: kind, Endpoint: endpoint, Authorization: auth, Enabled: body.Enabled,
			}
			if old != nil && old.Kind == kind && old.Endpoint == endpoint && old.Authorization == auth {
				store.Items[i].PricingSource.LastSyncedAt = old.LastSyncedAt
				store.Items[i].PricingSource.LastError = old.LastError
				store.Items[i].PricingSource.MatchedNodeCount = old.MatchedNodeCount
			}
			break
		}
		if !found {
			s.subscriptionMu.Unlock()
			s.pricingSyncMu.Unlock()
			jsonOut(w, http.StatusNotFound, map[string]string{"error": "subscription not found"})
			return
		}
		if err := s.saveSubscriptionStore(store); err != nil {
			s.subscriptionMu.Unlock()
			s.pricingSyncMu.Unlock()
			jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "failed to save pricing source"})
			return
		}
		s.subscriptionMu.Unlock()
		s.pricingSyncMu.Unlock()
		if body.Enabled {
			_ = s.syncSubscriptionPricing(body.ID)
		} else if s.trafficAccounting != nil {
			if err := s.trafficAccounting.DisableAutomaticPricing(body.ID); err != nil {
				jsonOut(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		}
		s.subscriptionMu.Lock()
		store, _ = s.loadSubscriptionStore()
		response := s.publicSubscriptions(store)
		s.subscriptionMu.Unlock()
		jsonOut(w, http.StatusOK, response)
	case http.MethodPost:
		if err := s.syncSubscriptionPricing(body.ID); err != nil {
			jsonOut(w, http.StatusBadGateway, map[string]string{"error": sanitizeLogSecrets(err.Error())})
			return
		}
		s.subscriptionMu.Lock()
		store, _ := s.loadSubscriptionStore()
		response := s.publicSubscriptions(store)
		s.subscriptionMu.Unlock()
		jsonOut(w, http.StatusOK, response)
	}
}
