-- ============================================================================
-- 01-create-userdata.sql — postgres init script
-- ----------------------------------------------------------------------------
-- Creates the `userdata` database used for federated transactional tables
-- (the Seeds plugin / param tables). Runs ONCE when the postgres volume is
-- first initialised — Docker's postgres entrypoint executes everything in
-- /docker-entrypoint-initdb.d/ in lexical order on a fresh data dir.
--
-- For existing installs where the volume already exists, run manually:
--   docker exec infra-postgres-1 psql -U rat -d rat -c "CREATE DATABASE userdata"
--
-- The "rat" role owns it. ratq attaches to it from DuckDB via the postgres
-- extension (see query/src/rat_query/engine.py).
-- ============================================================================

CREATE DATABASE userdata;
GRANT ALL PRIVILEGES ON DATABASE userdata TO rat;

\connect userdata
-- The public schema exists by default; just make sure the rat role can
-- create tables in it.
GRANT ALL ON SCHEMA public TO rat;
