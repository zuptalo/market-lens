INSERT INTO exchanges (id, mic, name, country, currency, timezone, active) VALUES
    ('20000000-0000-4000-8000-000000000001', 'XSTO', 'Nasdaq Stockholm',  'SE', 'SEK', 'Europe/Stockholm',  true),
    ('20000000-0000-4000-8000-000000000002', 'XCSE', 'Nasdaq Copenhagen', 'DK', 'DKK', 'Europe/Copenhagen', true),
    ('20000000-0000-4000-8000-000000000003', 'XHEL', 'Nasdaq Helsinki',   'FI', 'EUR', 'Europe/Helsinki',   true),
    ('20000000-0000-4000-8000-000000000004', 'XOSL', 'Oslo Børs',         'NO', 'NOK', 'Europe/Oslo',       true);

INSERT INTO research_universes (id, code, name, description, active) VALUES
    ('21000000-0000-4000-8000-000000000001', 'nordic-liquid-v1', 'Nordic Liquid Equities',
     'User-reviewed liquid common-equity listings from the primary Stockholm, Copenhagen, Helsinki, and Oslo markets. Broker availability is not guaranteed.', true);

CREATE TEMP TABLE nordic_universe_seed (
    id uuid, mic text, isin text, ticker text, name text, provider_symbol text, selection_basis text
) ON COMMIT DROP;

