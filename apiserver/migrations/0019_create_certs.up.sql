CREATE TABLE certs (
  id bigserial PRIMARY KEY NOT NULL,

  domain_id bigint REFERENCES domains(id) NOT NULL,

  certificate_path character varying(255) NOT NULL,
  private_key_path character varying(255) NOT NULL,

  common_name character varying(255),
  starts_at timestamp without time zone NOT NULL,
  expires_at timestamp without time zone NOT NULL,

  created_at timestamp without time zone DEFAULT now() NOT NULL,
  updated_at timestamp without time zone DEFAULT now() NOT NULL,
  deleted_at timestamp without time zone
);

CREATE UNIQUE INDEX index_certs_on_domain_id ON certs USING btree (domain_id) WHERE deleted_at IS NULL;
