-- Feature 014: sector classification as curated reference data.
--
-- Sector was null for every instrument in the curated universe. The seed migration never
-- populated it, the code path that would write it has no caller, and this deployment's
-- market-data plan excludes fundamental data — so the provider cannot supply it. The interface
-- meanwhile offered a sector filter whose every choice returned nothing.
--
-- The classification below is the project's own editorial judgement about each company's
-- primary business, using conventional sector names. It is not a reproduction of a licensed
-- classification's assignments, and nothing here claims conformance to one. Each row records
-- where it came from and when it was reviewed, so a stale classification reads as stale rather
-- than as current fact.
--
-- The vocabulary contains an explicit `unclassified` member and the column becomes NOT NULL
-- against it. That is deliberate: it makes "no classification at all" — the state this feature
-- exists to end — unrepresentable rather than merely discouraged.

CREATE TABLE sectors (
    code text PRIMARY KEY CHECK (btrim(code) <> ''),
    name text NOT NULL UNIQUE CHECK (btrim(name) <> ''),
    display_order int NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE sectors IS
    'The closed vocabulary the instrument sector filter offers. Adding a value is a reviewed migration, so a typo cannot create a sector.';

INSERT INTO sectors (code, name, display_order) VALUES
    ('communication_services', 'Communication Services', 1),
    ('consumer_discretionary', 'Consumer Discretionary', 2),
    ('consumer_staples', 'Consumer Staples', 3),
    ('energy', 'Energy', 4),
    ('financials', 'Financials', 5),
    ('health_care', 'Health Care', 6),
    ('industrials', 'Industrials', 7),
    ('information_technology', 'Information Technology', 8),
    ('materials', 'Materials', 9),
    ('real_estate', 'Real Estate', 10),
    ('utilities', 'Utilities', 11),
    -- Last on purpose: it is a stated answer, not a preferred one.
    ('unclassified', 'Unclassified', 99);

ALTER TABLE instruments
    ADD COLUMN sector_source text NOT NULL DEFAULT 'unclassified',
    ADD COLUMN sector_reviewed_on date NOT NULL DEFAULT CURRENT_DATE;

COMMENT ON COLUMN instruments.sector_source IS
    'Where this classification came from. Curated data with no provenance is indistinguishable from current data.';
COMMENT ON COLUMN instruments.sector_reviewed_on IS
    'When the classification was last checked. What makes a stale classification visible as stale.';

-- Any value already stored is free text from before this vocabulary existed. Map the display
-- names this project used to their codes; anything else becomes unclassified rather than
-- blocking the migration.
CREATE TEMP TABLE sector_assignment (isin text PRIMARY KEY, code text) ON COMMIT DROP;

INSERT INTO sector_assignment (isin, code) VALUES
    ('FI0009007884','communication_services'),  -- ELISA, Elisa Oyj
    ('NO0010063308','communication_services'),  -- TEL, Telenor ASA
    ('SE0005190238','communication_services'),  -- TEL2-B, Tele2 AB B
    ('SE0000667925','communication_services'),  -- TELIA, Telia Company AB
    ('NO0010736879','communication_services'),  -- VEND, Vend Marketplaces ASA
    ('SE0000106270','consumer_discretionary'),  -- HM-B, Hennes & Mauritz AB B
    ('DK0060252690','consumer_discretionary'),  -- PNDORA, Pandora A/S
    ('FI0009005318','consumer_discretionary'),  -- TYRES, Nokian Renkaat Oyj
    ('FO0000000179','consumer_staples'),  -- BAKKA, Bakkafrost P/F
    ('SE0009922164','consumer_staples'),  -- ESSITY-B, Essity AB B
    ('FI0009000202','consumer_staples'),  -- KESKOB, Kesko Oyj B
    ('NO0003054108','consumer_staples'),  -- MOWI, Mowi ASA
    ('NO0003733800','consumer_staples'),  -- ORK, Orkla ASA
    ('NO0010310956','consumer_staples'),  -- SALM, SalMar ASA
    ('NO0010345853','energy'),  -- AKRBP, Aker BP ASA
    ('BMG173841013','energy'),  -- BWLPG, BW LPG Ltd (ISIN corrected by 0014)
    ('NO0012851874','energy'),  -- DOFG, DOF Group ASA
    ('NO0010096985','energy'),  -- EQNR, Equinor ASA
    ('CY0200352116','energy'),  -- FRO, Frontline plc
    ('FI0009013296','energy'),  -- NESTE, Neste Oyj
    ('LU0075646355','energy'),  -- SUBC, Subsea 7 S.A.
    ('NO0011202772','energy'),  -- VAR, Vår Energi ASA
    ('DK0010311471','financials'),  -- AL, AL Sydbank A/S
    ('DK0010274414','financials'),  -- DANSKE, Danske Bank A/S
    ('NO0010161896','financials'),  -- DNB, DNB Bank ASA
    ('SE0012853455','financials'),  -- EQT, EQT AB
    ('NO0010582521','financials'),  -- GJF, Gjensidige Forsikring ASA
    ('SE0000107203','financials'),  -- INDU-C, Industrivärden AB C
    ('SE0015811963','financials'),  -- INVE-B, Investor AB B
    ('DK0010307958','financials'),  -- JYSK, Jyske Bank A/S
    ('FI4000552526','financials'),  -- MANTA, Mandatum Oyj
    ('FI4000297767','financials'),  -- NDA-SE, Nordea Bank Abp
    ('NO0010209331','financials'),  -- PROT, Protector Forsikring ASA
    ('DK0060854669','financials'),  -- RILBA, Ringkjøbing Landbobank A/S
    ('FI4000552500','financials'),  -- SAMPO, Sampo Oyj A
    ('SE0000148884','financials'),  -- SEB-A, Skandinaviska Enskilda Banken AB A
    ('SE0007100599','financials'),  -- SHB-A, Svenska Handelsbanken AB A
    ('NO0003053605','financials'),  -- STB, Storebrand ASA
    ('SE0000242455','financials'),  -- SWED-A, Swedbank AB A
    ('DK0060636678','financials'),  -- TRYG, Tryg A/S
    ('DK0060946788','health_care'),  -- AMBU-B, Ambu A/S B
    ('GB0009895292','health_care'),  -- AZN, AstraZeneca PLC
    ('DK0015998017','health_care'),  -- BAVA, Bavarian Nordic A/S
    ('DK0060448595','health_care'),  -- COLO-B, Coloplast A/S B
    ('DK0060738599','health_care'),  -- DEMANT, Demant A/S
    ('DK0010272202','health_care'),  -- GMAB, Genmab A/S
    ('DK0010272632','health_care'),  -- GN, GN Store Nord A/S
    ('DK0062498333','health_care'),  -- NOVO-B, Novo Nordisk A/S B
    ('FI0009014377','health_care'),  -- ORNBV, Orion Oyj B
    ('DK0060257814','health_care'),  -- ZEAL, Zealand Pharma A/S
    ('CH0012221716','industrials'),  -- ABB, ABB Ltd
    ('SE0014781795','industrials'),  -- ADDT-B, Addtech AB B
    ('SE0000695876','industrials'),  -- ALFA, Alfa Laval AB
    ('SE0007100581','industrials'),  -- ASSA-B, Assa Abloy AB B
    ('SE0017486889','industrials'),  -- ATCO-A, Atlas Copco AB A
    ('DK0060079531','industrials'),  -- DSV, DSV A/S
    ('SE0015658109','industrials'),  -- EPI-A, Epiroc AB A
    ('DK0010234467','industrials'),  -- FLS, FLSmidth & Co. A/S
    ('NO0011082075','industrials'),  -- HAUTO, Höegh Autoliners ASA
    ('FI4000571013','industrials'),  -- HIAB, Hiab Oyj
    ('DK0060542181','industrials'),  -- ISS, ISS A/S
    ('FI0009005870','industrials'),  -- KCR, Konecranes Oyj
    ('FI0009013403','industrials'),  -- KNEBV, KONE Oyj B
    ('NO0013536151','industrials'),  -- KOG, Kongsberg Gruppen ASA
    ('DK0010244425','industrials'),  -- MAERSK-A, A.P. Møller - Mærsk A/S A
    ('DK0010244508','industrials'),  -- MAERSK-B, A.P. Møller - Mærsk A/S B
    ('FI0009014575','industrials'),  -- MOCORP, Metso Oyj
    ('NO0010196140','industrials'),  -- NAS, Norwegian Air Shuttle ASA
    ('DK0010287663','industrials'),  -- NKT, NKT A/S
    ('DK0063855168','industrials'),  -- ROCK-B, Rockwool A/S B (ISIN corrected by 0014)
    ('SE0000667891','industrials'),  -- SAND, Sandvik AB
    ('SE0000113250','industrials'),  -- SKA-B, Skanska AB B
    ('SE0000108227','industrials'),  -- SKF-B, SKF AB B
    ('NO0012470089','industrials'),  -- TOM, Tomra Systems ASA
    ('FI4000074984','industrials'),  -- VALMT, Valmet Oyj
    ('SE0000115446','industrials'),  -- VOLV-B, Volvo AB B
    ('DK0061539921','industrials'),  -- VWS, Vestas Wind Systems A/S
    ('NO0010571680','industrials'),  -- WAWI, Wallenius Wilhelmsen ASA
    ('FI0009003727','industrials'),  -- WRT1V, Wärtsilä Oyj Abp
    ('FI0009007264','information_technology'),  -- BITTI, Bittium Oyj
    ('SE0000108656','information_technology'),  -- ERIC-B, Ericsson B
    ('SE0015961909','information_technology'),  -- HEXA-B, Hexagon AB B
    ('NO0003055501','information_technology'),  -- NOD, Nordic Semiconductor ASA
    ('FI0009000681','information_technology'),  -- NOKIA, Nokia Oyj
    ('FI4000198031','information_technology'),  -- QTCOM, Qt Group Oyj
    ('FI0009000277','information_technology'),  -- TIETO, Tietoevry Oyj
    ('SE0020050417','materials'),  -- BOL, Boliden AB
    ('FI0009000459','materials'),  -- HUH1V, Huhtamäki Oyj
    ('FI0009004824','materials'),  -- KEMIRA, Kemira Oyj
    ('NO0005052605','materials'),  -- NHY, Norsk Hydro ASA
    ('DK0060336014','materials'),  -- NSIS-B, Novonesis A/S B
    ('FI0009002422','materials'),  -- OUT1V, Outokumpu Oyj
    ('FI0009005961','materials'),  -- STERV, Stora Enso Oyj R
    ('FI0009005987','materials'),  -- UPM, UPM-Kymmene Oyj
    ('NO0010208051','materials'),  -- YAR, Yara International ASA
    ('FI4000312251','real_estate'),  -- KOJAMO, Kojamo Oyj
    ('FI0009007132','utilities'),  -- FORTUM, Fortum Oyj
    ('DK0060094928','utilities')  -- ORSTED, Ørsted A/S
;

UPDATE instruments i SET
    sector = s.name,
    sector_source = 'curated-2026-09',
    sector_reviewed_on = DATE '2026-09-02'
FROM sector_assignment a JOIN sectors s ON s.code = a.code
WHERE i.isin = a.isin;

-- Everything the assignment did not name, including any free text an earlier sync stored.
UPDATE instruments SET sector = 'Unclassified', sector_source = 'unclassified'
WHERE sector IS NULL OR sector NOT IN (SELECT name FROM sectors);

-- Store the code rather than the display name, so renaming a sector is one row.
UPDATE instruments i SET sector = s.code FROM sectors s WHERE i.sector = s.name;

ALTER TABLE instruments
    ALTER COLUMN sector SET DEFAULT 'unclassified',
    ALTER COLUMN sector SET NOT NULL,
    ADD CONSTRAINT instruments_sector_fkey FOREIGN KEY (sector) REFERENCES sectors(code);

CREATE INDEX instruments_sector_idx ON instruments (sector);
