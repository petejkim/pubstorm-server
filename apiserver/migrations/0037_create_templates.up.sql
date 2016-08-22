CREATE TABLE templates (
  id bigserial PRIMARY KEY NOT NULL,

  name character varying(255) NOT NULL,
  rank bigint DEFAULT 0 NOT NULL,
  download_url text,
  preview_url text,
  preview_image_url text,

  created_at timestamp without time zone DEFAULT now() NOT NULL,
  updated_at timestamp without time zone DEFAULT now() NOT NULL,
  deleted_at timestamp without time zone
);
