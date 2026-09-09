package store

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// Embedding is one message's vector, with the model that produced it.
type Embedding struct {
	MessageID  string
	Profile    string
	Model      string
	Dimensions int
	Vector     []float32
}

// SaveEmbedding stores one vector.
func SaveEmbedding(e *Embedding) error {
	if len(e.Vector) == 0 {
		return fmt.Errorf("refusing to store an empty vector for %s", e.MessageID)
	}
	_, err := db.Exec(`
		INSERT INTO message_embeddings (message_id, profile, model, dimensions, vector, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(message_id) DO UPDATE SET
			profile = excluded.profile, model = excluded.model,
			dimensions = excluded.dimensions, vector = excluded.vector,
			created_at = excluded.created_at`,
		e.MessageID, e.Profile, e.Model, len(e.Vector), encodeVector(e.Vector),
		time.Now().UnixMilli())
	return err
}

// PendingEmbeddings lists messages this model has not embedded yet.
//
// Keyed on the model rather than on presence: a vector made by a different
// model is not a vector for this one, so switching models means re-embedding
// rather than believing what is already there.
func PendingEmbeddings(model string, scope string, limit int) ([]string, error) {
	where := "1=1"
	var args []any
	if scope != "" {
		clause, scopeArgs, err := filterClause(scope)
		if err != nil {
			return nil, err
		}
		where, args = clause, scopeArgs
	}

	sql := `SELECT m.id FROM messages m
		WHERE ` + where + `
		  AND NOT EXISTS (SELECT 1 FROM message_embeddings e
		                  WHERE e.message_id = m.id AND e.model = ?)
		ORDER BY m.date DESC`
	args = append(args, model)
	if limit > 0 {
		sql += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := db.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// EmbeddingCoverage reports how much of the mailbox a model has embedded.
type EmbeddingCoverage struct {
	Model      string `json:"model"`
	Dimensions int    `json:"dimensions"`
	Embedded   int64  `json:"embedded"`
	Total      int64  `json:"total"`
}

// Coverage reports what has been embedded, by model.
//
// By model and not in total, because a mailbox half-embedded by one model and
// half by another has no usable vectors at all — they cannot be compared —
// and a single "80% done" would hide that completely.
func Coverage() ([]EmbeddingCoverage, error) {
	var total int64
	if err := db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&total); err != nil {
		return nil, err
	}

	rows, err := db.Query(`
		SELECT model, dimensions, COUNT(*) FROM message_embeddings
		GROUP BY model, dimensions ORDER BY COUNT(*) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []EmbeddingCoverage{}
	for rows.Next() {
		c := EmbeddingCoverage{Total: total}
		if err := rows.Scan(&c.Model, &c.Dimensions, &c.Embedded); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Neighbour is one similar message.
type Neighbour struct {
	MessageID  string  `json:"messageId"`
	Similarity float64 `json:"similarity"`
}

// SimilarTo returns the messages nearest a given one, above a threshold.
//
// # Why a full scan
//
// SQLite has no vector index without an extension, and sqlite-vec is cgo and a
// distribution burden. A scan over float32 blobs is a few hundred milliseconds
// at a hundred thousand messages, which is past the size of a personal
// mailbox — and it is exact, where an index would be approximate.
//
// Only vectors from the same model are considered. Comparing across models is
// not a worse answer, it is a meaningless one.
func SimilarTo(messageID string, threshold float64, limit int) ([]Neighbour, error) {
	var model string
	var dims int
	var raw []byte
	err := db.QueryRow(
		"SELECT model, dimensions, vector FROM message_embeddings WHERE message_id = ?",
		messageID).Scan(&model, &dims, &raw)
	if err != nil {
		return nil, fmt.Errorf("no embedding for %s: %w", messageID, err)
	}
	target := decodeVector(raw)
	normaliseVector(target)

	rows, err := db.Query(
		"SELECT message_id, vector FROM message_embeddings WHERE model = ? AND dimensions = ? AND message_id != ?",
		model, dims, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Neighbour
	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, err
		}
		v := decodeVector(blob)
		if len(v) != len(target) {
			continue
		}
		normaliseVector(v)
		sim := dot(target, v)
		if sim < threshold {
			continue
		}
		out = append(out, Neighbour{MessageID: id, Similarity: sim})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sortNeighbours(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// SimilarityHistogram reports how many neighbours a message has at each
// threshold.
//
// The number beside the slider. A similarity threshold is not portable between
// models and cosine values cluster in a narrow band, so a bare "0.75" tells
// nobody anything — the distribution is what makes the choice a decision
// rather than a guess.
func SimilarityHistogram(messageID string, buckets []float64) (map[string]int, error) {
	all, err := SimilarTo(messageID, -1, 0)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, b := range buckets {
		n := 0
		for _, nb := range all {
			if nb.Similarity >= b {
				n++
			}
		}
		out[fmt.Sprintf("%.2f", b)] = n
	}
	return out, nil
}

func sortNeighbours(in []Neighbour) {
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j].Similarity > in[j-1].Similarity; j-- {
			in[j-1], in[j] = in[j], in[j-1]
		}
	}
}

// encodeVector packs float32s little-endian.
func encodeVector(v []float32) []byte {
	out := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(f))
	}
	return out
}

func decodeVector(b []byte) []float32 {
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}

// normaliseVector scales to unit length so a dot product is the cosine.
//
// Done on read rather than on write: a stored vector is what the model
// returned, and normalising before storage would throw away the magnitude with
// no way to get it back.
func normaliseVector(v []float32) {
	var sum float64
	for _, f := range v {
		sum += float64(f) * float64(f)
	}
	if sum == 0 {
		return
	}
	inv := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
}

func dot(a, b []float32) float64 {
	var sum float64
	for i := range a {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}
