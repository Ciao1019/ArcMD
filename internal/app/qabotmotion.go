package app

import (
	"math"
	"time"
)

// Native port of bot-avatars' engine.ts (Jakub Antalik, MIT), pinned in
// third_party/bot-avatars. The simulation is independent of its renderer:
// pointer following, eye darts, blinks, jumps and state transitions stay live.
const botDeg = math.Pi / 180
const botTau = 2 * math.Pi

type botMode uint8

const (
	botIdle botMode = iota
	botWorking
	botSleeping
)

type botPose struct {
	Yaw, Pitch, Roll, X, Y, SX, SY        float64
	EyeOpen, BlinkL, BlinkR, LookX, LookY float64
	Breath, Laugh, Whirl, WhirlAngle      float64
	W                                     [3]float64
}

type botRest struct{ pitch, roll, y, lookX, lookY float64 }

var botRests = [3]botRest{{}, {pitch: 5 * botDeg}, {pitch: -16 * botDeg, roll: 6 * botDeg, y: 3, lookY: 1}}

func botRestPose(state botMode) botPose {
	r := botRests[state]
	p := botPose{Pitch: r.pitch, Roll: r.roll, Y: r.y, SX: 1, SY: 1, EyeOpen: 1, LookX: r.lookX, LookY: r.lookY}
	p.W[state] = 1
	return p
}

func botApproach(cur, target, rate, dt float64) float64 {
	return cur + (target-cur)*(1-math.Exp(-rate*dt))
}
func botEase(p float64) float64 {
	if p < .5 {
		return 4 * p * p * p
	}
	return 1 - math.Pow(-2*p+2, 3)/2
}
func botSine(p float64) float64          { return .5 - .5*math.Cos(math.Pi*p) }
func botClamp(v, lo, hi float64) float64 { return max(lo, min(hi, v)) }
func botHopSquash(a float64) float64 {
	return math.Exp(-math.Pow(min(math.Abs(a), math.Abs(a-1))/.11, 2))
}

// The stock pulse easing, used for crouch, ground hold and recovery.
func botSquash(u float64) float64 {
	if u <= 0 || u >= 1 {
		return 0
	}
	k := 7 * u
	x := k * k * math.Exp(2-k) / 4
	tail := 1.0
	if u > .85 {
		tail = 1 - (u-.85)/.15
	}
	return x * tail * tail * (3 - 2*tail)
}
func botGround(u float64) float64 { return 1 + .25*botSquash(u) }
func botRise(v float64) float64 {
	if v <= 0 {
		return 1
	}
	if v >= 1 {
		return 0
	}
	k := 4.2 * v
	return (1+k)*math.Exp(-k) - v*v*v*.078
}
func botFlipPre(poked bool) float64 {
	if poked {
		return .24
	}
	return .2 * .68
}
func botFlipDuration(poked bool) float64 {
	squash := .37
	if poked {
		squash = .24
	}
	return botFlipPre(poked) + .68 + (2.0/7)*squash + .11 + .33 + .05
}

type botEvent struct{ p, duration float64 }

func botNewEvent(d float64) botEvent { return botEvent{-1, d} }
func (e *botEvent) active() bool     { return e.p >= 0 }
func (e *botEvent) fire()            { e.p = 0 }
func (e *botEvent) update(dt float64) {
	if e.p < 0 {
		return
	}
	e.p += dt / e.duration
	if e.p >= 1 {
		e.p = -1
	}
}

type botWander struct {
	value, vel, target, next, amp, holdMin, holdMax, rate float64
	spring                                                bool
}

