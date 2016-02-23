CREATE TABLE oauth_clients (
  id bigserial PRIMARY KEY NOT NULL,

  client_id character varying(255) DEFAULT encode(gen_random_bytes(16), 'hex') NOT NULL,
  client_secret character varying(255) DEFAULT encode(gen_random_bytes(64), 'hex') NOT NULL,

  email character varying(255) DEFAULT '' NOT NULL,
  name character varying(255) DEFAULT '' NOT NULL,
  organization character varying(255) DEFAULT '' NOT NULL,

  created_at timestamp without time zone DEFAULT now() NOT NULL,
  updated_at timestamp without time zone DEFAULT now() NOT NULL,
  deleted_at timestamp without time zone
);

CREATE UNIQUE INDEX index_oauth_clients_on_client_id ON oauth_clients USING btree (client_id);

INSERT INTO oauth_clients (
  email,
  name,
  organization
) VALUES (
  'admin@nitrous.io',
  'Rise CLI',
  'Nitrous, Inc.'
);

INSERT INTO oauth_clients (
  email,
  name,
  organization
) VALUES (
  'admin@nitrous.io',
  'Rise Web',
  'Nitrous, Inc.'
);
