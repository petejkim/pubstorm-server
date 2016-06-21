CREATE TABLE acme_certs (
  id bigserial PRIMARY KEY NOT NULL,

  domain_id bigint REFERENCES domains(id) NOT NULL,

  letsencrypt_key text NOT NULL,
  private_key text,
  cert text,

  http_challenge_path character varying(255),
  http_challenge_resource character varying(255),

  created_at timestamp without time zone DEFAULT now() NOT NULL,
  updated_at timestamp without time zone DEFAULT now() NOT NULL,
  deleted_at timestamp without time zone
);

CREATE UNIQUE INDEX index_acme_certs_on_domain_id ON acme_certs USING btree (domain_id) WHERE deleted_at IS NULL;
CREATE INDEX index_acme_certs_on_http_challenge_path ON acme_certs USING btree (http_challenge_path) WHERE deleted_at IS NULL;
