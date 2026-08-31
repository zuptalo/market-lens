-- Member login codes are six digits, so only one million distinct values exist. A global
-- uniqueness constraint over every challenge ever issued therefore starts rejecting
-- issuance after a few thousand sign-ins and eventually rejects every code. Only codes
-- that are live at the same moment need to be distinct from one another.

ALTER TABLE member_login_challenges
    DROP CONSTRAINT member_login_challenges_code_digest_key;

CREATE UNIQUE INDEX member_login_challenges_active_code_digest_idx
    ON member_login_challenges (code_digest) WHERE state = 'active';
