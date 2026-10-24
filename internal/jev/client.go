package jev

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/providerhttp"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

const Endpoint = "https://api.typesafe.ai/v1/systemone"
const DefaultModel = "jev-1.14.0"
const CredentialName = "TYPESAFE_API_KEY"
const maxRequestBytes = 24 << 20 // below the API's 32 MiB cap, including base64

var ErrDisabled = errors.New("Jev is disabled")
var ErrBudget = errors.New("Jev attempt budget exhausted")

type Config struct {
	Enabled bool
	Model   string
	// Images is off, synthetic, or masked. Text redaction never masks pixels.
	Images string
}

func (c Config) normalized() (Config, error) {
	if c.Model == "" {
		c.Model = DefaultModel
	}
	if c.Images == "" {
		c.Images = "off"
	}
	if !modelName.MatchString(c.Model) || (c.Images != "off" && c.Images != "synthetic" && c.Images != "masked") {
		return c, errors.New("invalid Jev model or image-upload policy")
	}
	return c, nil
}

type Preflight struct {
	Version           string `json:"version"`
	Status            string `json:"status"`
	Model             string `json:"model"`
	Images            string `json:"images"`
	CredentialPresent bool   `json:"credentialPresent"`
	Connectivity      string `json:"connectivity"`
}

// CheckConfig reads one scoped credential for presence only. No network calls,
// model-availability claims, worker environment, paths, or credential values.
func CheckConfig(config Config) (Preflight, error) {
	c, err := config.normalized()
	if err != nil {
		return Preflight{}, err
	}
	p := Preflight{Version: Version, Status: "disabled", Model: c.Model, Images: c.Images, Connectivity: "not_checked"}
	if c.Enabled {
		p.Status = "missing_credential"
		p.CredentialPresent = os.Getenv(CredentialName) != ""
		if p.CredentialPresent {
			p.Status = "configured"
		}
	}
	return p, nil
}

// Limits are shared by every call on an attempt client, including failures.
// There are no retries. Callers must reuse this client for the owning attempt.
type Limits struct {
	MaxRequests, MaxQuestions int
	MaxBytes                  int64
	Timeout, MaxEvidenceAge   time.Duration
}

func DefaultLimits() Limits {
	return Limits{MaxRequests: 4, MaxQuestions: 32, MaxBytes: 32 << 20, Timeout: 60 * time.Second, MaxEvidenceAge: 30 * time.Second}
}

type Client struct {
	config                 Config
	identity               runner.AttemptIdentity
	revision, sourceSHA256 string
	limits                 Limits
	ctx                    context.Context
	cancel                 context.CancelFunc
	// Endpoint is fixed in production; only package-local offline fixtures change it.
	endpoint            string
	mu                  sync.Mutex
	requests, questions int
	bytes               int64
}

func New(parent context.Context, config Config, identity runner.AttemptIdentity, sourceRevision, sourceSHA256 string, limits Limits) (*Client, error) {
	c, err := config.normalized()
	if err != nil {
		return nil, err
	}
	if !identifier.MatchString(identity.RunID) || !identifier.MatchString(identity.JobID) || !identifier.MatchString(identity.AttemptID) || !identifier.MatchString(identity.Target) || !revision.MatchString(sourceRevision) || len(sourceSHA256) != 64 || !revision.MatchString(sourceSHA256) {
		return nil, errors.New("invalid Jev attempt identity")
	}
	if limits.MaxRequests < 1 || limits.MaxRequests > 100 || limits.MaxQuestions < 1 || limits.MaxQuestions > 800 || limits.MaxBytes < 1 || limits.MaxBytes > 256<<20 || limits.Timeout <= 0 || limits.Timeout > 10*time.Minute || limits.MaxEvidenceAge <= 0 || limits.MaxEvidenceAge > time.Minute {
		return nil, errors.New("invalid Jev attempt limits")
	}
	ctx, cancel := context.WithTimeout(parent, limits.Timeout)
	return &Client{config: c, identity: identity, revision: sourceRevision, sourceSHA256: sourceSHA256, limits: limits, ctx: ctx, cancel: cancel, endpoint: Endpoint}, nil
}

func (c *Client) Close()                    { c.cancel() }
func (c *Client) CredentialNames() []string { return []string{CredentialName} }

type Usage struct {
	InputTokens  int  `json:"inputTokens"`
	OutputTokens int  `json:"outputTokens"`
	Available    bool `json:"available"`
}

// Record is the only persistable decision data. It contains no snapshot,
// prompts, answers, provider error payloads, credentials or image bytes.
// Artifact references are content hashes, never arbitrary browser paths/URLs.
type Record struct {
	Version         string       `json:"version"`
	Binding         Binding      `json:"binding"`
	Task            string       `json:"task"`
	QuestionVersion string       `json:"questionVersion"`
	CriteriaVersion string       `json:"criteriaVersion"`
	Status          string       `json:"status"`
	RequestCount    int          `json:"requestCount"`
	QuestionCount   int          `json:"questionCount"`
	RequestBytes    int          `json:"requestBytes"`
	DurationMS      int64        `json:"durationMs"`
	ConfiguredModel string       `json:"configuredModel"`
	Model           string       `json:"model,omitempty"`
	Usage           Usage        `json:"usage"`
	CostAvailable   bool         `json:"costAvailable"` // always false; no invented billing
	ObservedAt      time.Time    `json:"observedAt"`
	SnapshotSHA256  string       `json:"snapshotSHA256,omitempty"`
	Image           *ImageRecord `json:"image,omitempty"`
}

