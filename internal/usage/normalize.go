package usage

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"opencode2api/internal/store"
	"opencode2api/internal/telemetry"
)

// Normalize turns the request trace into one metadata-only usage event. It
// returns false only when there is no trace or the request is not an inference
// endpoint. The event is not priced; Service.record applies the price.
func Normalize(meta *telemetry.RequestMeta, path string, status int, started, firstWrite time.Time) (store.Event, bool) {
	if meta == nil || !isInferencePath(path) {
		return store.Event{}, false
	}
	if status == 0 {
		status = http.StatusOK
	}
	finished := time.Now().UTC()
	if finished.Before(started) {
		finished = started
	}
	outcome := outcomeOf(meta, status)
	success := successOf(meta, status)
	event := store.Event{
		RequestID:   requestID(),
		TS:          finished,
		Day:         finished.Format("2006-01-02"),
		Provider:    providerOf(meta),
		Model:       strings.TrimSpace(meta.Model),
		Endpoint:    endpointOf(path),
		Tier:        tierOf(meta),
		Status:      status,
		Outcome:     outcome,
		Success:     success,
		LatencyMS:   finished.Sub(started).Milliseconds(),
		Stream:      meta.Stream,
		Attempts:    max(meta.Attempts, 1),
		UsageSource: "unavailable",
	}
	if event.Model == "" {
		event.Model = strings.TrimSpace(meta.ModelAlias)
	}
	if event.Model == "" {
		event.Model = "unknown"
	}
	if event.LatencyMS < 0 {
		event.LatencyMS = 0
	}
	if !firstWrite.IsZero() {
		ttft := firstWrite.Sub(started).Milliseconds()
		if ttft >= 0 {
			event.TTFTMS = &ttft
		}
	}
	if !success {
		code := strings.TrimSpace(meta.Outcome)
		if code == "" {
			code = fmt.Sprintf("http_%d", status)
		}
		event.ErrorCode = &code
	}
	if meta.ServerKeyID != "" {
		fp := meta.ServerKeyID
		event.ServerKeyFP = &fp
	}
	if meta.Session != "" {
		event.SessionFP = fingerprint(meta.Session)
	}
	if meta.Proxy != "" {
		kind := "static"
		if strings.Contains(strings.ToLower(meta.Proxy), "pool") {
			kind = "pool"
		}
		event.ProxyKind = &kind
		event.ProxyID = fingerprint(meta.Proxy)
	} else {
		kind := "direct"
		event.ProxyKind = &kind
	}

	input := int64(meta.Usage.Input) - int64(meta.Usage.Cached) - int64(meta.Usage.CacheCreation)
	if input < 0 {
		input = 0
	}
	event.InputTokens = input
	event.CachedTokens = maxInt64(int64(meta.Usage.Cached), 0)
	event.CacheWriteTokens = maxInt64(int64(meta.Usage.CacheCreation), 0)
	event.OutputTokens = maxInt64(int64(meta.Usage.Output), 0)
	event.ReasoningTokens = maxInt64(int64(meta.Usage.Reasoning), 0)
	event.TotalTokens = event.InputTokens + event.CachedTokens + event.CacheWriteTokens + event.OutputTokens
	switch {
	case meta.UsageReported:
		event.UsageSource = "provider"
	case event.TotalTokens > 0:
		event.UsageSource = "partial"
	default:
		event.UsageSource = "unavailable"
	}
	if metaJSON := buildMetaJSON(meta, event.TotalTokens); metaJSON != "" {
		event.MetaJSON = &metaJSON
	}
	return event, true
}

// Price is a USD-per-million-token price. A nil component means the source did
// not state it.
type Price struct {
	Input      *float64
	Output     *float64
	CacheRead  *float64
	CacheWrite *float64
	Source     string
}

