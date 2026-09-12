package store

import (
	"fmt"
	"strings"
	"time"
)

// # Files near each other in meaning
//
// Search finds a file when you know a word in it. This finds one when you do
// not: the second copy of a contract under a different name, the other three
// invoices from the same supplier, the version of the form somebody filled in.
//
// It reads the text that extraction and OCR produced, so a file nothing has
// read cannot participate — which is the honest constraint and worth stating,
// because "no similar files" and "this file has never been read" look
// identical from the outside.

// AttachmentEmbedding is one file's vector.
type AttachmentEmbedding struct {
	ContentHash string
	Profile     string
	Model       string
	Dimensions  int
	Vector      []float32
	Characters  int
}

// SaveAttachmentEmbedding stores one vector.
func SaveAttachmentEmbedding(e *AttachmentEmbedding) error {
	if len(e.Vector) == 0 {
		return fmt.Errorf("refusing to store an empty vector for %s", e.ContentHash)
	}
	_, err := db.Exec(`
		INSERT INTO attachment_embeddings
			(content_hash, profile, model, dimensions, vector, characters, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(content_hash) DO UPDATE SET
			profile = excluded.profile, model = excluded.model,
			dimensions = excluded.dimensions, vector = excluded.vector,
			characters = excluded.characters, created_at = excluded.created_at`,
		e.ContentHash, e.Profile, e.Model, len(e.Vector), encodeVector(e.Vector),
		e.Characters, time.Now().UnixMilli())
	return err
}

// AttachmentEmbeddingInput is a file waiting to be embedded, with its text.
type AttachmentEmbeddingInput struct {
	ContentHash string
	Filename    string
	Text        string
}

// maxEmbeddingChars bounds what is sent for one file.
//
// An embedding model has a context window, and a ninety-page contract exceeds
// every one of them. Truncating is not a compromise so much as an admission:
// a single vector cannot represent a long document, and the first several
// thousand characters — title, parties, subject matter — is the part that
// makes one document recognisably the same as another. The stored character
// count records how much went in, so nothing downstream has to guess.
const maxEmbeddingChars = 8000

// PendingAttachmentEmbeddings lists files this model has not embedded yet.
//
// Keyed on the model rather than on presence, exactly as the message side is:
// a vector made by a different model is not a vector for this one.
func PendingAttachmentEmbeddings(model string, limit int) ([]AttachmentEmbeddingInput, error) {
	sql := `
		SELECT e.content_hash,
		       COALESCE((SELECT a.filename FROM attachments a
		                 WHERE a.content_hash = e.content_hash LIMIT 1), ''),
		       (SELECT GROUP_CONCAT(t.text, '
')
		        FROM (SELECT text FROM attachment_text
		              WHERE content_hash = e.content_hash ORDER BY page LIMIT 40) t)
		FROM attachment_extractions e
		WHERE e.status = 'ok'
		  AND NOT EXISTS (SELECT 1 FROM attachment_embeddings v
		                  WHERE v.content_hash = e.content_hash AND v.model = ?)
		ORDER BY e.content_hash`
	args := []any{model}
	if limit > 0 {
		sql += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := db.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AttachmentEmbeddingInput
	for rows.Next() {
		var in AttachmentEmbeddingInput
		var text any
		if err := rows.Scan(&in.ContentHash, &in.Filename, &text); err != nil {
			return nil, err
		}
		if s, ok := text.(string); ok {
			in.Text = s
		}
		// The filename leads the text. It is often the most identifying thing
		// about a document — "gtri_fmla_leave_request_form" says more than the
		// first paragraph of the form does — and a file whose text is a page of
		// boilerplate would otherwise embed as indistinguishable from every
		// other page of the same boilerplate.
		in.Text = strings.TrimSpace(in.Filename + "\n" + in.Text)
		if len(in.Text) > maxEmbeddingChars {
			in.Text = in.Text[:maxEmbeddingChars]
		}
		if in.Text == "" {
			continue
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// AttachmentEmbeddingCoverage reports how many readable files a model has
// embedded.
type AttachmentEmbeddingCoverage struct {
	Model      string `json:"model"`
	Embedded   int    `json:"embedded"`
	Readable   int    `json:"readable"`
	Dimensions int    `json:"dimensions"`
}

// AttachmentCoverage reports embedding coverage per model.
func AttachmentCoverage() ([]AttachmentEmbeddingCoverage, error) {
	var readable int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM attachment_extractions WHERE status = 'ok'").Scan(&readable); err != nil {
		return nil, err
	}

	rows, err := db.Query(`
		SELECT model, COUNT(*), MAX(dimensions)
		FROM attachment_embeddings GROUP BY model ORDER BY model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AttachmentEmbeddingCoverage
	for rows.Next() {
		c := AttachmentEmbeddingCoverage{Readable: readable}
		if err := rows.Scan(&c.Model, &c.Embedded, &c.Dimensions); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SimilarAttachments returns the files nearest one in meaning.
//
// The same exact scan the message side uses, and for the same reasons: SQLite
// has no vector index without a cgo extension, and at the scale of the files
// in a personal mailbox an exact answer costs milliseconds.
func SimilarAttachments(key string, threshold float64, limit int) ([]AttachmentNeighbour, error) {
	var model string
	var dims int
	var raw []byte
	err := db.QueryRow(
		"SELECT model, dimensions, vector FROM attachment_embeddings WHERE content_hash = ?",
		key).Scan(&model, &dims, &raw)
	if err != nil {
		return nil, fmt.Errorf("no embedding for %s: %w", key, err)
	}
	target := decodeVector(raw)
	normaliseVector(target)

	rows, err := db.Query(`
		SELECT content_hash, vector FROM attachment_embeddings
		WHERE model = ? AND dimensions = ? AND content_hash != ?`, model, dims, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AttachmentNeighbour
	for rows.Next() {
		var hash string
		var blob []byte
		if err := rows.Scan(&hash, &blob); err != nil {
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
		out = append(out, AttachmentNeighbour{ContentHash: hash, Similarity: sim})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Similarity > out[j-1].Similarity; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// AttachmentNeighbour is one nearby file.
type AttachmentNeighbour struct {
	ContentHash string  `json:"contentHash"`
	Similarity  float64 `json:"similarity"`
}

// similarAttachmentKeys resolves a file to its neighbours, for the compiler.
//
// Returns the target as well as its neighbours: `similar:x` meaning "things
// like x, but not x" would make the result of clicking a file exclude the file
// clicked, which reads as the wrong answer rather than a definition.
func similarAttachmentKeys(key string, threshold float64) ([]string, error) {
	if threshold < 0 {
		threshold = DefaultSimilarityThreshold()
	}
	// Capped for the same reason the message side is: `similar:` is a filter,
	// and an unbounded one would put every key into a single IN clause.
	neighbours, err := SimilarAttachments(key, threshold, 500)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(neighbours)+1)
	out = append(out, key)
	for _, n := range neighbours {
		out = append(out, n.ContentHash)
	}
	return out, nil
}
