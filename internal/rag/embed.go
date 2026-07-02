package rag

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
	Model() string
	Provider() string
}

type FakeEmbedder struct {
	ModelID string
	Dim     int
}

func (e FakeEmbedder) Model() string {
	if e.ModelID == "" {
		return "fake/bge-m3"
	}
	return e.ModelID
}

func (e FakeEmbedder) Provider() string { return "fake" }

func (e FakeEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	dim := e.Dim
	if dim <= 0 {
		dim = defaultEmbeddingDim
	}
	out := make([][]float64, 0, len(texts))
	for _, text := range texts {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		vec := make([]float64, dim)
		sum := sha256.Sum256([]byte(text))
		for i := 0; i < dim; i++ {
			n := binary.BigEndian.Uint16(sum[(i*2)%len(sum) : (i*2)%len(sum)+2])
			vec[i] = float64(n%2000)/1000 - 1
		}
		out = append(out, Normalize(vec))
	}
	return out, nil
}

type OllamaEmbedder struct {
	BaseURL string
	ModelID string
	Client  *http.Client
}

func (e *OllamaEmbedder) Model() string {
	if e.ModelID == "" {
		return DefaultModel
	}
	return e.ModelID
}

func (e *OllamaEmbedder) Provider() string { return DefaultProvider }

func (e *OllamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	base := e.BaseURL
	if base == "" {
		base = "http://127.0.0.1:11434"
	}
	client := e.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	body := map[string]any{"model": e.Model(), "input": texts}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/embed", &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil, fmt.Errorf("ollama embed status %d", res.StatusCode)
	}
	var parsed struct {
		Embeddings [][]float64 `json:"embeddings"`
		Embedding  []float64   `json:"embedding"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 50*1024*1024)).Decode(&parsed); err != nil {
		return nil, err
	}
	vectors := parsed.Embeddings
	if len(vectors) == 0 && len(parsed.Embedding) > 0 {
		vectors = [][]float64{parsed.Embedding}
	}
	if len(vectors) != len(texts) {
		return nil, fmt.Errorf("ollama returned %d embeddings for %d inputs", len(vectors), len(texts))
	}
	for i := range vectors {
		vectors[i] = Normalize(vectors[i])
	}
	return vectors, nil
}

func Normalize(vec []float64) []float64 {
	var sum float64
	for _, v := range vec {
		sum += v * v
	}
	if sum == 0 {
		return vec
	}
	norm := math.Sqrt(sum)
	out := make([]float64, len(vec))
	for i, v := range vec {
		out[i] = v / norm
	}
	return out
}

func Cosine(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var sum float64
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}
