ALTER TABLE users ADD COLUMN confirmation_code character varying(255) DEFAULT lpad((floor(random() * 999999) + 1)::text, 6, '0') NOT NULL;
ALTER TABLE users ADD COLUMN confirmed_at timestamp without time zone;
