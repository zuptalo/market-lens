package portfolio

import (
	"context"
	"errors"
	"sort"
)

// Deriving the whole portfolio.
//
// Nothing read here is stored as a figure. Positions, cost, realised results and totals are all a
// fold over the person's own trades plus stored prices — which is what FR-010 requires and what
// makes a correction a re-read rather than a repair.

// View is the whole portfolio, derived from the trades and stored prices.
func (s *Service) View(ctx context.Context, userID string) (View, error) {
	if err := s.ready(userID); err != nil {
		return View{}, err
	}

	held, err := s.repository.Portfolio(ctx, userID)
	if errors.Is(err, ErrNotFound) {
		// Nobody starts with a portfolio, and not having one is not a failure. An empty view says
		// so, including the return it will never report.
		return View{
			Portfolio: Portfolio{UserID: UUID(userID), AccountingCurrency: defaultCurrency},
			Holdings:  []Holding{}, Realised: []RealisedResult{},
			Totals:                emptyTotals(),
			RecordsWhatYouEntered: true,
		}, nil
	}
	if err != nil {
		return View{}, err
	}

	trades, err := s.repository.CurrentTrades(ctx, userID)
	if err != nil {
		return View{}, err
	}

	byInstrument := map[UUID][]Trade{}
	order := make([]UUID, 0, len(trades))
	for _, trade := range trades {
		if _, seen := byInstrument[trade.InstrumentID]; !seen {
			order = append(order, trade.InstrumentID)
		}
		byInstrument[trade.InstrumentID] = append(byInstrument[trade.InstrumentID], trade)
	}

	identifiers := make([]string, 0, len(order))
	for _, id := range order {
		identifiers = append(identifiers, id.String())
	}
	prices, err := s.repository.latestPrices(ctx, identifiers)
	if err != nil {
		return View{}, err
	}

	// Rates are read once per session in play rather than once per holding, so a portfolio spanning
	// three markets reads three sessions rather than three rates per holding.
	sessions := map[SessionDate]rates{}
	for _, id := range order {
		if price, ok := prices[id]; ok {
			if _, loaded := sessions[price.session]; !loaded {
				quoted, err := s.repository.ratesOn(ctx, price.session)
				if err != nil {
					return View{}, err
				}
				sessions[price.session] = quoted
			}
		}
	}

	view := View{Portfolio: held, Holdings: []Holding{}, Realised: []RealisedResult{},
		RecordsWhatYouEntered: true}
	totalValue, totalCost, totalRealised := decZero, decZero, decZero
	complete := true

	for _, id := range order {
		folded, err := walk(byInstrument[id])
		if err != nil {
			return View{}, err
		}
		view.Realised = append(view.Realised, folded.realised...)
		for _, realised := range folded.realised {
			amount, err := parseDec(realised.Realised)
			if err != nil {
				return View{}, err
			}
			totalRealised = totalRealised.Add(amount)
		}
		if folded.quantity.Sign() <= 0 {
			// Sold in full. The holding leaves the open list and its realised result stays
			// readable — a re-purchase later starts a new cost rather than merging into this one.
			continue
		}

		sample := byInstrument[id][0]
		holding := Holding{InstrumentID: id, Ticker: sample.Ticker, Name: sample.Name,
			Currency: sample.Currency, Sector: sample.Sector, SectorName: sample.SectorName,
			MIC: sample.MIC, Quantity: folded.quantity.String(), Cost: folded.cost.String()}

		var latest *priced
		if price, ok := prices[id]; ok {
			latest = &price
		}
		quoted := rates{}
		if latest != nil {
			quoted = sessions[latest.session]
		}
		holding.Valuation = value(folded.quantity, sample.Currency, held.AccountingCurrency, latest, quoted)

		// The cost is in the holding's own currency; the value is in the accounting currency. They
		// are only comparable once the cost is converted too, at the same session's rates.
		costInAccounting, _, converted := convert(folded.cost, sample.Currency, held.AccountingCurrency, quoted)
		if holding.Valuation.Value != nil && converted {
			amount, err := parseDec(*holding.Valuation.Value)
			if err != nil {
				return View{}, err
			}
			unrealised := amount.Sub(costInAccounting).String()
			holding.Unrealised = &unrealised
			totalValue = totalValue.Add(amount)
			totalCost = totalCost.Add(costInAccounting)
		} else {
			complete = false
		}

		holding.Comparison, err = s.compare(ctx, holding, folded, latest)
		if err != nil {
			return View{}, err
		}
		view.Holdings = append(view.Holdings, holding)
	}

	sort.SliceStable(view.Holdings, func(i, j int) bool {
		return view.Holdings[i].Ticker < view.Holdings[j].Ticker
	})
	sort.SliceStable(view.Realised, func(i, j int) bool {
		return view.Realised[i].Ticker < view.Realised[j].Ticker
	})

	view.Totals = Totals{Cost: totalCost.String(), Realised: totalRealised.String(),
		Complete: complete, ReturnAbsence: ReturnAbsence}
	if complete {
		total := totalValue.String()
		unrealised := totalValue.Sub(totalCost).String()
		view.Totals.Value, view.Totals.Unrealised = &total, &unrealised
	} else {
		// The unvaluable holding stays in the list. Omitting it would make the total look whole,
		// which is the one thing a total must never do.
		reason := "at least one holding could not be valued, so no total is stated"
		view.Totals.IncompleteReason = &reason
	}
	return view, nil
}

// emptyTotals is what somebody with no trades sees: zeroes that are honestly zero, and the same
// stated absence of a return that a full portfolio carries.
func emptyTotals() Totals {
	value, unrealised := decZero.String(), decZero.String()
	return Totals{Value: &value, Cost: decZero.String(), Unrealised: &unrealised,
		Realised: decZero.String(), Complete: true, ReturnAbsence: ReturnAbsence}
}

// ValueOf prices a hypothetical quantity of any instrument this product carries, in the person's
// accounting currency.
//
// Feature 025 needs it for an intent in something the person does not hold yet: the portfolio
// values only what is held, but the product knows the price, and reporting "no price" for an
// instrument it has bars for would be untrue. Valuing it here rather than in the calling package
// keeps one implementation of what a holding is worth — including the euro cross and the two
// absences that can arise.
func (s *Service) ValueOf(ctx context.Context, userID string, instrumentID UUID, quantity string) (Valuation, error) {
	if err := s.ready(userID); err != nil {
		return Valuation{}, err
	}
	amount, err := parseDec(quantity)
	if err != nil || amount.Sign() <= 0 {
		reason := ValuationNoPrice
		return Valuation{AbsenceReason: &reason}, nil
	}

	held, err := s.repository.Portfolio(ctx, userID)
	accounting := defaultCurrency
	if err == nil {
		accounting = held.AccountingCurrency
	} else if !errors.Is(err, ErrNotFound) {
		return Valuation{}, err
	}

	currency, err := s.repository.InstrumentCurrency(ctx, instrumentID.String())
	if err != nil {
		return Valuation{}, err
	}
	if currency == "" {
		reason := ValuationNoPrice
		return Valuation{AbsenceReason: &reason}, nil
	}

	prices, err := s.repository.latestPrices(ctx, []string{instrumentID.String()})
	if err != nil {
		return Valuation{}, err
	}
	latest, priced := prices[instrumentID]
	if !priced {
		reason := ValuationNoPrice
		return Valuation{AbsenceReason: &reason}, nil
	}
	quoted, err := s.repository.ratesOn(ctx, latest.session)
	if err != nil {
		return Valuation{}, err
	}
	return value(amount, currency, accounting, &latest, quoted), nil
}
