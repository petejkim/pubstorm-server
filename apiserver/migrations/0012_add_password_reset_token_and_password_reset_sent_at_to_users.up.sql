ALTER TABLE users ADD COLUMN password_reset_token character varying(255) DEFAULT NULL;
ALTER TABLE users ADD COLUMN password_reset_token_created_at timestamp without time zone;