type Result struct {
	Record  Record            `json:"record"`
	Answers map[string]Answer `json:"-"`
}

// Decide accepts only engine-owned options/questions and an exact expected
// binding. Observed text occupies data fields, never request configuration.
// Answered means schema-valid inference, not a passing test or verified action.
func (c *Client) Decide(ctx context.Context, expected Binding, observation Observation, contract Contract) (result Result, err error) {
	started := time.Now()
	result.Record = Record{Version: Version, Status: "invalid_request", ConfiguredModel: c.config.Model}
	defer func() { result.Record.DurationMS = time.Since(started).Milliseconds() }()
	if !c.config.Enabled {
		result.Record.Status = "disabled"
		return result, ErrDisabled
	}
	if !expected.validate() || expected.RunID != c.identity.RunID || expected.JobID != c.identity.JobID || expected.AttemptID != c.identity.AttemptID || expected.Target != c.identity.Target || expected.Revision != c.revision || expected.SourceSHA256 != c.sourceSHA256 {
		return result, errors.New("foreign Jev attempt")
	}
	result.Record.Binding = expected
	if err = contract.validate(); err != nil {
		return result, err
	}
	result.Record.Task, result.Record.QuestionVersion, result.Record.CriteriaVersion = contract.Task, contract.QuestionVersion, contract.CriteriaVersion
	if err = observation.validate(expected, time.Now(), c.limits.MaxEvidenceAge, c.config.Images); err != nil {
		return result, err
	}
	result.Record.ObservedAt = observation.ObservedAt
	snapshot := runner.RedactText(observation.Snapshot)
	if len(snapshot) > maxSnapshotBytes {
		return result, errors.New("redacted Jev observation is too large")
	}
	result.Record.SnapshotSHA256 = hash([]byte(snapshot))
	state := map[string]string{"accessible_observation": snapshot, "failure_class": observation.FailureClass}
	payload := struct {
		Model     string              `json:"model"`
		State     map[string]string   `json:"state"`
		Questions map[string]Question `json:"questions"`
		Assets    map[string]any      `json:"assets,omitempty"`
	}{Model: c.config.Model, State: state, Questions: contract.Questions}
	images := observation.Image != nil
	for _, q := range contract.Questions {
		if q.Type == "box" && !images {
			return result, errors.New("Jev Box requires image evidence")
		}
	}
	if images {
		// A fresh unpredictable reference cannot be injected through observed text.
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return result, errors.New("could not prepare Jev image reference")
		}
		ref := "9l-image-" + hex.EncodeToString(nonce[:])
		state["screenshot"] = ref
		payload.Assets = map[string]any{ref: map[string]string{"type": "image", "format": "png", "data_base64": base64.StdEncoding.EncodeToString(observation.Image.data)}}
		record := observation.Image.Record()
		result.Record.Image = &record
	}
	body, err := marshal(payload)
	if err != nil || len(body) > maxRequestBytes {
		return result, errors.New("Jev request exceeds byte limit")
	}
	// No environment key resolution until every local contract/evidence check passes.
	key := os.Getenv(CredentialName)
	if key == "" {
		result.Record.Status = "unavailable"
		return result, errors.New("Jev credential is not configured")
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.ctx, cancel)
	defer stop()
	defer cancel()
	if err := c.ctx.Err(); err != nil {
		result.Record.Status = "cancelled"
		return result, err
	}
	if err := ctx.Err(); err != nil {
		result.Record.Status = "cancelled"
		return result, err
	}
	// Reserve before starting I/O. Parallel calls cannot overspend the attempt.
	c.mu.Lock()
	if c.requests >= c.limits.MaxRequests || c.questions+len(contract.Questions) > c.limits.MaxQuestions || c.bytes+int64(len(body)) > c.limits.MaxBytes {
		c.mu.Unlock()
		result.Record.Status = "budget_exhausted"
		return result, ErrBudget
	}
	c.requests++
	c.questions += len(contract.Questions)
	c.bytes += int64(len(body))
	c.mu.Unlock()
	result.Record.RequestCount, result.Record.QuestionCount, result.Record.RequestBytes = 1, len(contract.Questions), len(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		result.Record.Status = "provider_error"
		return result, errors.New("invalid Jev endpoint")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	if images {
		req.Header.Set("TypeSafe-Danger-Zone-Images", "true")
	}
	raw, err := providerhttp.Do(req, c.limits.Timeout, 1<<20)
	if err != nil {
		result.Record.Status = "provider_error"
		if ctx.Err() != nil {
			result.Record.Status = "cancelled"
		}
		return result, err
	}
	answers, model, usage, err := parseResponse(raw, contract)
	if err != nil {
		result.Record.Status = "invalid_response"
		return result, err
	}
	result.Record.Model, result.Record.Usage = model, usage
	// A late result cannot be applied to evidence that expired during inference.
	if err := c.ctx.Err(); err != nil {
		result.Record.Status = "cancelled"
		return result, err
	}
	if err := ctx.Err(); err != nil {
		result.Record.Status = "cancelled"
		return result, err
	}
	if err := observation.validate(expected, time.Now(), c.limits.MaxEvidenceAge, c.config.Images); err != nil {
		result.Record.Status = "stale_evidence"
		return result, err
	}
	result.Answers = answers
	result.Record.Status = "answered"
	return result, nil
}

func marshal(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, errors.New("invalid Jev data")
	}
	return raw, nil
}
