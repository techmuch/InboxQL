package laya

import (
	"math"
	"testing"
)

// An over-confident model is the case this exists for: the published
// checkpoint reports 0.99 on answers that are right about three quarters of
// the time, and the fit should soften those scores rather than leave them.
func TestFitSoftensAnOverConfidentModel(t *testing.T) {
	var obs []Observation
	// 40 answers reported at 0.98; 30 of them right. A calibrated model would
	// have said about 0.75.
	for i := 0; i < 40; i++ {
		obs = append(obs, Observation{Probability: 0.98, Correct: i < 30})
	}
	got, err := FitTemperature(obs)
	if err != nil {
		t.Fatal(err)
	}
	if got.Value <= 1.0 {
		t.Errorf("temperature %.2f does not soften an over-confident model", got.Value)
	}
	if got.ECEAfter >= got.ECEBefore {
		t.Errorf("calibration did not improve: %.3f → %.3f", got.ECEBefore, got.ECEAfter)
	}
	// The whole point: after the fit, the reported number is close to the
	// observed frequency.
	if p := got.Rescale(0.98); math.Abs(p-0.75) > 0.1 {
		t.Errorf("0.98 rescales to %.3f, want about 0.75", p)
	}
}

// A model that is already honest should be left roughly alone, rather than
// having a scale invented for it.
func TestFitLeavesACalibratedModelAlone(t *testing.T) {
	var obs []Observation
	for i := 0; i < 40; i++ {
		obs = append(obs, Observation{Probability: 0.75, Correct: i < 30})
	}
	got, err := FitTemperature(obs)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got.Value-1.0) > 0.25 {
		t.Errorf("temperature %.2f on an already calibrated model", got.Value)
	}
}

// Rescaling must not reorder. Temperature scaling cannot make the model more
// accurate — every answer stays the answer — and a fit that changed which
// option won would be a bug, not an improvement.
func TestRescalePreservesOrder(t *testing.T) {
	temp := &Temperature{Value: 2.7}
	prev := -1.0
	for _, p := range []float64{0.01, 0.1, 0.3, 0.49, 0.51, 0.7, 0.9, 0.99} {
		got := temp.Rescale(p)
		if got <= prev {
			t.Errorf("rescale(%.2f) = %.4f, which is not above the previous %.4f", p, got, prev)
		}
		// A probability either side of a half must stay on its side, or the
		// answer itself changes.
		if (p > 0.5) != (got > 0.5) {
			t.Errorf("rescale(%.2f) = %.4f crossed the decision boundary", p, got)
		}
		prev = got
	}
}

// Too few rulings is refused rather than fitted. A temperature from eight
// corrections mostly describes which eight messages somebody happened to look
// at, and a number that invites trust is worse than no number.
func TestFitRefusesTooFewRulings(t *testing.T) {
	var obs []Observation
	for i := 0; i < MinCalibrationSamples-1; i++ {
		obs = append(obs, Observation{Probability: 0.9, Correct: true})
	}
	if _, err := FitTemperature(obs); err == nil {
		t.Fatal("fitted a temperature on too few rulings")
	}
}

// A nil temperature is the uncalibrated state and must pass scores through
// untouched, so an annotator nobody has corrected behaves exactly as before.
func TestNilTemperatureIsTransparent(t *testing.T) {
	var temp *Temperature
	if got := temp.Rescale(0.42); got != 0.42 {
		t.Errorf("nil temperature changed %v to %v", 0.42, got)
	}
}

func TestCalibrateRewritesBothSidesOfTheDistribution(t *testing.T) {
	d := &Decision{
		Label: "true", Index: 1, Probability: 0.98,
		Probabilities: []float64{0.02, 0.98},
	}
	d.Calibrate(&Temperature{Value: 3.0})

	if d.Probabilities[0]+d.Probabilities[1] < 0.999 {
		t.Errorf("probabilities no longer sum to one: %v", d.Probabilities)
	}
	if d.Probability != d.Probabilities[1] {
		t.Errorf("Probability %v does not match the distribution %v", d.Probability, d.Probabilities)
	}
	if d.Probability >= 0.98 {
		t.Errorf("a softening temperature did not lower %v", d.Probability)
	}
	if d.Label != "true" || d.Index != 1 {
		t.Errorf("calibration changed the answer: %+v", d)
	}
}
