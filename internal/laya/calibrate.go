package laya

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
)

// # Why a temperature has to be fitted before a score means anything
//
// The published checkpoint ships with every temperature at 1.0, and its authors
// measure its expected calibration error at 0.466. Their own source says the
// shipped weights are over-confident and that a threshold applied to them
// "selects below model accuracy".
//
// That is visible on this mailbox within thirty messages: a software newsletter
// scored 0.993 on "is this a receipt". So a raw score orders messages — a 0.95
// is more likely right than a 0.6 — and does not mean "95% of these are right".
// `label:x@0.9` against raw scores is a ranking cut wearing the clothes of a
// probability.
//
// Temperature scaling is the standard repair and the cheapest one: divide the
// logits by a single number before the softmax, chosen so that the reported
// confidence matches the observed frequency. It cannot make the model more
// accurate — the ordering is unchanged, so every answer stays the answer — it
// only makes the number honest.
//
// # Where the labels come from
//
// Human corrections, which the annotation store already keeps and already
// ranks above machine results. Nothing new is asked of anyone: the rulings
// somebody made because a label was wrong are exactly the held-out set this
// needs.
//
// It follows that an annotator nobody has corrected cannot be calibrated, and
// this says so rather than fitting to nothing.

// Temperature is a fitted scale for one annotator's scores.
type Temperature struct {
	// Value divides the logits. Above 1 softens an over-confident model, which
	// is the direction this checkpoint needs; below 1 sharpens.
	Value float64 `json:"value"`
	// N is how many human rulings it was fitted on. Small is not an error, but
	// it is worth showing next to the number.
	N int `json:"n"`
	// ECEBefore and ECEAfter are the expected calibration error either side of
	// the fit, so the improvement can be seen rather than assumed.
	ECEBefore float64 `json:"eceBefore"`
	ECEAfter  float64 `json:"eceAfter"`
}

// MinCalibrationSamples is the fewest rulings worth fitting on.
//
// Twenty is not a statistical claim; it is the point below which a fitted
// temperature is mostly describing which twelve messages somebody happened to
// correct. Under it, refusing and saying so is more useful than a number that
// invites trust.
const MinCalibrationSamples = 20

// Observation is one human ruling against what the model said.
type Observation struct {
	// Probability is what the model reported for the answer it gave.
	Probability float64
	// Correct is whether the person agreed with it.
	Correct bool
}

// FitTemperature finds the scale that makes reported confidence match observed
// accuracy.
//
// A one-dimensional search rather than gradient descent: the objective is
// smooth and bounded, there is one parameter, and a grid over a plausible range
// is both exact enough and impossible to get subtly wrong.
func FitTemperature(obs []Observation) (*Temperature, error) {
	if len(obs) < MinCalibrationSamples {
		ruling := "rulings"
		if len(obs) == 1 {
			ruling = "ruling"
		}
		return nil, fmt.Errorf(
			"only %d human %s; %d is the fewest worth fitting on.\n"+
				"Correct more of its answers with `iql annotate correct`, then try again",
			len(obs), ruling, MinCalibrationSamples)
	}

	best, bestECE := 1.0, math.Inf(1)
	for t := 0.25; t <= 8.0; t += 0.01 {
		if e := eceAt(obs, t); e < bestECE {
			best, bestECE = t, e
		}
	}
	return &Temperature{
		Value: math.Round(best*100) / 100, N: len(obs),
		ECEBefore: eceAt(obs, 1.0), ECEAfter: bestECE,
	}, nil
}

// eceAt is the expected calibration error with a given temperature applied.
//
// Ten equal-width bins over the probability range, each contributing the gap
// between its mean confidence and its accuracy, weighted by how many
// observations fell in it. That is the standard definition, and the one the
// upstream project reports against, so the numbers are comparable to theirs.
func eceAt(obs []Observation, t float64) float64 {
	const bins = 10
	var sum, n float64
	type bin struct {
		conf, acc, n float64
	}
	b := make([]bin, bins)
	for _, o := range obs {
		p := rescale(o.Probability, t)
		i := int(p * bins)
		if i >= bins {
			i = bins - 1
		}
		b[i].conf += p
		if o.Correct {
			b[i].acc++
		}
		b[i].n++
		n++
	}
	for _, x := range b {
		if x.n == 0 {
			continue
		}
		sum += x.n * math.Abs(x.conf/x.n-x.acc/x.n)
	}
	if n == 0 {
		return 0
	}
	return sum / n
}

// rescale applies a temperature to a two-outcome probability.
//
// Through the logit, because that is where a temperature acts: dividing the
// probability directly would not keep it in range and would not be the same
// transformation the model's own temperature field performs.
func rescale(p, t float64) float64 {
	p = math.Min(math.Max(p, 1e-9), 1-1e-9)
	logit := math.Log(p / (1 - p))
	return 1 / (1 + math.Exp(-logit/t))
}

// Rescale applies a fitted temperature to a reported probability.
func (t *Temperature) Rescale(p float64) float64 {
	if t == nil || t.Value <= 0 {
		return p
	}
	return rescale(p, t.Value)
}

// temperatureFile is where a fitted temperature lives, keyed by annotator.
const temperatureFile = "temperatures.json"

// LoadTemperatures reads the fitted temperatures for a data directory.
func LoadTemperatures(dataDir string) map[string]*Temperature {
	out := map[string]*Temperature{}
	blob, err := os.ReadFile(filepath.Join(Dir(dataDir), temperatureFile))
	if err != nil {
		return out
	}
	_ = json.Unmarshal(blob, &out)
	return out
}

// SaveTemperature records a fit for one annotator.
//
// Beside the weights rather than in the database, because it describes this
// checkpoint: replacing the model invalidates every fit, and deleting the model
// directory should take them with it rather than leaving stale scales behind to
// be applied to different weights.
func SaveTemperature(dataDir, annotator string, t *Temperature) error {
	all := LoadTemperatures(dataDir)
	all[annotator] = t
	blob, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(Dir(dataDir), temperatureFile), blob, 0o644)
}

// SortedNames lists the annotators with a fitted temperature.
func SortedNames(all map[string]*Temperature) []string {
	out := make([]string, 0, len(all))
	for k := range all {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
