// Package notify tells a person about a change they asked to be told about.
//
// Everything here is opt-in and nothing is on by default. A product that mails somebody unasked has
// made a decision on their behalf, which is the thing this codebase has refused at every turn — so
// the absence of a preference row and a disabled one mean the same thing, and both mean silence.
//
// Three properties are enforced rather than intended:
//
//   - Nothing reaches somebody who did not ask. The delivery pass reads consent, and a person with
//     no preferences is never a recipient of anything.
//   - Nothing is sent twice. The notification row is a state machine, so at-most-once holds under
//     two passes running at once rather than by either being careful.
//   - Nothing raised inside quiet hours is sent early or lost. It is held, and released.
package notify

import (
	"time"

	"market-lens/server/internal/instruments"
)

type UUID = instruments.UUID

// Kind is what a notification is about.
//
// Four, and the fourth is the one that needed a second look. A signal changing is a fact about a
// strategy's view; a message about it is one careless sentence from reading as a recommendation,
// which is why its template is constrained by requirement and scanned by the advice-vocabulary
// guard rather than trusted to care.
type Kind string

const (
	// KindDecisionWaiting: something only a person can settle — a quality finding the product
	// refuses to decide, or a limit they are outside.
	KindDecisionWaiting Kind = "decision_waiting"
	// KindPaperFill: a promoted order filled, or could not. It happens overnight with nobody
	// watching, which makes it the one event a person cannot otherwise learn about.
	KindPaperFill Kind = "paper_fill"
	// KindPipelineFailure: an import that failed. Offered to the owner alone, because they are the
	// only person who can do anything about it.
	KindPipelineFailure Kind = "pipeline_failure"
	// KindSignalChange: a strategy's view of an instrument changed. States the two views and the
	// strategy's caveat; never what to do.
	KindSignalChange Kind = "signal_change"
)

// Kinds is every kind, in the order a person reads them.
var Kinds = []Kind{KindDecisionWaiting, KindPaperFill, KindPipelineFailure, KindSignalChange}

// OwnerOnlyKinds are offered to the owner and absent for everybody else — absent rather than
// present and refused, because a switch that cannot be switched is a worse answer than no switch.
var OwnerOnlyKinds = map[Kind]bool{KindPipelineFailure: true}

// Channel is where a notification goes.
type Channel string

const (
	ChannelEmail   Channel = "email"
	ChannelWebPush Channel = "web_push"
)

// Channels is every channel.
var Channels = []Channel{ChannelEmail, ChannelWebPush}

// State is where a notification has got to.
type State string

const (
	StatePending State = "pending"
	// StateSending is held only for as long as one attempt takes. It exists so two passes running
	// at once cannot both claim the same row.
	StateSending State = "sending"
	StateSent    State = "sent"
	// StateFailed is one attempt that did not work and will be tried again.
	StateFailed State = "failed"
	// StateAbandoned is given up on, with the reason kept so a person can see why.
	StateAbandoned State = "abandoned"
)

// MaximumAttempts is how many times delivery is tried before a notification is abandoned.
//
// Five, with a widening gap: enough to ride out a mail server restart or a push service having a
// bad ten minutes, and few enough that a permanently wrong address stops being retried the same day.
const MaximumAttempts = 5

// Preference is one switch: a kind, a channel, and whether the person asked for it.
type Preference struct {
	Kind    Kind
	Channel Channel
	Enabled bool
}

// QuietHours is the window during which nothing arrives.
//
// Local wall-clock times plus an IANA zone, because a person means "while I am asleep" and daylight
// saving moves that against UTC twice a year. A window whose end is before its start crosses
// midnight, which is the ordinary case.
type QuietHours struct {
	StartsAt string
	EndsAt   string
	Timezone string
}

// Settings is everything a person has said about being told things.
type Settings struct {
	Preferences []Preference
	QuietHours  *QuietHours
	// NothingIsOnByDefault is always true and always present.
	NothingIsOnByDefault bool
}

// Subscription is one device, as a person reads it. The endpoint and keys are what is needed to
// send to it, not something a page has any use for, so they are not part of this.
type Subscription struct {
	ID         UUID
	Label      string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// Record is one telling, as a person reads it afterwards — without the payload, which is not kept
// beyond what was needed to send it.
type Record struct {
	Kind      Kind
	Channel   Channel
	State     State
	Count     int
	Attempts  int
	LastError *string
	CreatedAt time.Time
	SentAt    *time.Time
}

// Raise is one thing worth telling somebody about, before it is fanned out to the people who asked.
type Raise struct {
	Kind Kind
	// SubjectKey is what it is about, so several of the same thing collapse into one telling.
	SubjectKey string
	Count      int
	// Detail is the minimum a template needs. What may appear here is asserted per kind: a push
	// carries a kind and a count, an email may add a ticker, and neither may hold a figure.
	Detail map[string]string
	// Audience narrows a raise to particular people. Empty means everybody who consented — which
	// is right for a shared change like a pipeline failure, and wrong for a paper fill.
	Audience []UUID
}
