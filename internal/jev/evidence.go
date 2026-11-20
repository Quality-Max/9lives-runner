package jev

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"math"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

const maxImageBytes = 16 << 20
const maxSnapshotBytes = 16 << 10

// Binding is supplied by the owning engine, not inferred from page content.
// SourceSHA256 covers the exact source including uncommitted edits.
type Binding struct {
	RunID         string `json:"runId"`
	JobID         string `json:"jobId"`
	AttemptID     string `json:"attemptId"`
	Target        string `json:"target"`
	Revision      string `json:"revision"`
	SourceSHA256  string `json:"sourceSHA256"`
	TestID        string `json:"testId"`
	StepID        string `json:"stepId"`
	ObservationID string `json:"observationId"`
}

func (b Binding) validate() bool {
	for _, id := range []string{b.RunID, b.JobID, b.AttemptID, b.Target, b.TestID, b.StepID, b.ObservationID} {
		if !identifier.MatchString(id) {
			return false
		}
	}
	return revision.MatchString(b.Revision) && len(b.SourceSHA256) == 64 && revision.MatchString(b.SourceSHA256)
}

type Observation struct {
	Binding    Binding   `json:"binding"`
	ObservedAt time.Time `json:"observedAt"`
	Snapshot   string    `json:"-"`
	// FailureClass is categorical; original errors remain with the runner.
	FailureClass string `json:"failureClass,omitempty"`
	Image        *Image `json:"image,omitempty"`
}

// Transform maps uploaded image pixels to viewport CSS coordinates:
// xCSS = xPixel*ScaleX + OffsetX (and likewise for y). No actions use it here.
type Transform struct {
	ScaleX  float64 `json:"scaleX"`
	ScaleY  float64 `json:"scaleY"`
	OffsetX float64 `json:"offsetX"`
	OffsetY float64 `json:"offsetY"`
}

func (t Transform) valid() bool {
	for _, v := range []float64{t.ScaleX, t.ScaleY, t.OffsetX, t.OffsetY} {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e7 {
			return false
		}
	}
	return t.ScaleX > 0 && t.ScaleY > 0
}

// Image has no exported pixel buffer. PrepareImage copies, masks and re-encodes
// pixels, removing metadata. JSON persistence includes metadata only.
type Image struct {
	record ImageRecord
	data   []byte
}

type ImageRecord struct {
	Binding       Binding   `json:"binding"`
	ObservedAt    time.Time `json:"observedAt"`
	SHA256        string    `json:"sha256"`
	Width         int       `json:"width"`
	Height        int       `json:"height"`
	Bytes         int       `json:"bytes"`
	PixelPolicy   string    `json:"pixelPolicy"`
	MaskedRegions int       `json:"maskedRegions"`
	Transform     Transform `json:"transform"`
}

func (i Image) MarshalJSON() ([]byte, error) { return marshal(i.record) }
func (i Image) Record() ImageRecord          { return i.record }

// PrepareImage accepts upright still PNG/JPEG only. It never resizes; callers
// preparing resized/cropped images must supply their exact coordinate transform.
// "masked" requires actual rectangles in uploaded-image pixels; "synthetic"
// is an explicit assertion by the caller that the pixels contain no real data.
func PrepareImage(binding Binding, observedAt time.Time, raw []byte, transform Transform, policy string, masks []image.Rectangle) (*Image, error) {
	invalid := errors.New("invalid Jev image evidence")
	if !binding.validate() || observedAt.IsZero() || len(raw) == 0 || len(raw) > maxImageBytes || !transform.valid() || len(masks) > 64 || (policy != "masked" && policy != "synthetic") || (policy == "masked" && len(masks) == 0) {
		return nil, invalid
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || (format != "png" && format != "jpeg") || config.Width < 280 || config.Height < 280 || config.Width > 1568 || config.Height > 1568 {
		return nil, invalid
	}
	if format == "png" && animatedPNG(raw) {
		return nil, invalid
	}
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, invalid
	}
	pixels := image.NewNRGBA(image.Rect(0, 0, config.Width, config.Height))
	draw.Draw(pixels, pixels.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	for _, mask := range masks {
		if mask.Empty() || !mask.In(pixels.Bounds()) {
			return nil, invalid
		}
		draw.Draw(pixels, mask, image.NewUniform(color.Black), image.Point{}, draw.Src)
	}
	var encoded bytes.Buffer
	if png.Encode(&encoded, pixels) != nil || encoded.Len() > maxImageBytes {
		return nil, invalid
	}
	data := encoded.Bytes()
	return &Image{record: ImageRecord{Binding: binding, ObservedAt: observedAt, SHA256: hash(data), Width: config.Width, Height: config.Height, Bytes: len(data), PixelPolicy: policy, MaskedRegions: len(masks), Transform: transform}, data: data}, nil
}

func animatedPNG(raw []byte) bool {
	// Walk chunks rather than looking for strings inside compressed pixels.
	for at := 8; at+12 <= len(raw); {
		n := uint64(binary.BigEndian.Uint32(raw[at : at+4]))
		if n+12 > uint64(len(raw)-at) {
			return true
		}
		if string(raw[at+4:at+8]) == "acTL" {
			return true
		}
		at += int(n) + 12
	}
	return false
}

func hash(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func (o Observation) validate(expected Binding, now time.Time, maxAge time.Duration, images string) error {
	if !expected.validate() || o.Binding != expected || o.ObservedAt.IsZero() || o.ObservedAt.After(now) || now.Sub(o.ObservedAt) > maxAge || !boundedText(o.Snapshot, maxSnapshotBytes) {
		return errors.New("missing, stale or foreign Jev observation")
	}
	switch o.FailureClass {
	case "", runner.FailureAssertion, runner.FailureTimeout, runner.FailureActionTimeout, runner.FailureEnvironment, runner.FailureUnexpectedPass, runner.FailureOther:
	default:
		return errors.New("invalid Jev failure class")
	}
	if o.Image != nil {
		i := o.Image
		if images == "off" || images != i.record.PixelPolicy || i.record.Binding != expected || !i.record.ObservedAt.Equal(o.ObservedAt) || len(i.data) == 0 || i.record.SHA256 != hash(i.data) {
			return errors.New("disabled, missing or foreign Jev image")
		}
	}
	return nil
}