INSERT INTO nordic_universe_seed VALUES
    ('22000000-0000-4000-8000-000000000001','XSTO','SE0015811963','INVE-B','Investor AB B','INVE-B.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000002','XSTO','SE0000115446','VOLV-B','Volvo AB B','VOLV-B.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000003','XSTO','SE0017486889','ATCO-A','Atlas Copco AB A','ATCO-A.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000004','XSTO','SE0000667891','SAND','Sandvik AB','SAND.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000005','XSTO','SE0000242455','SWED-A','Swedbank AB A','SWED-A.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000006','XSTO','CH0012221716','ABB','ABB Ltd','ABB.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000007','XSTO','SE0000148884','SEB-A','Skandinaviska Enskilda Banken AB A','SEB-A.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000008','XSTO','SE0007100581','ASSA-B','Assa Abloy AB B','ASSA-B.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000009','XSTO','SE0000108656','ERIC-B','Ericsson B','ERIC-B.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000010','XSTO','GB0009895292','AZN','AstraZeneca PLC','AZN.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000011','XSTO','SE0007100599','SHB-A','Svenska Handelsbanken AB A','SHB-A.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000012','XSTO','SE0015961909','HEXA-B','Hexagon AB B','HEXA-B.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000013','XSTO','SE0012853455','EQT','EQT AB','EQT.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000014','XSTO','SE0000695876','ALFA','Alfa Laval AB','ALFA.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000015','XSTO','SE0015658109','EPI-A','Epiroc AB A','EPI-A.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000016','XSTO','SE0020050417','BOL','Boliden AB','BOL.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000017','XSTO','SE0009922164','ESSITY-B','Essity AB B','ESSITY-B.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000018','XSTO','SE0000667925','TELIA','Telia Company AB','TELIA.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000019','XSTO','SE0000113250','SKA-B','Skanska AB B','SKA-B.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000020','XSTO','FI4000297767','NDA-SE','Nordea Bank Abp','NDA-SE.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000021','XSTO','SE0000108227','SKF-B','SKF AB B','SKF-B.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000022','XSTO','SE0005190238','TEL2-B','Tele2 AB B','TEL2-B.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000023','XSTO','SE0000107203','INDU-C','Industrivärden AB C','INDU-C.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000024','XSTO','SE0014781795','ADDT-B','Addtech AB B','ADDT-B.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),
    ('22000000-0000-4000-8000-000000000025','XSTO','SE0000106270','HM-B','Hennes & Mauritz AB B','HM-B.ST','XACT OMXS30 ESG holdings, highest-weight reviewed listings'),

    ('22000000-0000-4000-8000-000000000026','XCSE','DK0062498333','NOVO-B','Novo Nordisk A/S B','NOVO-B.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000027','XCSE','DK0060079531','DSV','DSV A/S','DSV.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000028','XCSE','DK0010274414','DANSKE','Danske Bank A/S','DANSKE.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000029','XCSE','DK0061539921','VWS','Vestas Wind Systems A/S','VWS.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000030','XCSE','DK0060336014','NSIS-B','Novonesis A/S B','NSIS-B.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000031','XCSE','DK0010272202','GMAB','Genmab A/S','GMAB.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000032','XCSE','DK0060094928','ORSTED','Ørsted A/S','ORSTED.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000033','XCSE','DK0010244425','MAERSK-A','A.P. Møller - Mærsk A/S A','MAERSK-A.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000034','XCSE','DK0010244508','MAERSK-B','A.P. Møller - Mærsk A/S B','MAERSK-B.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000035','XCSE','DK0060448595','COLO-B','Coloplast A/S B','COLO-B.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000036','XCSE','DK0060252690','PNDORA','Pandora A/S','PNDORA.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000037','XCSE','DK0010311471','AL','AL Sydbank A/S','AL.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000038','XCSE','DK0010287663','NKT','NKT A/S','NKT.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000039','XCSE','DK0060636678','TRYG','Tryg A/S','TRYG.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000040','XCSE','DK0010307958','JYSK','Jyske Bank A/S','JYSK.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000041','XCSE','DK0060854669','RILBA','Ringkjøbing Landbobank A/S','RILBA.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000042','XCSE','DK0060542181','ISS','ISS A/S','ISS.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000043','XCSE','FI4000297767','NDA-DK','Nordea Bank Abp','NDA-DK.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000044','XCSE','DK0060738599','DEMANT','Demant A/S','DEMANT.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000045','XCSE','DK0010234467','FLS','FLSmidth & Co. A/S','FLS.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000046','XCSE','DK0060257814','ZEAL','Zealand Pharma A/S','ZEAL.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000047','XCSE','DK0010219153','ROCK-B','Rockwool A/S B','ROCK-B.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000048','XCSE','DK0015998017','BAVA','Bavarian Nordic A/S','BAVA.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000049','XCSE','DK0010272632','GN','GN Store Nord A/S','GN.CO','XACT OMXC25 ESG holdings'),
    ('22000000-0000-4000-8000-000000000050','XCSE','DK0060946788','AMBU-B','Ambu A/S B','AMBU-B.CO','XACT OMXC25 ESG holdings'),

    ('22000000-0000-4000-8000-000000000051','XHEL','FI0009007264','BITTI','Bittium Oyj','BITTI.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000052','XHEL','FI0009007884','ELISA','Elisa Oyj','ELISA.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000053','XHEL','FI0009007132','FORTUM','Fortum Oyj','FORTUM.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000054','XHEL','FI4000571013','HIAB','Hiab Oyj','HIAB.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000055','XHEL','FI0009000459','HUH1V','Huhtamäki Oyj','HUH1V.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000056','XHEL','FI0009005870','KCR','Konecranes Oyj','KCR.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000057','XHEL','FI0009004824','KEMIRA','Kemira Oyj','KEMIRA.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000058','XHEL','FI0009000202','KESKOB','Kesko Oyj B','KESKOB.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000059','XHEL','FI0009013403','KNEBV','KONE Oyj B','KNEBV.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000060','XHEL','FI4000312251','KOJAMO','Kojamo Oyj','KOJAMO.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000061','XHEL','FI4000552526','MANTA','Mandatum Oyj','MANTA.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000062','XHEL','FI0009014575','MOCORP','Metso Oyj','MOCORP.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000063','XHEL','FI4000297767','NDA-FI','Nordea Bank Abp','NDA-FI.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000064','XHEL','FI0009013296','NESTE','Neste Oyj','NESTE.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000065','XHEL','FI0009000681','NOKIA','Nokia Oyj','NOKIA.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000066','XHEL','FI0009014377','ORNBV','Orion Oyj B','ORNBV.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000067','XHEL','FI0009002422','OUT1V','Outokumpu Oyj','OUT1V.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000068','XHEL','FI4000198031','QTCOM','Qt Group Oyj','QTCOM.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000069','XHEL','FI4000552500','SAMPO','Sampo Oyj A','SAMPO.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000070','XHEL','FI0009005961','STERV','Stora Enso Oyj R','STERV.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000071','XHEL','FI0009000277','TIETO','Tietoevry Oyj','TIETO.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000072','XHEL','FI0009005318','TYRES','Nokian Renkaat Oyj','TYRES.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000073','XHEL','FI0009005987','UPM','UPM-Kymmene Oyj','UPM.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000074','XHEL','FI4000074984','VALMT','Valmet Oyj','VALMT.HE','OMXH25 composition effective 2026-08-03'),
    ('22000000-0000-4000-8000-000000000075','XHEL','FI0009003727','WRT1V','Wärtsilä Oyj Abp','WRT1V.HE','OMXH25 composition effective 2026-08-03'),

    ('22000000-0000-4000-8000-000000000076','XOSL','NO0010345853','AKRBP','Aker BP ASA','AKRBP.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000077','XOSL','FO0000000179','BAKKA','Bakkafrost P/F','BAKKA.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000078','XOSL','SGXZ69436764','BWLPG','BW LPG Ltd','BWLPG.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000079','XOSL','NO0010161896','DNB','DNB Bank ASA','DNB.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000080','XOSL','NO0012851874','DOFG','DOF Group ASA','DOFG.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000081','XOSL','NO0010096985','EQNR','Equinor ASA','EQNR.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000082','XOSL','CY0200352116','FRO','Frontline plc','FRO.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000083','XOSL','NO0010582521','GJF','Gjensidige Forsikring ASA','GJF.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000084','XOSL','NO0011082075','HAUTO','Höegh Autoliners ASA','HAUTO.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000085','XOSL','NO0013536151','KOG','Kongsberg Gruppen ASA','KOG.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000086','XOSL','NO0003054108','MOWI','Mowi ASA','MOWI.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000087','XOSL','NO0003055501','NOD','Nordic Semiconductor ASA','NOD.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000088','XOSL','NO0005052605','NHY','Norsk Hydro ASA','NHY.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000089','XOSL','NO0010196140','NAS','Norwegian Air Shuttle ASA','NAS.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000090','XOSL','NO0003733800','ORK','Orkla ASA','ORK.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000091','XOSL','NO0010209331','PROT','Protector Forsikring ASA','PROT.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000092','XOSL','NO0010310956','SALM','SalMar ASA','SALM.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000093','XOSL','NO0003053605','STB','Storebrand ASA','STB.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000094','XOSL','LU0075646355','SUBC','Subsea 7 S.A.','SUBC.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000095','XOSL','NO0010063308','TEL','Telenor ASA','TEL.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000096','XOSL','NO0012470089','TOM','Tomra Systems ASA','TOM.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000097','XOSL','NO0010736879','VEND','Vend Marketplaces ASA','VEND.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000098','XOSL','NO0011202772','VAR','Vår Energi ASA','VAR.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000099','XOSL','NO0010571680','WAWI','Wallenius Wilhelmsen ASA','WAWI.OL','Euronext OBX composition reviewed 2026-08-28'),
    ('22000000-0000-4000-8000-000000000100','XOSL','NO0010208051','YAR','Yara International ASA','YAR.OL','Euronext OBX composition reviewed 2026-08-28');

INSERT INTO instruments
    (id, exchange_id, isin, ticker, name, currency, country, instrument_type, active, purchasability_status)
SELECT s.id, e.id, s.isin, s.ticker, s.name, e.currency, e.country, 'common_stock', true, 'unverified'
FROM nordic_universe_seed s JOIN exchanges e ON e.mic = s.mic;

INSERT INTO provider_instruments (provider, provider_symbol, instrument_id, active)
SELECT 'eodhd', provider_symbol, id, true FROM nordic_universe_seed;

INSERT INTO universe_memberships
    (universe_id, instrument_id, included_from, curation_source, curation_note)
SELECT '21000000-0000-4000-8000-000000000001', id, DATE '2026-08-28',
       'User review using official/current benchmark composition sources', selection_basis
FROM nordic_universe_seed;