// ApplyPrice fills the canonical integer micro-USD cost fields. Cost is left
// nil when no effective price is known or when the provider did not report
// usage; the UI then shows an em dash rather than a fabricated zero.
func ApplyPrice(event *store.Event, price Price, known bool) {
	if event.UsageSource != "provider" {
		setSource(event, "none")
		return
	}
	if !known {
		if isFreeModel(event.Model) {
			zero := int64(0)
			event.PricingSource = stringPtr("hardcoded")
			event.CostMicroUSD = &zero
			event.CostInputMicroUSD = &zero
			event.CostOutputMicroUSD = &zero
			event.CostCacheMicroUSD = &zero
		} else {
			setSource(event, "none")
		}
		return
	}
	if event.InputTokens > 0 && price.Input == nil ||
		event.OutputTokens > 0 && price.Output == nil ||
		event.CachedTokens > 0 && price.CacheRead == nil ||
		event.CacheWriteTokens > 0 && price.CacheWrite == nil {
		setSource(event, "none")
		return
	}
	inputCost := componentCost(event.InputTokens, price.Input)
	outputCost := componentCost(event.OutputTokens, price.Output)
	cacheCost := componentCost(event.CachedTokens, price.CacheRead) + componentCost(event.CacheWriteTokens, price.CacheWrite)
	total := inputCost + outputCost + cacheCost
	source := price.Source
	if source == "" {
		source = "opencode"
	}
	event.PricingSource = &source
	event.CostInputMicroUSD = &inputCost
	event.CostOutputMicroUSD = &outputCost
	event.CostCacheMicroUSD = &cacheCost
	event.CostMicroUSD = &total
}

func componentCost(tokens int64, perMillion *float64) int64 {
	if tokens <= 0 || perMillion == nil || *perMillion <= 0 {
		return 0
	}
	return int64(math.Floor(float64(tokens)*(*perMillion) + 0.5 + 1e-9))
}

func setSource(event *store.Event, source string) {
	event.PricingSource = &source
}

func isFreeModel(model string) bool {
	return strings.Contains(strings.ToLower(model), "free")
}

func isInferencePath(path string) bool {
	switch path {
	case "/v1/chat/completions", "/v1/responses", "/v1/messages", "/v1/systemone":
		return true
	default:
		return false
	}
}

func endpointOf(path string) string {
	switch path {
	case "/v1/chat/completions":
		return "chat"
	case "/v1/responses":
		return "responses"
	case "/v1/messages":
		return "messages"
	case "/v1/systemone":
		return "systemone"
	default:
		return "unknown"
	}
}

func providerOf(meta *telemetry.RequestMeta) string {
	if meta.Anonymous || meta.Channel == "anonymous" {
		return "anonymous"
	}
	if meta.Tier == "go" {
		return "opencode-go"
	}
	return "opencode"
}

func tierOf(meta *telemetry.RequestMeta) string {
	if meta.Anonymous || meta.Channel == "anonymous" {
		return "anonymous"
	}
	if meta.Tier != "" {
		return meta.Tier
	}
	return "unknown"
}

func outcomeOf(meta *telemetry.RequestMeta, status int) string {
	if meta.Outcome != "" {
		return meta.Outcome
	}
	if meta.Channel == "" || meta.Channel == "not_routed" {
		return "not_routed"
	}
	switch {
	case status >= 200 && status < 400:
		return "success"
	case status >= 400 && status < 500:
		return "client_error"
	case status >= 500:
		return "server_error"
	default:
		return "unknown"
	}
}

func successOf(meta *telemetry.RequestMeta, status int) bool {
	if status < 200 || status >= 400 {
		return false
	}
	return meta.Outcome == "" || meta.Outcome == "success"
}

func buildMetaJSON(meta *telemetry.RequestMeta, canonicalTotal int64) string {
	payload := map[string]any{"schema_version": 1}
	if meta.UsageReported && int64(meta.Usage.Total) > 0 && int64(meta.Usage.Total) != canonicalTotal {
		payload["provider_total_tokens"] = meta.Usage.Total
	}
	if meta.AttemptOutcome != "" {
		payload["attempt_outcome"] = meta.AttemptOutcome
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func requestID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", data[0:4], data[4:6], data[6:8], data[8:10], data[10:16])
}

func fingerprint(value string) *string {
	sum := sha256.Sum256([]byte(value))
	encoded := hex.EncodeToString(sum[:])[:16]
	return &encoded
}

func stringPtr(value string) *string { return &value }

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