func (w *botWander) set(amp, lo, hi, rate float64) {
	w.amp, w.holdMin, w.holdMax, w.rate, w.next = amp, lo, hi, rate, 0
}
func (w *botWander) aim(v float64) { w.target, w.next = v, math.Inf(1) }
func (w *botWander) settled(eps float64) bool {
	if math.Abs(w.target-w.value) > eps {
		return false
	}
	return !w.spring || math.Abs(w.vel) <= eps
}
func (w *botWander) update(t, dt float64, rand func() float64) {
	if t >= w.next {
		w.target = (rand()*2 - 1) * w.amp
		w.next = t + w.holdMin + rand()*(w.holdMax-w.holdMin)
	}
	if w.spring {
		omega := w.rate * 1.6
		w.vel += (omega*omega*(w.target-w.value) - 2*.9*omega*w.vel) * dt
		w.value += w.vel * dt
	} else {
		w.value = botApproach(w.value, w.target, w.rate, dt)
	}
}

type botMotion struct {
	pose                                                          botPose
	state                                                         botMode
	random                                                        uint32
	t, tr, trDuration                                             float64
	wFrom                                                         [3]float64
	yawW, pitchW, rollW, lookXW, lookYW                           botWander
	blink, dart, flip, laughEv, nod                               botEvent
	blinkAt, dartAt, flipAt, laughAt, nodAt                       float64
	blinkAgain, flipPoked                                         bool
	dartX, dartY, flipSide                                        float64
	hopPhase, hopGain                                             float64
	hopCount                                                      int
	prevYaw, jelly, jellyV, gazeLead, gazeAt, turnK, breathPhase  float64
	gazeDir                                                       [2]float64
	ptrX, ptrY, ptrS, ptrTargetX, ptrTargetY, ptrTargetS, baseYaw float64
}

