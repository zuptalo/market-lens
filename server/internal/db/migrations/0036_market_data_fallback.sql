-- Feature 030: a temporary fallback for daily prices while the primary provider refuses us.
--
-- Nothing here turns the fallback on. The switch arrives off, and an installation that never asks
-- for unofficial data never receives any.

-- The owner's switch. A singleton — a second row would make "is it on?" ambiguous — and in the
-- database rather than the environment, because Keel deploys images, never configuration: a
-- switch in a ConfigMap would silently not arrive.
CREATE TABLE market_data_fallback_settings (
    singleton  boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    enabled    boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO market_data_fallback_settings DEFAULT VALUES;

-- What the product last said about the fallback. The state itself is derived from bar provenance;
-- this row records it once per change, so a change is told exactly once and a quiet night says
-- nothing. Written only by the pass that observes it, inside the transaction that raises the
-- notice and publishes the event.
CREATE TABLE market_data_fallback_state (
    singleton              boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    active                 boolean NOT NULL DEFAULT false,
    -- The earliest session still carried by a fallback bar.
    since                  date,
    -- How many instruments hold at least one fallback bar.
    instruments            integer NOT NULL DEFAULT 0 CHECK (instruments >= 0),
    -- The primary is delivering again, but older fallback bars have not been replaced yet.
    pending_reconciliation boolean NOT NULL DEFAULT false,
    changed_at             timestamptz NOT NULL DEFAULT now(),
    CHECK (active = (instruments > 0)),
    CHECK (active = (since IS NOT NULL)),
    CHECK (active OR NOT pending_reconciliation)
);
INSERT INTO market_data_fallback_state DEFAULT VALUES;

-- Fallback bars are few and temporary, and every question about them — is the product on the
-- fallback, since when, which instruments — is asked of them alone. A partial index keeps those
-- questions from reading every bar the product has ever stored.
CREATE INDEX daily_price_bars_fallback_idx ON daily_price_bars (instrument_id, session_date)
    WHERE provider = 'yahoo';

-- A fallback run is a kind of its own, recorded as the child of the primary run it covered, so
-- every run's provider column says which provider it actually asked.
ALTER TABLE import_runs
    DROP CONSTRAINT import_runs_kind_check,
    ADD CONSTRAINT import_runs_kind_check CHECK (kind IN
        ('universe_sync', 'backfill', 'daily_update', 'retry', 'fallback')),
    ADD CONSTRAINT import_runs_fallback_has_parent CHECK (kind <> 'fallback' OR parent_run_id IS NOT NULL);

-- A sixth kind of thing worth being told about: the product started, or stopped, running on
-- fallback prices. Both tables widen together, as before.
ALTER TABLE notification_preferences
    DROP CONSTRAINT notification_preferences_kind_check,
    ADD CONSTRAINT notification_preferences_kind_check CHECK (kind IN
        ('decision_waiting', 'paper_fill', 'pipeline_failure', 'signal_change', 'release_deployed',
         'market_data_fallback'));

ALTER TABLE notifications
    DROP CONSTRAINT notifications_kind_check,
    ADD CONSTRAINT notifications_kind_check CHECK (kind IN
        ('decision_waiting', 'paper_fill', 'pipeline_failure', 'signal_change', 'release_deployed',
         'market_data_fallback'));

-- The fallback's symbol for every instrument, verified against production on 2026-09-29: the
-- fallback reported the instrument's currency and exactly its stored close on its newest stored
-- session (specs/030-market-data-fallback/research.md, R3). Not one was inferred — the comment on
-- each row is the evidence it matched.
--
-- Keyed on ISIN and exchange rather than on the primary's symbol. The ISIN survives a rename, and
-- the exchange is needed because Nordea's one ISIN is listed on three of the four exchanges.
INSERT INTO provider_instruments (provider, provider_symbol, instrument_id, active)
SELECT 'yahoo', mapped.symbol, i.id, true
FROM (VALUES
    ('XCSE','DK0010311471','ALSYDB.CO'),  -- AL: DKK 723.5 on 2026-09-28
    ('XCSE','DK0060946788','AMBU-B.CO'),  -- AMBU-B: DKK 69.25 on 2026-09-28
    ('XCSE','DK0015998017','BAVA.CO'),  -- BAVA: DKK 218.8 on 2026-09-28
    ('XCSE','DK0060448595','COLO-B.CO'),  -- COLO-B: DKK 420.8 on 2026-09-28
    ('XCSE','DK0010274414','DANSKE.CO'),  -- DANSKE: DKK 380.4 on 2026-09-28
    ('XCSE','DK0060738599','DEMANT.CO'),  -- DEMANT: DKK 321.2 on 2026-09-28
    ('XCSE','DK0060079531','DSV.CO'),  -- DSV: DKK 1190 on 2026-09-28
    ('XCSE','DK0010234467','FLS.CO'),  -- FLS: DKK 578.5 on 2026-09-28
    ('XCSE','DK0010272202','GMAB.CO'),  -- GMAB: DKK 2302 on 2026-09-28
    ('XCSE','DK0010272632','GN.CO'),  -- GN: DKK 107 on 2026-09-28
    ('XCSE','DK0060542181','ISS.CO'),  -- ISS: DKK 290.2 on 2026-09-28
    ('XCSE','DK0010307958','JYSK.CO'),  -- JYSK: DKK 1115 on 2026-09-28
    ('XCSE','DK0010244425','MAERSK-A.CO'),  -- MAERSK-A: DKK 22320 on 2026-09-28
    ('XCSE','DK0010244508','MAERSK-B.CO'),  -- MAERSK-B: DKK 23140 on 2026-09-28
    ('XCSE','FI4000297767','NDA-DK.CO'),  -- NDA-DK: DKK 133.45 on 2026-09-28
    ('XCSE','DK0010287663','NKT.CO'),  -- NKT: DKK 895 on 2026-09-28
    ('XCSE','DK0062498333','NOVO-B.CO'),  -- NOVO-B: DKK 252.25 on 2026-09-28
    ('XCSE','DK0060336014','NSIS-B.CO'),  -- NSIS-B: DKK 426.3 on 2026-09-28
    ('XCSE','DK0060094928','ORSTED.CO'),  -- ORSTED: DKK 134.45 on 2026-09-28
    ('XCSE','DK0060252690','PNDORA.CO'),  -- PNDORA: DKK 847.4 on 2026-09-28
    ('XCSE','DK0060854669','RILBA.CO'),  -- RILBA: DKK 1855 on 2026-09-28
    ('XCSE','DK0063855168','ROCK-B.CO'),  -- ROCK-B: DKK 191.9 on 2026-09-28
    ('XCSE','DK0060636678','TRYG.CO'),  -- TRYG: DKK 141.6 on 2026-09-28
    ('XCSE','DK0061539921','VWS.CO'),  -- VWS: DKK 199.25 on 2026-09-28
    ('XCSE','DK0060257814','ZEAL.CO'),  -- ZEAL: DKK 268 on 2026-09-28
    ('XHEL','FI0009007264','BITTI.HE'),  -- BITTI: EUR 37.05 on 2026-09-25
    ('XHEL','FI0009007884','ELISA.HE'),  -- ELISA: EUR 36.76 on 2026-09-28
    ('XHEL','FI0009007132','FORTUM.HE'),  -- FORTUM: EUR 23.77 on 2026-09-28
    ('XHEL','FI4000571013','HIAB.HE'),  -- HIAB: EUR 59.2 on 2026-09-25
    ('XHEL','FI0009000459','HUH1V.HE'),  -- HUH1V: EUR 30.1 on 2026-09-25
    ('XHEL','FI0009005870','KCR.HE'),  -- KCR: EUR 32.06 on 2026-09-28
    ('XHEL','FI0009004824','KEMIRA.HE'),  -- KEMIRA: EUR 17.32 on 2026-09-25
    ('XHEL','FI0009000202','KESKOB.HE'),  -- KESKOB: EUR 22.96 on 2026-09-25
    ('XHEL','FI0009013403','KNEBV.HE'),  -- KNEBV: EUR 51.5 on 2026-09-25
    ('XHEL','FI4000312251','LUMO.HE'),  -- LUMO: EUR 7.16 on 2026-09-28
    ('XHEL','FI4000552526','MANTA.HE'),  -- MANTA: EUR 6.135 on 2026-09-28
    ('XHEL','FI0009014575','METSO.HE'),  -- MOCORP: EUR 16.9 on 2026-09-28
    ('XHEL','FI4000297767','NDA-FI.HE'),  -- NDA-FI: EUR 17.855 on 2026-09-28
    ('XHEL','FI0009013296','NESTE.HE'),  -- NESTE: EUR 34.16 on 2026-09-28
    ('XHEL','FI0009000681','NOKIA.HE'),  -- NOKIA: EUR 8.83 on 2026-09-28
    ('XHEL','FI0009014377','ORNBV.HE'),  -- ORNBV: EUR 82.95 on 2026-09-25
    ('XHEL','FI0009002422','OUT1V.HE'),  -- OUT1V: EUR 5.395 on 2026-09-28
    ('XHEL','FI4000198031','QTCOM.HE'),  -- QTCOM: EUR 33.42 on 2026-09-25
    ('XHEL','FI4000552500','SAMPO.HE'),  -- SAMPO: EUR 8.84 on 2026-09-28
    ('XHEL','FI0009005961','STERV.HE'),  -- STERV: EUR 10.385 on 2026-09-28
    ('XHEL','FI0009000277','TIETO.HE'),  -- TIETO: EUR 18.29 on 2026-09-28
    ('XHEL','FI0009005318','TYRES.HE'),  -- TYRES: EUR 15.98 on 2026-09-25
    ('XHEL','FI0009005987','UPM.HE'),  -- UPM: EUR 25.36 on 2026-09-28
    ('XHEL','FI4000074984','VALMT.HE'),  -- VALMT: EUR 28.44 on 2026-09-28
    ('XHEL','FI0009003727','WRT1V.HE'),  -- WRT1V: EUR 28.36 on 2026-09-28
    ('XOSL','NO0010345853','AKRBP.OL'),  -- AKRBP: NOK 350 on 2026-09-28
    ('XOSL','FO0000000179','BAKKA.OL'),  -- BAKKA: NOK 451.4 on 2026-09-28
    ('XOSL','BMG173841013','BWLPG.OL'),  -- BWLPG: NOK 235.8 on 2026-09-28
    ('XOSL','NO0010161896','DNB.OL'),  -- DNB: NOK 323.2 on 2026-09-28
    ('XOSL','NO0012851874','DOFG.OL'),  -- DOFG: NOK 130.9 on 2026-09-28
    ('XOSL','NO0010096985','EQNR.OL'),  -- EQNR: NOK 405.7 on 2026-09-28
    ('XOSL','CY0200352116','FRO.OL'),  -- FRO: NOK 462.8 on 2026-09-28
    ('XOSL','NO0010582521','GJF.OL'),  -- GJF: NOK 262.2 on 2026-09-28
    ('XOSL','NO0011082075','HAUTO.OL'),  -- HAUTO: NOK 185.7 on 2026-09-28
    ('XOSL','NO0013536151','KOG.OL'),  -- KOG: NOK 311.5 on 2026-09-28
    ('XOSL','NO0003054108','MOWI.OL'),  -- MOWI: NOK 207.2 on 2026-09-28
    ('XOSL','NO0010196140','NAS.OL'),  -- NAS: NOK 12.23 on 2026-09-28
    ('XOSL','NO0005052605','NHY.OL'),  -- NHY: NOK 82.82 on 2026-09-28
    ('XOSL','NO0003055501','NOD.OL'),  -- NOD: NOK 182.3 on 2026-09-28
    ('XOSL','NO0003733800','ORK.OL'),  -- ORK: NOK 92.85 on 2026-09-28
    ('XOSL','NO0010209331','PROT.OL'),  -- PROT: NOK 432.8 on 2026-09-28
    ('XOSL','NO0010310956','SALM.OL'),  -- SALM: NOK 589 on 2026-09-28
    ('XOSL','NO0003053605','STB.OL'),  -- STB: NOK 194.7 on 2026-09-28
    ('XOSL','LU0075646355','SUBC.OL'),  -- SUBC: NOK 330.6 on 2026-09-28
    ('XOSL','NO0010063308','TEL.OL'),  -- TEL: NOK 130.3 on 2026-09-28
    ('XOSL','NO0012470089','TOM.OL'),  -- TOM: NOK 91.55 on 2026-09-28
    ('XOSL','NO0011202772','VAR.OL'),  -- VAR: NOK 51.6 on 2026-09-28
    ('XOSL','NO0010736879','VEND.OL'),  -- VEND: NOK 204.8 on 2026-09-28
    ('XOSL','NO0010571680','WAWI.OL'),  -- WAWI: NOK 180.2 on 2026-09-28
    ('XOSL','NO0010208051','YAR.OL'),  -- YAR: NOK 435.6 on 2026-09-28
    ('XSTO','CH0012221716','ABB.ST'),  -- ABB: SEK 958.8 on 2026-09-28
    ('XSTO','SE0014781795','ADDT-B.ST'),  -- ADDT-B: SEK 341 on 2026-09-28
    ('XSTO','SE0000695876','ALFA.ST'),  -- ALFA: SEK 565.2 on 2026-09-28
    ('XSTO','SE0007100581','ASSA-B.ST'),  -- ASSA-B: SEK 357.7 on 2026-09-28
    ('XSTO','SE0017486889','ATCO-A.ST'),  -- ATCO-A: SEK 203.6 on 2026-09-28
    ('XSTO','GB0009895292','AZN.ST'),  -- AZN: SEK 1650.5 on 2026-09-28
    ('XSTO','SE0020050417','BOL.ST'),  -- BOL: SEK 508.6 on 2026-09-28
    ('XSTO','SE0015658109','EPI-A.ST'),  -- EPI-A: SEK 256.3 on 2026-09-28
    ('XSTO','SE0012853455','EQT.ST'),  -- EQT: SEK 283.5 on 2026-09-28
    ('XSTO','SE0000108656','ERIC-B.ST'),  -- ERIC-B: SEK 92.44 on 2026-09-28
    ('XSTO','SE0009922164','ESSITY-B.ST'),  -- ESSITY-B: SEK 259.1 on 2026-09-28
    ('XSTO','SE0015961909','HEXA-B.ST'),  -- HEXA-B: SEK 97.28 on 2026-09-28
    ('XSTO','SE0000106270','HM-B.ST'),  -- HM-B: SEK 161.05 on 2026-09-28
    ('XSTO','SE0000107203','INDU-C.ST'),  -- INDU-C: SEK 537.4 on 2026-09-28
    ('XSTO','SE0015811963','INVE-B.ST'),  -- INVE-B: SEK 408.15 on 2026-09-28
    ('XSTO','FI4000297767','NDA-SE.ST'),  -- NDA-SE: SEK 202.6 on 2026-09-28
    ('XSTO','SE0000667891','SAND.ST'),  -- SAND: SEK 374.1 on 2026-09-28
    ('XSTO','SE0000148884','SEB-A.ST'),  -- SEB-A: SEK 235.9 on 2026-09-28
    ('XSTO','SE0007100599','SHB-A.ST'),  -- SHB-A: SEK 155.75 on 2026-09-28
    ('XSTO','SE0000113250','SKA-B.ST'),  -- SKA-B: SEK 281.6 on 2026-09-28
    ('XSTO','SE0000108227','SKF-B.ST'),  -- SKF-B: SEK 270.1 on 2026-09-28
    ('XSTO','SE0000242455','SWED-A.ST'),  -- SWED-A: SEK 411 on 2026-09-28
    ('XSTO','SE0005190238','TEL2-B.ST'),  -- TEL2-B: SEK 166.6 on 2026-09-28
    ('XSTO','SE0000667925','TELIA.ST'),  -- TELIA: SEK 45.38 on 2026-09-28
    ('XSTO','SE0000115446','VOLV-B.ST')   -- VOLV-B: SEK 325.9 on 2026-09-28
) AS mapped(mic, isin, symbol)
JOIN exchanges e ON e.mic = mapped.mic
JOIN instruments i ON i.exchange_id = e.id AND i.isin = mapped.isin AND i.active;
