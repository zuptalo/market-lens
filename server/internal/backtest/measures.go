package backtest

import (
	"math"
	"math/big"
	"sort"
	"time"
)

// The six measures, and the benchmark's.
//
// They are reported together or not at all. A result free to report a subset would report the
// flattering one, and the two that flatter least — maximum drawdown and total costs — are exactly
// the two a curve makes easy to leave out. Drawdown is stated as a loss, so a positive value is a
// sign error rather than good news, and the database refuses it.

// tradingSessionsPerYear is the conventional figure volatility is annualised by. Stated here
// rather than derived from the calendar so two results over different ranges are comparable.
const tradingSessionsPerYear = 252

func bigFromInt(value int) *big.Int { return big.NewInt(int64(value)) }

// point is one valued session of a series — the strategy's equity or a benchmark's close.
type point struct {
	session SessionDate
	value   float64
}

func (e *engine) measure(equity []EquityPoint, trades []Trade) []Measures {
	curve := make([]point, 0, len(equity))
	for _, item := range equity {
		if item.Total == nil {
			// A session the portfolio could not be valued on contributes no return. It is not
			// bridged: pretending the gap was flat would invent a day that never happened.
			continue
		}
		total, err := parseDec(*item.Total)
		if err != nil {
			continue
		}
		curve = append(curve, point{session: item.SessionDate, value: total.Float()})
	}

	count := int64(len(trades))
	costs := decZero
	for _, trade := range trades {
		for _, part := range []string{trade.Brokerage, trade.Slippage, trade.SpreadCost} {
			value, err := parseDec(part)
			if err != nil {
				continue
			}
			costs = costs.Add(value)
		}
	}

	measures := []Measures{e.measureSeries(MeasureSubjectStrategy, nil, curve, count, costs)}
	if len(curve) < 2 {
		// With no valued range there is nothing to compare a benchmark over either, and a
		// comparison invented from one point would be arithmetic on a single number.
		for index := range e.in.benchmarks {
			series := &e.in.benchmarks[index]
			measures = append(measures, absentMeasures(series, MeasureInsufficientSessions))
		}
		return measures
	}

	from, to := curve[0].session, curve[len(curve)-1].session
	for index := range e.in.benchmarks {
		series := &e.in.benchmarks[index]
		measures = append(measures, e.measureBenchmark(series, from, to))
	}
	return measures
}

// measureBenchmark reports the series over the identical range, or states plainly why it cannot.
//
// Two different things have to be told apart here. A series with no point on the range's first
// session because its market was shut that day covers the range perfectly well; a series that
// begins months into it does not. The give-up window separates them — the same bound the
// simulation uses everywhere else — so a holiday is tolerated and a genuine gap is reported.
//
// The Danish case is the one this exists for. OMXC25.INDX begins 110 days after this product's
// stored history does, and reporting it over the window it happens to cover would compare two
// different periods and present the result as one comparison. Splicing in OMXC20, the index it
// replaced, would be worse: two different things shown as one series.
func (e *engine) measureBenchmark(series *benchmark, from, to SessionDate) Measures {
	if len(series.sessions) == 0 {
		return absentMeasures(series, MeasureInsufficientSessions)
	}
	firstInRange := sort.Search(len(series.sessions), func(i int) bool { return series.sessions[i] >= from })
	lastInRange := sort.Search(len(series.sessions), func(i int) bool { return series.sessions[i] > to }) - 1
	if firstInRange > lastInRange {
		return absentMeasures(series, MeasureInsufficientSessions)
	}
	if e.sessionsApart(from, series.sessions[firstInRange]) > e.configuration.GiveUpSessions {
		return absentMeasures(series, MeasureSeriesStartsAfter)
	}
	if e.sessionsApart(series.sessions[lastInRange], to) > e.configuration.GiveUpSessions {
		return absentMeasures(series, MeasureSeriesEndsBefore)
	}

	curve := make([]point, 0, lastInRange-firstInRange+1)
	for _, session := range series.sessions[firstInRange : lastInRange+1] {
		curve = append(curve, point{session: session, value: series.closes[session].Float()})
	}
	// A benchmark bears no trades and no costs. Reporting them as zero rather than omitting them
	// is what makes the two columns read as the same six figures.
	return e.measureSeries(series.code, &series.id, curve, 0, decZero)
}

