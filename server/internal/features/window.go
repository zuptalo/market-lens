package features

// SessionStatus mirrors exchange_sessions.status.
type SessionStatus string

const (
	SessionOpen    SessionStatus = "open"
	SessionHalfDay SessionStatus = "half_day"
	SessionClosed  SessionStatus = "closed"
)

// Session is one dated row of an exchange calendar.
type Session struct {
	Date   SessionDate
	Status SessionStatus
}

// Bar is one stored daily bar, in currency units, on the price basis the caller chose.
type Bar struct {
	Session SessionDate
	Open    float64
	High    float64
	Low     float64
	Close   float64
	Volume  int64
}

// IsSession reports whether the exchange traded on the date: a half day is a session, a
// closed day is not (FR-004, FR-005).
func (s Session) IsSession() bool {
	return s.Status == SessionOpen || s.Status == SessionHalfDay
}

// History is one instrument's stored bars aligned to its exchange calendar, indexed so a
// window ending at any session resolves in constant time.
type History struct {
	sessions []SessionDate // trading sessions only, ascending
	index    map[SessionDate]int
	bars     []*Bar // by session index; nil where no bar is stored
	runStart []int  // by session index: the index the gap-free run containing it started at
	first    int    // index of the earliest stored bar, or -1
}

// NewHistory aligns bars to the calendar. Bars on dates the calendar does not hold as a
// trading session are ignored: FR-016 says no value may describe such a date.
func NewHistory(bars []Bar, calendar []Session) *History {
	h := &History{index: map[SessionDate]int{}, first: -1}
	for _, session := range calendar {
		if session.IsSession() {
			h.index[session.Date] = len(h.sessions)
			h.sessions = append(h.sessions, session.Date)
		}
	}
	h.bars = make([]*Bar, len(h.sessions))
	for i := range bars {
		if at, ok := h.index[bars[i].Session]; ok {
			h.bars[at] = &bars[i]
		}
	}
	h.runStart = make([]int, len(h.sessions))
	for i := range h.sessions {
		switch {
		case h.bars[i] == nil:
			h.runStart[i] = -1
		case i > 0 && h.bars[i-1] != nil:
			h.runStart[i] = h.runStart[i-1]
		default:
			h.runStart[i] = i
		}
		if h.bars[i] != nil && h.first < 0 {
			h.first = i
		}
	}
	return h
}

// Sessions returns every trading session the calendar holds, ascending.
func (h *History) Sessions() []SessionDate { return h.sessions }

// Bar returns the stored bar at a session, if any.
func (h *History) Bar(at SessionDate) (Bar, bool) {
	i, ok := h.index[at]
	if !ok || h.bars[i] == nil {
		return Bar{}, false
	}
	return *h.bars[i], true
}

// Window returns the n stored bars ending at the session, ascending, or the reason there is
// no such window. A session with no stored bar of its own yields nothing at all: there is no
// observation to describe. Otherwise the window's first calendar session either precedes
// the instrument's first stored bar (insufficient_history) or a trading session inside the
// window lacks a bar (window_gap). Closed days are not sessions and consume no slot.
func (h *History) Window(end SessionDate, n int) ([]Bar, AbsenceReason) {
	e, ok := h.index[end]
	if !ok || h.bars[e] == nil || n <= 0 {
		return nil, ""
	}
	start := e - n + 1
	if start < 0 || start < h.first {
		return nil, AbsenceInsufficientHistory
	}
	if h.runStart[e] > start {
		return nil, AbsenceWindowGap
	}
	out := make([]Bar, 0, n)
	for i := start; i <= e; i++ {
		out = append(out, *h.bars[i])
	}
	return out, ""
}

// Window is the one-shot form of History.Window for callers holding a single window.
func Window(bars []Bar, calendar []Session, end SessionDate, n int) ([]Bar, AbsenceReason) {
	return NewHistory(bars, calendar).Window(end, n)
}
