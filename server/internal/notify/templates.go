package notify

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"market-lens/server/internal/mail"
)

// What each kind says.
//
// Two rules hold across all of them, and both are asserted by test rather than trusted to care.
//
// **Nothing here tells anybody what to do.** A signal changing is a fact about a strategy's view;
// a message about it is one careless sentence from reading as a recommendation. Feature 021
// measured this strategy over ten years and found it lost to two of its three benchmarks, so the
// message states the two views and the strategy's caveat and stops. The advice-vocabulary guard
// that scans the interface scans this file too.
//
// **Nothing here carries what somebody owns.** No figure, no quantity, no valuation, no balance.
// An email may name an instrument; a push payload may not even do that.

// pushPayload is what travels to a browser's push service.
//
// A kind, a count, and a path. The payload rests on a third party's server until the browser
// collects it, and the threat there is not interception but accumulation — a service should learn
// nothing from what it is asked to store. This makes push deliberately less useful than it could
// be: "your paper order filled", not "ABB filled at 600.30". That is the trade, the right way round.
type pushPayload struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Path  string `json:"path"`
	Count int    `json:"count"`
}

var pushWording = map[Kind]struct{ title, body, path string }{
	KindDecisionWaiting: {"Something is waiting for you", "Open Market Lens to see what.", "/"},
	KindPaperFill:       {"A paper order settled", "Open Market Lens to see what happened.", "/paper"},
	KindPipelineFailure: {"Market data did not arrive", "Open Market Lens to see the run.", "/operations"},
	KindSignalChange:    {"A strategy changed its view", "Open Market Lens to read it.", "/signals"},
}

func buildPushPayload(kind Kind, count int) ([]byte, error) {
	wording, known := pushWording[kind]
	if !known {
		return nil, fmt.Errorf("no push wording for %q", kind)
	}
	return json.Marshal(pushPayload{
		Kind: string(kind), Title: wording.title, Body: wording.body,
		Path: wording.path, Count: count,
	})
}

// buildEmail writes the message for one notification.
//
// The closing lines are not decoration: every message says the person asked for it and how to stop,
// because a message that does not is one somebody reports as spam rather than unsubscribes from.
func buildEmail(recipient string, kind Kind, count int, detail map[string]string,
	baseURL, unsubscribeToken string) (mail.Message, error) {
	subject, body, err := emailWording(kind, count, detail)
	if err != nil {
		return mail.Message{}, err
	}
	stop := fmt.Sprintf("%s/unsubscribe?token=%s",
		strings.TrimRight(baseURL, "/"), url.QueryEscape(unsubscribeToken))

	body += "\n\nYou are receiving this because you asked Market Lens to tell you about it."
	body += "\nTo stop these, open " + stop
	body += "\nor turn it off under Account settings. Nothing is ever sent unless you asked for it."

	return mail.Message{To: recipient, Subject: subject, Text: body}, nil
}

func emailWording(kind Kind, count int, detail map[string]string) (string, string, error) {
	switch kind {
	case KindDecisionWaiting:
		thing := "decision"
		if count != 1 {
			thing = "decisions"
		}
		return fmt.Sprintf("%d %s waiting in Market Lens", count, thing),
			fmt.Sprintf("There %s %d %s waiting that only you can make.\n\n"+
				"Market Lens does not settle these itself: a finding it cannot decide, or a rule "+
				"you set that is no longer being met. The Overview lists them.",
				plural(count, "is", "are"), count, thing), nil

	case KindPaperFill:
		ticker := detail["ticker"]
		outcome := detail["outcome"]
		body := "A paper order you promoted has settled.\n\n" +
			"This is a simulation over stored prices. Nothing was traded and no order was placed " +
			"anywhere. The paper account shows what it filled at, and what it cost."
		if ticker != "" {
			return fmt.Sprintf("Your paper order in %s settled", ticker), body, nil
		}
		_ = outcome
		return "A paper order settled", body, nil

	case KindPipelineFailure:
		return "Market data did not arrive",
			"An import did not complete, so today's prices may be missing or incomplete.\n\n" +
				"Every figure in Market Lens is derived from stored bars, so until this is settled " +
				"some screens are reading older data than they look like they are. Operations has " +
				"the run and its error.", nil

	case KindSignalChange:
		// The one that needed constraining. It states the two views and the strategy's caveat, and
		// contains no imperative: what a person does about it is not this product's business.
		ticker := detail["ticker"]
		from, to := detail["from"], detail["to"]
		strategy := detail["strategy"]
		body := fmt.Sprintf(
			"The %s strategy's view of %s changed from %s to %s.\n\n"+
				"This is a strategy output, not advice. Its weights are stated rather than fitted, "+
				"and backtesting has measured it over ten years against the markets it trades in, "+
				"where it came out behind. Market Lens takes no view on what to do about this.",
			orUnknown(strategy), orUnknown(ticker), orUnknown(from), orUnknown(to))
		return fmt.Sprintf("%s: a strategy view changed", orUnknown(ticker)), body, nil
	}
	return "", "", fmt.Errorf("no email wording for %q", kind)
}

func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}
	return many
}

func orUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "an instrument"
	}
	return value
}
