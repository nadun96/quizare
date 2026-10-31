-- Changing one's email (D-98): the new address waits in pending_email until
-- the link sent to it is followed; logging in keeps the old one until then.
ALTER TABLE auth.users ADD COLUMN pending_email text;
ALTER TABLE auth.tokens DROP CONSTRAINT tokens_purpose_check;
ALTER TABLE auth.tokens ADD CONSTRAINT tokens_purpose_check CHECK (purpose IN ('verify_email', 'reset_password', 'change_email'));