func newBotMotion(seed float64, state botMode) botMotion {
	m := botMotion{pose: botRestPose(botIdle), tr: 1, trDuration: 1.2, turnK: 1, flipSide: 1,
		blink: botNewEvent(.17), dart: botNewEvent(.12), flip: botNewEvent(botFlipDuration(false)), laughEv: botNewEvent(.8), nod: botNewEvent(1.7)}
	m.random = uint32(uint64(math.Floor(seed*1e6)+1) * 0x9e3779b1)
	if m.random == 0 {
		m.random = 1
	}
	m.yawW = botWander{amp: 36 * botDeg, holdMin: 1.1, holdMax: 2.6, rate: 3, spring: true}
	m.pitchW = botWander{amp: 10 * botDeg, holdMin: 1.1, holdMax: 2.6, rate: 2.6, spring: true}
	m.rollW = botWander{amp: 5 * botDeg, holdMin: 1.6, holdMax: 3.2, rate: 2, spring: true}
	m.lookXW = botWander{amp: 3.6, holdMin: .5, holdMax: 2, rate: 14}
	m.lookYW = botWander{amp: 2.4, holdMin: .5, holdMax: 2, rate: 14}
	m.t = m.rand() * 10
	m.hopPhase = m.rand()
	m.breathPhase = m.rand()
	m.blinkAt = m.t + 1 + m.rand()*3
	m.flipAt = m.nextFlip(m.t, 1)
	m.nodAt = m.t + 3 + m.rand()*4
	m.dartAt = m.t + 1 + m.rand()*2
	m.laughAt = m.t + .6 + m.rand()*1.5
	m.setState(state, true)
	return m
}
func (m *botMotion) rand() float64 {
	m.random += 0x6d2b79f5
	t := m.random
	t = (t ^ (t >> 15)) * (t | 1)
	t ^= t + (t^(t>>7))*(t|61)
	return float64(t^(t>>14)) / 4294967296
}
func (m *botMotion) setState(next botMode, immediate bool) {
	if next == m.state && !immediate {
		return
	}
	from := m.state
	m.state = next
	if immediate {
		m.pose.W = [3]float64{}
		m.pose.W[next] = 1
		m.tr = 1
	} else {
		m.wFrom = m.pose.W
		m.tr = 0
		m.trDuration = [3]float64{1.2, .7, 1.4}[next]
		if from == botSleeping {
			m.trDuration = 1
		}
	}
	switch next {
	case botIdle:
		m.yawW.set(35*botDeg, 2.6, 5.4, 2)
		m.pitchW.set(14*botDeg, 2.8, 5.8, 1.8)
		m.rollW.set(3.2*botDeg, 3.4, 6.6, 1.5)
		m.gazeAt = 0
		m.gazeDir = [2]float64{}
		m.lookXW.set(3.6, .6, 2.2, 13)
		m.lookYW.set(2.4, .6, 2.2, 13)
		m.flipAt = m.nextFlip(m.t, .6)
	case botWorking:
		m.yawW.set(16*botDeg, .9, 1.8, 4)
		m.pitchW.set(3*botDeg, 1.2, 2.4, 3)
		m.rollW.set(0, 1, 2, 3)
		m.lookXW.set(2, .5, 1.2, 12)
		m.lookYW.set(1, .5, 1.2, 12)
		m.hopPhase = 0
		m.hopCount = 0
		m.laughAt = m.t + .5 + m.rand()*1.2
	case botSleeping:
		m.yawW.set(7*botDeg, 3, 6, .7)
		m.pitchW.set(3*botDeg, 3, 6, .7)
		m.rollW.set(2*botDeg, 3, 6, .6)
		m.lookXW.set(0, 2, 4, 2)
		m.lookYW.set(0, 2, 4, 2)
		m.nodAt = m.t + 2.5 + m.rand()*4
	}
}
func (m *botMotion) setPointer(x, y, strength float64) {
	m.ptrTargetX = botClamp(x, -1.2, 1.2)
	m.ptrTargetY = botClamp(y, -1.2, 1.2)
	m.ptrTargetS = botClamp(strength, 0, 1)
}
func (m *botMotion) poke() {
	if m.flip.active() && m.flip.p < .6 {
		return
	}
	m.flipPoked = true
	m.flip.duration = botFlipDuration(true)
	m.flipSide = 1
	if m.rand() < .5 {
		m.flipSide = -1
	}
	m.flip.fire()
	m.flipAt = m.nextFlip(m.t, 1.1)
}
func (m *botMotion) nextFlip(t, k float64) float64 { return t + 8*k*(.625+m.rand()*.75) }
func (m *botMotion) nextGaze() [2]float64 {
	x, y := m.gazeDir[0], m.gazeDir[1]
	if x != 0 || y != 0 {
		p := m.rand()
		if p < .66 {
			return [2]float64{-x, -y}
		}
		if p < .85 {
			return [2]float64{-x, y}
		}
		return [2]float64{}
	}
	return [4][2]float64{{1, -1}, {-1, 1}, {-1, -1}, {1, 1}}[int(math.Floor(m.rand()*4))]
}
func (m *botMotion) update(dt float64) {
	dt = min(dt, .05)
	m.t += dt
	t := m.t
	p := &m.pose
	if m.tr < 1 {
		m.tr = min(1, m.tr+dt/m.trDuration)
		e := botSine(m.tr)
		for i := range 3 {
			target := 0.0
			if botMode(i) == m.state {
				target = 1
			}
			p.W[i] = m.wFrom[i] + (target-m.wFrom[i])*e
		}
	}
	wd, ww, ws := p.W[0], p.W[1], p.W[2]
	rest := botRest{}
	for i, r := range botRests {
		w := p.W[i]
		rest.pitch += r.pitch * w
		rest.roll += r.roll * w
		rest.y += r.y * w
		rest.lookX += r.lookX * w
		rest.lookY += r.lookY * w
	}
	if m.state == botIdle && t >= m.gazeAt {
		m.gazeDir = m.nextGaze()
		gx, gy := m.gazeDir[0], m.gazeDir[1]
		reach := .84 + m.rand()*.16
		m.yawW.aim(gx * 35 * botDeg * reach * m.turnK)
		m.pitchW.aim(gy * 14 * botDeg * reach)
		m.rollW.aim(gx * 3.2 * botDeg * reach * m.turnK)
		m.gazeAt = t + 2.6 + m.rand()*(4.4-2.6)
	}
	m.yawW.update(t, dt, m.rand)
	m.pitchW.update(t, dt, m.rand)
	m.rollW.update(t, dt, m.rand)
	m.lookXW.update(t, dt, m.rand)
	m.lookYW.update(t, dt, m.rand)
	m.ptrS = botApproach(m.ptrS, m.ptrTargetS, 8, dt)
	m.ptrX = botApproach(m.ptrX, m.ptrTargetX, 14, dt)
	m.ptrY = botApproach(m.ptrY, m.ptrTargetY, 14, dt)
	ps := m.ptrS
	quiet := 1 - .75*ps
	m.baseYaw = botApproach(m.baseYaw, m.yawW.value*quiet+22*botDeg*m.ptrX*ps, 5, dt)
	basePitch := rest.pitch + m.pitchW.value*quiet - 12*botDeg*m.ptrY*ps
	baseRoll := rest.roll + m.rollW.value*quiet
	baseLookX := rest.lookX + m.lookXW.value*quiet + 4.5*m.ptrX*ps
	baseLookY := rest.lookY + m.lookYW.value*quiet + 3*m.ptrY*ps
	spin, hopY, sx, sy, rollAdd, pitchAdd, blinkClose, lookXAdd, lookYAdd, laugh := 0.0, 0.0, 1.0, 1.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0
	whirl, whirlAngle := 0.0, 0.0
	smooth := func(a, b, v float64) float64 { x := botClamp((v-a)/(b-a), 0, 1); return x * x * (3 - 2*x) }
	envelope := func(q float64) float64 { return smooth(.1, .26, q) * (1 - smooth(.66, .9, q)) }
	ringAngle := func(q float64) float64 { return botTau * (1.5*q + .9*botEase(q)) }
	if t >= m.blinkAt && !m.blink.active() && wd+ww > .5 {
		m.blink.fire()
		m.blinkAgain = !m.blinkAgain && m.rand() < .22
		if m.blinkAgain {
			m.blinkAt = t + .28
		} else {
			m.blinkAt = t + 2.2 + m.rand()*2.6
		}
	}
	m.blink.update(dt)
	if m.blink.active() {
		blinkClose = math.Sin(math.Pi * m.blink.p)
	}
	if t >= m.dartAt && !m.dart.active() && wd+ww > .5 {
		m.dart.fire()
		m.dartX = (m.rand()*2 - 1) * 4
		m.dartY = (m.rand()*2 - 1) * 2
		m.dart.duration = .25 + m.rand()*.45
		m.dartAt = t + 1.2 + m.rand()*2.6
	}
	m.dart.update(dt)
	if m.dart.active() {
		q := m.dart.p
		hold := 1.0
		if q < .15 {
			hold = q / .15
		} else if q > .8 {
			hold = (1 - q) / .2
		}
		lookXAdd += m.dartX * hold * (wd + ww)
		lookYAdd += m.dartY * hold * (wd + ww)
	}
	if m.state == botIdle && t >= m.flipAt && !m.flip.active() {
		m.flipPoked = false
		m.flip.duration = botFlipDuration(false)
		m.flipSide = 1
		if m.rand() < .5 {
			m.flipSide = -1
		}
		m.flip.fire()
		m.flipAt = m.nextFlip(t, 1)
	}
	m.flip.update(dt)
	if m.flip.active() {
		pre := botFlipPre(m.flipPoked)
		a := (m.flip.p*m.flip.duration - pre) / .68
		q := botClamp(a, 0, 1)
		arc := math.Sin(math.Pi * q)
		spin += botTau * botEase(q)
		hopY -= 26 * arc
		tl := (a - 1) * .68
		squashTime := .37
		if m.flipPoked {
			squashTime = .24
		}
		peakT := (2.0 / 7) * squashTime
		rise := tl - peakT - .11
		depth := 0.0
		if tl <= peakT {
			depth = botSquash(tl / squashTime)
		} else if rise <= 0 {
			depth = botGround((tl - peakT) / .11)
		} else {
			depth = botRise(rise / .33)
		}
		land := 0.0
		if a < 0 {
			u := max(0, 1+a*.68/pre)
			if m.flipPoked {
				land = u * u * (3 - 2*u)
			} else {
				land = botHopSquash(u - 1)
			}
		} else if tl > 0 {
			land = depth
		} else if a < .2 {
			land = botHopSquash(a)
		}
		land *= 1.15
		sx += .16*land - .06*arc
		sy += -.18*land + .09*arc
		rollAdd += m.flipSide * 6 * botDeg * arc
		laugh = max(laugh, arc)
		whirl = max(whirl, envelope(q))
		whirlAngle = ringAngle(q)
	}
	if m.state == botWorking {
		m.hopGain = ww
	}
	if m.hopGain > .02 && (m.state == botWorking || m.hopPhase > 0) {
		m.hopPhase += dt / .68
		if m.hopPhase >= 1 {
			if m.state == botWorking {
				m.hopPhase -= 1
				m.hopCount++
			} else if (m.hopPhase-1)*.68 >= .11+.33 {
				m.hopPhase = 0
				m.hopGain = 0
			}
		}
		g := m.hopGain
		q := min(1, m.hopPhase)
		arc := math.Sin(math.Pi * q)
		spinning := m.hopCount%3 == 2
		h := 18.0
		if spinning {
			h = 26
		}
		hopY -= h * arc * g
		exitT := -1.0
		if m.state != botWorking && m.hopPhase > 1 {
			exitT = (m.hopPhase - 1) * .68
		}
		land := 0.0
		if exitT < 0 {
			land = botHopSquash(m.hopPhase)
		} else if exitT < .11 {
			land = botGround(exitT / .11)
		} else {
			land = botRise((exitT - .11) / .33)
		}
		sx += (.16*land - .06*arc) * g
		sy += (-.18*land + .09*arc) * g
		if spinning {
			spin += botTau * botEase(q) * g
			laugh = max(laugh, arc*g)
			if envelope(q)*g > whirl {
				whirl = envelope(q) * g
				whirlAngle = ringAngle(q)
			}
		}
		side := 1.0
		if m.hopCount%2 != 0 {
			side = -1
		}
		rollAdd += side * 6 * botDeg * arc * g
	}
	if m.state == botWorking && t >= m.laughAt && !m.laughEv.active() {
		m.laughEv.fire()
		m.laughEv.duration = .6 + m.rand()*.5
		m.laughAt = t + 1.6 + m.rand()*2.2
	}
	m.laughEv.update(dt)
	if m.laughEv.active() {
		q := m.laughEv.p
		v := 1.0
		if q < .18 {
			v = q / .18
		} else if q > .78 {
			v = (1 - q) / .22
		}
		laugh = max(laugh, v)
	}
	if m.state == botSleeping && t >= m.nodAt && !m.nod.active() {
		m.nod.fire()
		m.nodAt = t + 4 + m.rand()*4
	}
	m.nod.update(dt)
	if m.nod.active() {
		q := m.nod.p
		dip := 0.0
		if q < .72 {
			dip = botEase(q / .72)
		} else {
			dip = 1 - botEase((q-.72)/.28)
		}
		pitchAdd -= 13 * botDeg * dip * ws
	}
	m.breathPhase += dt / (3.6 + 1.2*ws)
	breath := math.Sin(m.breathPhase * botTau)
	p.Breath = breath
	sx += breath * (.008 + .014*ws)
	sy += breath * (.012 + .02*ws)
	bob := math.Sin(t*botTau/3.4) * 2 * (1 - ws)
	p.Yaw = m.baseYaw + spin
	dyaw := math.Mod(math.Mod(m.baseYaw-m.prevYaw+math.Pi, botTau)+botTau, botTau) - math.Pi
	m.prevYaw = m.baseYaw
	rate, lead := 0.0, 0.0
	if dt > 0 {
		rate = math.Abs(dyaw) / dt
		lead = botClamp(dyaw/dt*2.4, -2.2, 2.2)
	}
	m.gazeLead = botApproach(m.gazeLead, lead, 9, dt)
	m.jellyV += (16*16*(min(.22, .055*rate)-m.jelly) - 2*.45*16*m.jellyV) * dt
	m.jelly += m.jellyV * dt
	jelly := botClamp(m.jelly, -.08, .28) * .6
	sx *= 1 + jelly
	sy *= 1 - .55*jelly
	p.Pitch = basePitch + pitchAdd
	p.Roll = baseRoll + rollAdd
	p.X = 0
	p.Y = rest.y + hopY + bob
	p.SX = sx
	p.SY = sy
	p.EyeOpen = 1
	p.Laugh = botApproach(p.Laugh, laugh, 30, dt)
	p.BlinkL = blinkClose
	p.BlinkR = blinkClose
	p.LookX = baseLookX + lookXAdd + m.gazeLead
	p.LookY = baseLookY + lookYAdd
	p.Whirl = whirl
	p.WhirlAngle = whirlAngle
}

