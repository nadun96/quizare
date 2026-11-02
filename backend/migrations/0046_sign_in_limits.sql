-- Sign-in limits the admin sets (D-126): how many sign-ins one internet
-- address may make at once and then each second (a school behind one router
-- signs in together), and how many passwords are checked at a time (null:
-- QP_ARGON2_WORKERS). The per-account limit, which stops password guessing,
-- stays fixed.
ALTER TABLE auth.policy
    ADD COLUMN network_burst   integer      NOT NULL DEFAULT 600 CHECK (network_burst BETWEEN 50 AND 5000),
    ADD COLUMN network_per_sec numeric(5,1) NOT NULL DEFAULT 20  CHECK (network_per_sec BETWEEN 1 AND 200),
    ADD COLUMN password_workers integer              CHECK (password_workers BETWEEN 1 AND 8);
