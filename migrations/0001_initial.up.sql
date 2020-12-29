BEGIN;

CREATE TABLE builds (
  id uuid PRIMARY KEY,
  created timestamptz NOT NULL DEFAULT now(),
  updated timestamptz,
  completed timestamptz,
  github_repo text,
  github_ref text,
  encrypted_github_credential bytea,
  image_repos text[],
  tags text[],
  commit_sha_tag boolean,
  build_options jsonb, -- misc build options from the request
  request jsonb,  -- serialized protobuf BuildRequest
  status integer,
  events text[]  -- ordered array of build event strings
);

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE api_keys (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    created timestamptz NOT NULL DEFAULT now(),
    name text DEFAULT '',
    description text DEFAULT '',
    github_user text DEFAULT '',
    read_only boolean NOT NULL DEFAULT false
);

INSERT INTO api_keys (name, description) VALUES ('root', 'root api key');

CREATE OR REPLACE FUNCTION trigger_set_updated_timestamp()
    RETURNS TRIGGER AS $$
BEGIN
    NEW.updated = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER set_updated_timestamp_builds
    BEFORE UPDATE ON builds
    FOR EACH ROW
EXECUTE PROCEDURE trigger_set_updated_timestamp();

COMMIT;