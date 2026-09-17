package portfolio

// Valuing a holding: the latest stored close, converted into the person's own currency.
//
// Two rules, both feature 021's and both kept because the alternative is a plausible number nobody
// could have observed. A price comes from a session the instrument actually traded, and if there is
// none the holding is stated unvalued rather than assumed. A conversion uses one session's rates,
// and if either leg is missing the holding is stated unvalued rather than converted at yesterday's.

// priced is one instrument's latest stored close and the session it came from.
type priced struct {
	close   dec
	session SessionDate
}

// rates are the euro rates for one session, keyed by the currency they quote.
type rates map[string]dec

// convert moves an amount from one currency into the accounting currency, crossing through the
// euro.
//
// Every stored rate has the euro as its base, because feature 021 chose the euro precisely to avoid
// cross rates. A personal portfolio cannot: telling somebody in Stockholm their holdings are worth
// €48,000 answers a question nobody asked. So a Danish holding in a Swedish portfolio goes
// DKK → EUR → SEK — divide by the euro rate of the holding's currency, multiply by the euro rate of
// the accounting currency — and the intermediate is rounded to stored precision before the second
// leg reads it, exactly as the backtest rounds between its own steps.
//
// Storing a direct cross, or an inverse, would let two stored numbers disagree with no way to say
// which was right. That is the rule this must not fork.
func convert(amount dec, from, accounting string, quoted rates) (value dec, rate dec, ok bool) {
	if from == accounting {
		return amount, decOne, true
	}

	inEuro := amount
	if from != "EUR" {
		fromRate, exists := quoted[from]
		if !exists || fromRate.Sign() <= 0 {
			return decZero, decZero, false
		}
		inEuro = amount.Div(fromRate)
	}
	if accounting == "EUR" {
		// One leg. The effective rate is what one unit of the holding's currency is worth in euro.
		if from == "EUR" {
			return inEuro, decOne, true
		}
		return inEuro, decOne.Div(quoted[from]), true
	}

	toRate, exists := quoted[accounting]
	if !exists || toRate.Sign() <= 0 {
		return decZero, decZero, false
	}
	converted := inEuro.Mul(toRate)

	// The rate recorded on the holding is the one that actually applied: how many units of the
	// accounting currency one unit of the holding's currency bought. Stated so a reader can
	// reproduce the conversion rather than take it on trust.
	effective := toRate
	if from != "EUR" {
		effective = toRate.Div(quoted[from])
	}
	return converted, effective, true
}

// value prices one holding, or states why it could not.
func value(quantity dec, currency, accounting string, latest *priced, quoted rates) Valuation {
	if latest == nil {
		reason := ValuationNoPrice
		return Valuation{AbsenceReason: &reason}
	}
	gross := quantity.Mul(latest.close)
	converted, rate, ok := convert(gross, currency, accounting, quoted)
	if !ok {
		reason := ValuationNoRate
		return Valuation{AbsenceReason: &reason}
	}

	amount, session := converted.String(), latest.session
	valuation := Valuation{Value: &amount, Session: &session}
	if currency != accounting {
		applied := rate.String()
		valuation.ConversionRate = &applied
	}
	return valuation
}