func (m *botMotion) activeMotion() bool {
	if m.tr < 1 || m.blink.active() || m.dart.active() || m.flip.active() || m.laughEv.active() || m.nod.active() {
		return true
	}
	if m.hopGain > .02 && (m.state == botWorking || m.hopPhase > 0) {
		return true
	}
	if m.ptrTargetS > .002 || m.ptrS > .002 || math.Abs(m.ptrX-m.ptrTargetX) > .005 || math.Abs(m.ptrY-m.ptrTargetY) > .005 {
		return true
	}
	if !m.yawW.settled(.004) || !m.pitchW.settled(.004) || !m.rollW.settled(.004) || !m.lookXW.settled(.02) || !m.lookYW.settled(.02) {
		return true
	}
	quiet := 1 - .75*m.ptrS
	yawTarget := m.yawW.value*quiet + 22*botDeg*m.ptrX*m.ptrS
	if math.Abs(yawTarget-m.baseYaw) > .004 || math.Abs(m.gazeLead) > .01 || math.Abs(m.jelly) > .003 || math.Abs(m.jellyV) > .005 || m.pose.Laugh > .01 {
		return true
	}
	return false
}

func (m *botMotion) advanceIdle(elapsed float64) float64 {
	if elapsed <= .05 || m.activeMotion() {
		return elapsed
	}
	skip := elapsed - .05
	m.t += skip
	ws := m.pose.W[2]
	m.breathPhase += skip / (3.6 + 1.2*ws)
	return .05
}

func (m *botMotion) nextWakeDelay() time.Duration {
	nextAt := math.Inf(1)
	consider := func(at float64) {
		if !math.IsInf(at, 0) && !math.IsNaN(at) && at < nextAt {
			nextAt = at
		}
	}
	consider(m.lookXW.next)
	consider(m.lookYW.next)
	consider(m.yawW.next)
	consider(m.pitchW.next)
	consider(m.rollW.next)
	if m.pose.W[0]+m.pose.W[1] > .5 {
		consider(m.blinkAt)
		consider(m.dartAt)
	}
	switch m.state {
	case botIdle:
		consider(m.gazeAt)
		consider(m.flipAt)
	case botWorking:
		consider(m.laughAt)
	case botSleeping:
		consider(m.nodAt)
	}
	if math.IsInf(nextAt, 1) {
		return 500 * time.Millisecond
	}
	sec := nextAt - m.t
	if sec <= 0 {
		return 16 * time.Millisecond
	}
	sec = botClamp(sec, .05, 2.0)
	return time.Duration(sec * float64(time.Second))
}