// sessionsApart counts the trading sessions between two dates on the simulation's own calendar,
// falling back to calendar days for a date the universe never traded on.
func (e *engine) sessionsApart(earlier, later SessionDate) int {
	start, startKnown := e.in.index[earlier]
	end, endKnown := e.in.index[later]
	if startKnown && endKnown {
		return end - start
	}
	from, err := time.Parse("2006-01-02", earlier.String())
	if err != nil {
		return 0
	}
	to, err := time.Parse("2006-01-02", later.String())
	if err != nil {
		return 0
	}
	return int(to.Sub(from).Hours() / 24)
}

func absentMeasures(series *benchmark, reason MeasureAbsence) Measures {
	id := series.id
	return Measures{Subject: series.code, BenchmarkSeriesID: &id, AbsenceReason: &reason}
}

func (e *engine) measureSeries(subject string, seriesID *UUID, curve []point,
	trades int64, costs dec) Measures {
	if len(curve) < 2 || curve[0].value <= 0 {
		reason := MeasureInsufficientSessions
		return Measures{Subject: subject, BenchmarkSeriesID: seriesID, AbsenceReason: &reason}
	}

	first, last := curve[0], curve[len(curve)-1]
	totalReturn := last.value/first.value - 1

	years := yearsBetween(first.session, last.session)
	annualised := totalReturn
	if years > 0 {
		annualised = math.Pow(1+totalReturn, 1/years) - 1
	}

	// Sample standard deviation of the session-to-session returns, annualised by the conventional
	// figure. Sample rather than population because a backtest is one realisation of a method,
	// not the whole of it.
	var sum, sumSquares float64
	returns := 0
	for index := 1; index < len(curve); index++ {
		previous := curve[index-1].value
		if previous <= 0 {
			continue
		}
		change := curve[index].value/previous - 1
		sum += change
		sumSquares += change * change
		returns++
	}
	volatility := 0.0
	if returns > 1 {
		mean := sum / float64(returns)
		variance := (sumSquares - float64(returns)*mean*mean) / float64(returns-1)
		if variance > 0 {
			volatility = math.Sqrt(variance) * math.Sqrt(tradingSessionsPerYear)
		}
	}

	// The worst peak-to-trough fall the series actually put a holder through. Computed against a
	// running peak rather than against the start, because a drawdown measured from the beginning
	// would report a rising series as having had none.
	peak := curve[0].value
	drawdown := 0.0
	for _, item := range curve {
		if item.value > peak {
			peak = item.value
		}
		if peak <= 0 {
			continue
		}
		if fall := item.value/peak - 1; fall < drawdown {
			drawdown = fall
		}
	}

	from, to := curve[0].session, curve[len(curve)-1].session
	totalText := render(totalReturn)
	annualisedText := render(annualised)
	volatilityText := render(volatility)
	drawdownText := render(drawdown)
	costsText := costs.String()
	tradeCount := trades
	return Measures{
		Subject:           subject,
		BenchmarkSeriesID: seriesID,
		From:              &from,
		To:                &to,
		TotalReturn:       &totalText,
		AnnualisedReturn:  &annualisedText,
		Volatility:        &volatilityText,
		MaxDrawdown:       &drawdownText,
		TradeCount:        &tradeCount,
		TotalCosts:        &costsText,
	}
}

// render writes a computed float at the stored precision. A non-finite figure would mean the
// arithmetic went wrong; reporting zero would hide that, so it becomes a zero return rather than
// a stored NaN the database would refuse anyway.
func render(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return decZero.String()
	}
	parsed, err := parseDec(formatFloat(value))
	if err != nil {
		return decZero.String()
	}
	return parsed.String()
}

func formatFloat(value float64) string {
	return new(big.Float).SetFloat64(value).Text('f', places+2)
}

func yearsBetween(from, to SessionDate) float64 {
	start, err := time.Parse("2006-01-02", from.String())
	if err != nil {
		return 0
	}
	end, err := time.Parse("2006-01-02", to.String())
	if err != nil {
		return 0
	}
	days := end.Sub(start).Hours() / 24
	if days <= 0 {
		return 0
	}
	return days / 365.25
}
