package features

// SessionRange is an inclusive span of sessions.
type SessionRange struct {
	From, To SessionDate
}

// AffectedRange is the span of sessions whose windows a bar at the given session takes part
// in: [at, at + (wMax − 1)] counted in the exchange's sessions — closed dates are no session
// — and clipped at the calendar's last session (research R-004). A session the calendar does
// not know affects only itself.
func AffectedRange(calendar []Session, at SessionDate, wMax int) SessionRange {
	affected := SessionRange{From: at, To: at}
	remaining := wMax - 1
	found := false
	for _, session := range calendar {
		if session.Status == SessionClosed || session.Date < at {
			continue
		}
		if session.Date == at {
			found = true
			continue
		}
		if !found || remaining <= 0 {
			break
		}
		affected.To = session.Date
		remaining--
	}
	return affected
}
