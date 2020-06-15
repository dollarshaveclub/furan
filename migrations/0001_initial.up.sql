BEGIN;

CREATE TABLE builds (
  id uuid PRIMARY KEY,
  created timestamptz NOT NULL DEFAULT now(),
  updated timestamptz,
  completed timestamptz,
  github_repo text,
  github_ref text,
  image_repo text,
  tags text[],
  commit_sha_tag boolean,
  request jsonb,  -- serialized protobuf BuildRequest
  status integer,
  events text[]  -- ordered array of build event strings
);

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