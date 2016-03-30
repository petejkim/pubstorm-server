CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
  id bigserial PRIMARY KEY NOT NULL,
  email character varying(255) NOT NULL,
  encrypted_password character varying(255) NOT NULL,
  name character varying(255) DEFAULT '' NOT NULL,
  organization character varying(255) DEFAULT '' NOT NULL,

  created_at timestamp without time zone DEFAULT now() NOT NULL,
  updated_at timestamp without time zone DEFAULT now() NOT NULL,
  deleted_at timestamp without time zone
);

CREATE UNIQUE INDEX index_users_on_email ON users USING btree (email) WHERE deleted_at IS NULL;

INSERT INTO users (
  email,
  encrypted_password,
  name,
  organization
) VALUES (
  'admin@nitrous.io',
  crypt(encode(gen_random_bytes(48), 'base64'), gen_salt('bf')),
  'Administrator',
  'Nitrous, Inc.'
);
