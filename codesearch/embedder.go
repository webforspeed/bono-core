package codesearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

const (
	maxBatchSize    = 100   // max inputs per embedding API call
	maxInputChars   = 24000 // ~6000 tokens, truncate beyond this
	maxRetries      = 3
	retryBaseDelay  = 500 * time.Millisecond
)

// Embedder calls the OpenRouter embedding API.
type Embedder struct {
	apiKey     string
	baseURL    string
	model      string
	dims       int
	httpClient *http.Client
}

// NewEmbedder creates an embedding client.
func NewEmbedder(apiKey, baseURL, model string, dims int) *Embedder {
	return &Embedder{
		apiKey:  apiKey,
		baseURL: baseURL,
		model:   model,
		dims:    dims,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// embeddingRequest is the JSON payload sent to the API.
type embeddingRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

// embeddingResponse is the JSON response from the API.
type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Embed sends texts to the embedding API and returns their vectors.
// Automatically batches if inputs exceed maxBatchSize.
func (e *Embedder) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	// Truncate long inputs
	truncated := make([]string, len(inputs))
	for i, s := range inputs {
		if len(s) > maxInputChars {
			truncated[i] = s[:maxInputChars]
		} else {
			truncated[i] = s
		}
	}

	results := make([][]float32, len(truncated))

	// Process in batches
	for start := 0; start < len(truncated); start += maxBatchSize {
		end := start + maxBatchSize
		if end > len(truncated) {
			end = len(truncated)
		}
		batch := truncated[start:end]

		vecs, err := e.embedBatch(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("codesearch: embed batch [%d:%d]: %w", start, end, err)
		}

		for i, vec := range vecs {
			results[start+i] = vec
		}
	}

	return results, nil
}

// EmbedSingle embeds a single text input.
func (e *Embedder) EmbedSingle(ctx context.Context, input string) ([]float32, error) {
	vecs, err := e.Embed(ctx, []string{input})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("codesearch: embed returned no vectors")
	}
	return vecs[0], nil
}

func (e *Embedder) embedBatch(ctx context.Context, inputs []string) ([][]float32, error) {
	reqBody := embeddingRequest{
		Model: e.model,
		Input: inputs,
	}
	if e.dims > 0 {
		reqBody.Dimensions = e.dims
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			delay := retryBaseDelay * time.Duration(math.Pow(2, float64(attempt-1)))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		vecs, err := e.doRequest(ctx, body)
		if err == nil {
			return vecs, nil
		}
		lastErr = err

		// Only retry on rate limit or server errors
		if !isRetryable(err) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("codesearch: embed failed after %d retries: %w", maxRetries, lastErr)
}

func (e *Embedder) doRequest(ctx context.Context, body []byte) ([][]float32, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://webforspeed.com")
	req.Header.Set("X-OpenRouter-Title", "webforspeed Bono")
	req.Header.Set("X-OpenRouter-Categories", "cli-agent")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &embedError{
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
	}

	var result embeddingResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("codesearch: decode embedding response: %w", err)
	}

	if result.Error != nil {
		return nil, fmt.Errorf("codesearch: embedding API error: %s", result.Error.Message)
	}

	// Sort by index and extract vectors
	vecs := make([][]float32, len(result.Data))
	for _, d := range result.Data {
		if d.Index < len(vecs) {
			vecs[d.Index] = d.Embedding
		}
	}

	return vecs, nil
}

type embedError struct {
	StatusCode int
	Body       string
}

func (e *embedError) Error() string {
	return fmt.Sprintf("embedding API %d: %s", e.StatusCode, e.Body)
}

func isRetryable(err error) bool {
	if ee, ok := err.(*embedError); ok {
		return ee.StatusCode == 429 || ee.StatusCode >= 500
	}
	return false
}
