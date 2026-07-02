package rag

import "time"

const (
	DefaultModel         = "bge-m3"
	DefaultProvider      = "ollama"
	DefaultStrategy      = StrategyStructural
	StrategyFixed        = "fixed"
	StrategyStructural   = "structural"
	ModeSemantic         = "semantic"
	ModeLexicalFallback  = "lexical_fallback"
	ModeSkipped          = "skipped"
	DefaultFixedTokens   = 700
	DefaultOverlapTokens = 100
	DefaultMaxFileBytes  = 512 * 1024
	DefaultSearchTopK    = 5
	defaultEmbeddingDim  = 64
)

type Config struct {
	Enabled bool   `json:"enabled,omitempty"`
	Model   string `json:"model,omitempty"`
}

func DefaultConfig() Config {
	return Config{Enabled: false, Model: DefaultModel}
}

type Document struct {
	Source string    `json:"source"`
	Path   string    `json:"path"`
	Title  string    `json:"title"`
	Text   string    `json:"text"`
	Lines  []string  `json:"-"`
	SHA256 string    `json:"sha256"`
	Size   int64     `json:"size_bytes"`
	ModAt  time.Time `json:"modified_at"`
}

type Chunk struct {
	ChunkID            string    `json:"chunk_id"`
	Strategy           string    `json:"strategy"`
	Source             string    `json:"source"`
	Path               string    `json:"path"`
	Title              string    `json:"title"`
	Section            string    `json:"section"`
	StartLine          int       `json:"start_line"`
	EndLine            int       `json:"end_line"`
	Text               string    `json:"text"`
	TokenCountEstimate int       `json:"token_count_estimate"`
	ContentSHA256      string    `json:"content_sha256"`
	CreatedAt          time.Time `json:"created_at"`
}

type Embedding struct {
	ChunkID       string    `json:"chunk_id"`
	Model         string    `json:"model"`
	Provider      string    `json:"provider"`
	Dimension     int       `json:"dimension"`
	Vector        []float64 `json:"vector"`
	ContentSHA256 string    `json:"content_sha256"`
	CreatedAt     time.Time `json:"created_at"`
}

type FileRecord struct {
	Path      string    `json:"path"`
	SHA256    string    `json:"sha256"`
	SizeBytes int64     `json:"size_bytes"`
	ModAt     time.Time `json:"modified_at"`
}

type Manifest struct {
	Version            int                       `json:"version"`
	WorkspaceRoot      string                    `json:"workspace_root"`
	WorkspaceID        string                    `json:"workspace_id"`
	EmbeddingProvider  string                    `json:"embedding_provider"`
	EmbeddingModel     string                    `json:"embedding_model"`
	EmbeddingDimension int                       `json:"embedding_dimension"`
	DefaultStrategy    string                    `json:"default_strategy"`
	IndexedAt          time.Time                 `json:"indexed_at"`
	Documents          int                       `json:"documents"`
	Strategies         map[string]StrategyRecord `json:"strategies"`
	Files              []FileRecord              `json:"files"`
}

type StrategyRecord struct {
	Chunks         int    `json:"chunks"`
	Path           string `json:"path"`
	EmbeddingsPath string `json:"embeddings_path"`
}

type Report struct {
	Corpus     CorpusReport   `json:"corpus"`
	Fixed      StrategyReport `json:"fixed"`
	Structural StrategyReport `json:"structural"`
	Summary    []string       `json:"summary"`
}

type CorpusReport struct {
	Documents              int   `json:"documents"`
	TextBytes              int64 `json:"text_bytes"`
	PageEquivalentEstimate int   `json:"page_equivalent_estimate"`
}

type StrategyReport struct {
	Chunks        int     `json:"chunks"`
	AvgTokens     float64 `json:"avg_tokens"`
	MinTokens     int     `json:"min_tokens"`
	MaxTokens     int     `json:"max_tokens"`
	OverlapTokens int     `json:"overlap_tokens,omitempty"`
	Sections      int     `json:"sections,omitempty"`
	FilesCovered  int     `json:"files_covered"`
}

type Status struct {
	Enabled         bool      `json:"enabled"`
	Ollama          string    `json:"ollama"`
	Model           string    `json:"model"`
	ModelReady      bool      `json:"model_ready"`
	Index           string    `json:"index"`
	IndexReady      bool      `json:"index_ready"`
	IndexStale      bool      `json:"index_stale"`
	Reason          string    `json:"reason,omitempty"`
	Next            string    `json:"next,omitempty"`
	Documents       int       `json:"documents,omitempty"`
	FixedChunks     int       `json:"fixed_chunks,omitempty"`
	Structural      int       `json:"structural_chunks,omitempty"`
	LastIndexedAt   time.Time `json:"last_indexed_at,omitempty"`
	WorkspaceRoot   string    `json:"workspace_root,omitempty"`
	WorkspaceID     string    `json:"workspace_id,omitempty"`
	DefaultStrategy string    `json:"default_strategy,omitempty"`
}

type SearchResult struct {
	Chunk Chunk   `json:"chunk"`
	Score float64 `json:"score"`
}

type RetrievalContext struct {
	Mode       string         `json:"mode"`
	Strategy   string         `json:"strategy"`
	Model      string         `json:"model,omitempty"`
	Warning    string         `json:"warning,omitempty"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	Results    []SearchResult `json:"results,omitempty"`
}

type IndexOptions struct {
	WorkspaceRoot string
	StorageRoot   string
	Model         string
	Provider      string
	FixedTokens   int
	OverlapTokens int
	MaxFileBytes  int64
	Now           time.Time
}

func normalizeOptions(in IndexOptions) IndexOptions {
	if in.Model == "" {
		in.Model = DefaultModel
	}
	if in.Provider == "" {
		in.Provider = DefaultProvider
	}
	if in.FixedTokens <= 0 {
		in.FixedTokens = DefaultFixedTokens
	}
	if in.OverlapTokens < 0 {
		in.OverlapTokens = DefaultOverlapTokens
	}
	if in.OverlapTokens >= in.FixedTokens {
		in.OverlapTokens = in.FixedTokens / 5
	}
	if in.MaxFileBytes <= 0 {
		in.MaxFileBytes = DefaultMaxFileBytes
	}
	if in.Now.IsZero() {
		in.Now = time.Now().UTC()
	}
	return in
}
