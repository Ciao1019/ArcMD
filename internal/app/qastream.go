package app

// Native port of LobeHub Streamdown's balanced smoother and char animation.
// See third_party/lobe-streaming for the pinned MIT sources and attribution.

import (
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

const qaFadeDuration = 180 * time.Millisecond

var qaFadeEase = ui.CubicBezier(0.33, 0, 0.67, 1)

// The raw answer remains authoritative. Only this display buffer is paced;
// network completion leaves it running; cancellation and reduced motion flush
// it. An SSE response may deliver hundreds of characters before one UI frame.
type qaStream struct {
	initialized           bool
	target, shown         string
	count, shownCount     int
	inputAt, commitAt     time.Time
	speed, chunk, arrival float64
}

func qaClamp(v, lo, hi float64) float64 { return min(hi, max(lo, v)) }

func (s *qaStream) sync(text string, now time.Time) {
	*s = qaStream{initialized: true, target: text, shown: text,
		count: utf8.RuneCountInString(text), shownCount: utf8.RuneCountInString(text),
		inputAt: now, speed: 38, chunk: 1, arrival: 38}
}

// step returns the next necessary wake, not a permanent animation ticker.
func (s *qaStream) step(text string, now time.Time, enabled bool, tailUnits int) time.Duration {
	if s.initialized && !enabled && text == s.shown && text == s.target {
		return 0
	}
	if !enabled || s.initialized && !strings.HasPrefix(text, s.target) {
		s.sync(text, now)
		return 0
	}
	if !s.initialized {
		s.sync("", now)
	}
	if text != s.target {
		n := utf8.RuneCountInString(text[len(s.target):])
		instant := float64(n) * 1000 / max(1, float64(now.Sub(s.inputAt))/float64(time.Millisecond))
		s.chunk = s.chunk*0.65 + float64(n)*0.35
		s.arrival = s.arrival*0.65 + qaClamp(instant, 18, 560)*0.35
		s.speed = s.speed*0.8 + qaClamp(instant, 18, 132)*0.2
		s.target, s.count, s.inputAt = text, s.count+n, now
	}
	backlog := s.count - s.shownCount
	if backlog <= 0 {
		s.commitAt = time.Time{}
		return 0
	}
	interval := time.Duration(min(96, 48*(1+float64(tailUnits)/256)) * float64(time.Millisecond))
	if s.commitAt.IsZero() {
		s.commitAt = now
	}
	dt := now.Sub(s.commitAt)
	if dt < interval {
		return interval - dt
	}
	s.commitAt = now
	idle := now.Sub(s.inputAt)
	active := idle <= 220*time.Millisecond
	base := qaClamp(s.speed, 18, 72)
	baseLag := max(1, math.Round(base*0.12))
	lag := 0.0
	cps := 0.0
	if active {
		lag = math.Round(qaClamp(baseLag+s.chunk*0.35, baseLag, max(baseLag+2, baseLag*3)))
		pressure := qaClamp(float64(backlog)/lag*0.6+s.chunk/lag*0.25+s.arrival/base*0.15, 1, 4.5)
		cps = qaClamp(base*pressure, 18, qaClamp(132+s.chunk*6, 132, 280))
	} else if idle >= 360*time.Millisecond {
		cps = qaClamp(float64(backlog)*1000/qaClamp(float64(backlog)*8, 180, 520), 120, 280)
	} else {
		cps = qaClamp(max(120, base*1.8, s.arrival*0.8), 120, 280)
	}
	minimum := 2
	if active && float64(backlog) <= lag*2.2 && s.chunk < lag*0.9 {
		minimum = 1
	}
	reveal := max(minimum, int(math.Round(cps*qaClamp(dt.Seconds(), 0.001, 0.12))))
	if active {
		shortfall := backlog - int(lag)
		if shortfall <= 0 {
			s.commitAt = time.Time{}
			return max(time.Millisecond, 220*time.Millisecond-idle)
		}
		reveal = min(reveal, shortfall)
	}
	reveal = min(reveal, backlog)
	end := len(s.shown)
	for range reveal {
		_, size := utf8.DecodeRuneInString(text[end:])
		end += size
	}
	// Do not bypass pacing for large chunks, HTML fences or complete
	// paragraphs. Unlike the playground's 2–8 character producer, real
	// providers/proxies can batch the entire answer into one delivery.
	s.shownCount += utf8.RuneCountInString(text[len(s.shown):end])
	s.shown = text[:end]
	if s.shownCount == s.count {
		s.commitAt = time.Time{}
		return 0
	}
	return interval
}

type qaFade struct {
	births      []time.Time
	blockBirth  time.Time
	rawCount    int
	charDelay   time.Duration
	initialized bool
	settled     int
}

// Births use rendered character indices, never Markdown byte offsets. Closing
// a delimiter or finishing a URL must not restart the words already shown.
func (f *qaFade) update(now time.Time, count, rawCount int, tail, enabled bool, gap time.Duration) {
	if !enabled {
		f.births, f.blockBirth = nil, time.Time{}
		f.rawCount, f.initialized = rawCount, true
		f.settled = 0
		return
	}
	if !f.initialized {
		f.charDelay = min(18*time.Millisecond, 3*time.Second/time.Duration(max(1, rawCount)))
		if !tail {
			f.blockBirth = now
		}
		f.initialized = true
	}
	if len(f.births) > count {
		f.births = f.births[:count]
	}
	f.settled = min(f.settled, len(f.births))
	for f.settled < len(f.births) && now.Sub(f.births[f.settled]) >= qaFadeDuration {
		f.settled++
	}
	gap = min(160*time.Millisecond, max(16*time.Millisecond, gap))
	pace, capTime := f.charDelay, qaFadeDuration
	if tail {
		pace = min(pace, max(2*time.Millisecond, gap/time.Duration(max(1, rawCount-f.rawCount))))
		capTime += gap
	}
	for len(f.births) < count {
		birth := now
		if len(f.births) > 0 {
			birth = f.births[len(f.births)-1].Add(pace)
			if birth.Before(now) {
				birth = now
			}
		}
		if limit := now.Add(capTime); birth.After(limit) {
			birth = limit
		}
		f.births = append(f.births, birth)
	}
	f.rawCount = rawCount
}

func qaFadeAlpha(now, birth time.Time) float32 {
	if birth.IsZero() {
		return 1
	}
	return qaFadeEase(min(1, max(0, float32(now.Sub(birth))/float32(qaFadeDuration))))
}

// MyGo uses an 8-bit alpha; zero means an inherited span color. Keep at
// least one alpha unit so the first frame cannot flash fully opaque text.
func qaFadeInk(ink ui.Color, opacity float32) ui.Color {
	ink = ink.Alpha(opacity)
	ink.A = max(1, ink.A)
	return ink
}

func (f *qaFade) alpha(now time.Time, index int) float32 {
	if !f.blockBirth.IsZero() {
		return qaFadeAlpha(now, f.blockBirth)
	}
	if index >= len(f.births) {
		return 1
	}
	return qaFadeAlpha(now, f.births[index])
}

func (f *qaFade) active(now time.Time) bool {
	if !f.blockBirth.IsZero() {
		return now.Sub(f.blockBirth) < qaFadeDuration
	}
	return len(f.births) > 0 && now.Sub(f.births[len(f.births)-1]) < qaFadeDuration
}

func (t *qaTurn) displayAnswer() string {
	if t.stream.initialized {
		return t.stream.shown
	}
	return t.answer
}

func (t *qaTurn) prepareStream(c *ui.Context) {
	if !t.stream.initialized && !t.revealing {
		t.stream.sync(t.answer, c.Now())
	}
	if t.stopped || t.err != nil || c.Preferences().ReduceMotion {
		t.revealing = false
	}
	if t.revealing && !t.running && t.stream.shown == t.answer &&
		t.view.animated && t.view.animatedSource == t.answer && !t.view.fading(c.Now()) {
		t.revealing = false
	}
	if delay := t.stream.step(t.answer, c.Now(), t.animateAnswer(c), t.view.tailUnits); delay > 0 {
		c.After(delay)
	}
	if t.revealing && !t.running {
		c.After(time.Second / 60)
	}
}

func (t *qaTurn) animateAnswer(c *ui.Context) bool {
	return (t.running || t.revealing) && !t.stopped && t.err == nil && !c.Preferences().ReduceMotion
}

func (v *qaTurnView) fading(now time.Time) bool {
	for _, b := range v.blocks {
		if b.fade != nil && b.fade.active(now) {
			return true
		}
	}
	return false
}
