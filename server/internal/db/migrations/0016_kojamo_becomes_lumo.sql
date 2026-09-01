-- Kojamo Oyj is now Lumo Kodit Oyj, and its trading code changed with it.
--
-- The company resolved the change at its 2026 Annual General Meeting; the new business name
-- was registered on 13 March 2026 and the Nasdaq Helsinki trading code changed from KOJAMO to
-- LUMO on 16 March 2026. The share ISIN, FI4000312251, did not change.
--
-- The seeded universe went on asking for KOJAMO.HE. The provider kept serving that symbol's
-- history until 2026-05-15 and then stopped, which is a difficult failure to notice: the
-- import succeeded every night, the instrument held nearly two thousand bars, and only its
-- newest session gave it away — four months behind a universe where nothing else was more
-- than a week behind.
--
-- The audit could not identify the replacement either, because a rename of this shape defeats
-- every lookup it makes. The symbol changed, the name changed, and the provider's catalog row
-- for LUMO.HE carries no ISIN at all — so matching on the one identifier that did survive
-- found nothing. `marketdata resolve --search lumo` returned the row directly:
--
--     XHEL LUMO.HE isin=- name="LUMO KODIT OYJ" currency=EUR
--
-- That is the provider's own answer, not an inference from the announcement. The distinction
-- has already earned its keep here once: the obvious guess for Sydbank was SYDB.CO and the
-- provider actually lists ALSYDB.CO.
--
-- The whole identity moves, not just the symbol. A row left reading "Kojamo Oyj" would
-- present the company under a name it no longer has and would defeat the next audit by name.
-- The ISIN is what ties the corrected row to the 1,986 bars already stored against it, so it
-- is the key here and is deliberately left untouched.

UPDATE instruments
SET ticker = 'LUMO',
    name   = 'Lumo Kodit Oyj'
WHERE isin = 'FI4000312251'
  AND ticker = 'KOJAMO';

UPDATE provider_instruments p
SET provider_symbol = 'LUMO.HE'
FROM instruments i
WHERE i.id = p.instrument_id
  AND i.isin = 'FI4000312251'
  AND p.provider = 'eodhd'
  AND p.provider_symbol = 'KOJAMO.HE';
