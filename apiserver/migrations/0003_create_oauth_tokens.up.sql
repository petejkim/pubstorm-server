CREATE TABLE oauth_tokens (
  id bigserial PRIMARY KEY NOT NULL,

  user_id bigint REFERENCES users(id) NOT NULL,
  oauth_client_id bigint REFERENCES oauth_clients(id) NOT NULL,
  token character varying(255) DEFAULT encode(gen_random_bytes(64), 'hex') NOT NULL,

  created_at timestamp without time zone DEFAULT now() NOT NULL
);

CREATE INDEX index_oauth_tokens_on_user_id ON oauth_tokens USING btree (user_id);
CREATE INDEX index_oauth_tokens_on_oauth_client_id ON oauth_tokens USING btree (oauth_client_id);
CREATE UNIQUE INDEX index_oauth_tokens_on_token ON oauth_tokens USING btree (token);
