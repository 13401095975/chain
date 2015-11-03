--
-- PostgreSQL database dump
--

SET statement_timeout = 0;
SET lock_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SET check_function_bodies = false;
SET client_min_messages = warning;

--
-- Name: plpgsql; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS plpgsql WITH SCHEMA pg_catalog;


--
-- Name: EXTENSION plpgsql; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON EXTENSION plpgsql IS 'PL/pgSQL procedural language';


--
-- Name: plv8; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS plv8 WITH SCHEMA pg_catalog;


--
-- Name: EXTENSION plv8; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON EXTENSION plv8 IS 'PL/JavaScript (v8) trusted procedural language';


SET search_path = public, pg_catalog;

--
-- Name: b32enc_crockford(bytea); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION b32enc_crockford(src bytea) RETURNS text
    LANGUAGE plpgsql IMMUTABLE
    AS $$
DECLARE
	-- alphabet is the base32 alphabet defined
	-- by Douglas Crockford. It preserves lexical
	-- order and avoids visually-similar symbols.
	-- See http://www.crockford.com/wrmg/base32.html.
	alphabet text := '0123456789ABCDEFGHJKMNPQRSTVWXYZ';
	dst text := '';
	n integer;
	b0 integer;
	b1 integer;
	b2 integer;
	b3 integer;
	b4 integer;
	b5 integer;
	b6 integer;
	b7 integer;
BEGIN
	FOR r IN 0..(length(src)-1) BY 5
	LOOP
		b0:=0; b1:=0; b2:=0; b3:=0; b4:=0; b5:=0; b6:=0; b7:=0;

		-- Unpack 8x 5-bit source blocks into an 8 byte
		-- destination quantum
		n := length(src) - r;
		IF n >= 5 THEN
			b7 := get_byte(src, r+4) & 31;
			b6 := get_byte(src, r+4) >> 5;
		END IF;
		IF n >= 4 THEN
			b6 := b6 | (get_byte(src, r+3) << 3) & 31;
			b5 := (get_byte(src, r+3) >> 2) & 31;
			b4 := get_byte(src, r+3) >> 7;
		END IF;
		IF n >= 3 THEN
			b4 := b4 | (get_byte(src, r+2) << 1) & 31;
			b3 := (get_byte(src, r+2) >> 4) & 31;
		END IF;
		IF n >= 2 THEN
			b3 := b3 | (get_byte(src, r+1) << 4) & 31;
			b2 := (get_byte(src, r+1) >> 1) & 31;
			b1 := (get_byte(src, r+1) >> 6) & 31;
		END IF;
		b1 := b1 | (get_byte(src, r) << 2) & 31;
		b0 := get_byte(src, r) >> 3;

		-- Encode 5-bit blocks using the base32 alphabet
		dst := dst || substr(alphabet, b0+1, 1);
		dst := dst || substr(alphabet, b1+1, 1);
		IF n >= 2 THEN
			dst := dst || substr(alphabet, b2+1, 1);
			dst := dst || substr(alphabet, b3+1, 1);
		END IF;
		IF n >= 3 THEN
			dst := dst || substr(alphabet, b4+1, 1);
		END IF;
		IF n >= 4 THEN
			dst := dst || substr(alphabet, b5+1, 1);
			dst := dst || substr(alphabet, b6+1, 1);
		END IF;
		IF n >= 5 THEN
			dst := dst || substr(alphabet, b7+1, 1);
		END IF;
	END LOOP;
	RETURN dst;
END;
$$;


--
-- Name: next_chain_id(text); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION next_chain_id(prefix text) RETURNS text
    LANGUAGE plpgsql
    AS $$
DECLARE
	our_epoch_ms bigint := 1433333333333; -- do not change
	seq_id bigint;
	now_ms bigint;     -- from unix epoch, not ours
	shard_id int := 4; -- must be different on each shard
	n bigint;
BEGIN
	SELECT nextval('chain_id_seq') % 1024 INTO seq_id;
	SELECT FLOOR(EXTRACT(EPOCH FROM clock_timestamp()) * 1000) INTO now_ms;
	n := (now_ms - our_epoch_ms) << 23;
	n := n | (shard_id << 10);
	n := n | (seq_id);
	RETURN prefix || b32enc_crockford(int8send(n));
END;
$$;


SET default_tablespace = '';

SET default_with_oids = false;

--
-- Name: accounts; Type: TABLE; Schema: public; Owner: -; Tablespace: 
--

CREATE TABLE accounts (
    id text DEFAULT next_chain_id('acc'::text) NOT NULL,
    manager_node_id text NOT NULL,
    key_index bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now(),
    next_address_index bigint DEFAULT 0 NOT NULL,
    label text
);


--
-- Name: activity; Type: TABLE; Schema: public; Owner: -; Tablespace: 
--

CREATE TABLE activity (
    id text DEFAULT next_chain_id('act'::text) NOT NULL,
    manager_node_id text NOT NULL,
    data json NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    txid text NOT NULL
);


--
-- Name: activity_accounts; Type: TABLE; Schema: public; Owner: -; Tablespace: 
--

CREATE TABLE activity_accounts (
    activity_id text NOT NULL,
    account_id text NOT NULL
);


--
-- Name: address_index_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE address_index_seq
    START WITH 10001
    INCREMENT BY 10000
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: addresses; Type: TABLE; Schema: public; Owner: -; Tablespace: 
--

CREATE TABLE addresses (
    id text DEFAULT next_chain_id('a'::text) NOT NULL,
    manager_node_id text NOT NULL,
    account_id text NOT NULL,
    keyset text[] NOT NULL,
    key_index bigint NOT NULL,
    address text NOT NULL,
    memo text,
    amount bigint,
    is_change boolean DEFAULT false NOT NULL,
    expiration timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    redeem_script bytea NOT NULL,
    pk_script bytea NOT NULL
);


--
-- Name: admin_nodes; Type: TABLE; Schema: public; Owner: -; Tablespace: 
--

CREATE TABLE admin_nodes (
    id text DEFAULT next_chain_id('an'::text) NOT NULL,
    project_id text NOT NULL,
    label text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: assets; Type: TABLE; Schema: public; Owner: -; Tablespace: 
--

CREATE TABLE assets (
    id text NOT NULL,
    issuer_node_id text NOT NULL,
    key_index bigint NOT NULL,
    keyset text[] DEFAULT '{}'::text[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    definition_mutable boolean DEFAULT false NOT NULL,
    definition_url text DEFAULT ''::text NOT NULL,
    definition bytea,
    redeem_script bytea NOT NULL,
    label text NOT NULL,
    issued bigint DEFAULT 0 NOT NULL,
    sort_id text DEFAULT next_chain_id('asset'::text) NOT NULL
);


--
-- Name: auth_tokens; Type: TABLE; Schema: public; Owner: -; Tablespace: 
--

CREATE TABLE auth_tokens (
    id text DEFAULT next_chain_id('at'::text) NOT NULL,
    secret_hash bytea NOT NULL,
    user_id text,
    type text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone
);


--
-- Name: chain_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE chain_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: invitations; Type: TABLE; Schema: public; Owner: -; Tablespace: 
--

CREATE TABLE invitations (
    id text NOT NULL,
    project_id text NOT NULL,
    email text NOT NULL,
    role text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT invitations_role_check CHECK (((role = 'developer'::text) OR (role = 'admin'::text)))
);


--
-- Name: issuance_activity; Type: TABLE; Schema: public; Owner: -; Tablespace: 
--

CREATE TABLE issuance_activity (
    id text DEFAULT next_chain_id('iact'::text) NOT NULL,
    issuer_node_id text NOT NULL,
    data json NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    txid text NOT NULL
);


--
-- Name: issuance_activity_assets; Type: TABLE; Schema: public; Owner: -; Tablespace: 
--

CREATE TABLE issuance_activity_assets (
    issuance_activity_id text NOT NULL,
    asset_id text NOT NULL
);


--
-- Name: issuer_nodes; Type: TABLE; Schema: public; Owner: -; Tablespace: 
--

CREATE TABLE issuer_nodes (
    id text DEFAULT next_chain_id('in'::text) NOT NULL,
    project_id text NOT NULL,
    block_chain text DEFAULT 'sandbox'::text NOT NULL,
    sigs_required integer DEFAULT 1 NOT NULL,
    key_index bigint NOT NULL,
    label text NOT NULL,
    keyset text[] NOT NULL,
    next_asset_index bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    generated_keys text[] DEFAULT '{}'::text[] NOT NULL
);


--
-- Name: issuer_nodes_key_index_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE issuer_nodes_key_index_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: issuer_nodes_key_index_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE issuer_nodes_key_index_seq OWNED BY issuer_nodes.key_index;


--
-- Name: manager_nodes; Type: TABLE; Schema: public; Owner: -; Tablespace: 
--

CREATE TABLE manager_nodes (
    id text DEFAULT next_chain_id('mn'::text) NOT NULL,
    project_id text NOT NULL,
    block_chain text DEFAULT 'sandbox'::text NOT NULL,
    sigs_required integer DEFAULT 1 NOT NULL,
    key_index bigint NOT NULL,
    label text NOT NULL,
    current_rotation text,
    next_asset_index bigint DEFAULT 0 NOT NULL,
    next_account_index bigint DEFAULT 0 NOT NULL,
    accounts_count bigint DEFAULT 0,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    generated_keys text[] DEFAULT '{}'::text[] NOT NULL
);


--
-- Name: manager_nodes_key_index_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE manager_nodes_key_index_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: manager_nodes_key_index_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE manager_nodes_key_index_seq OWNED BY manager_nodes.key_index;


--
-- Name: members; Type: TABLE; Schema: public; Owner: -; Tablespace: 
--

CREATE TABLE members (
    project_id text NOT NULL,
    user_id text NOT NULL,
    role text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT members_role_check CHECK (((role = 'developer'::text) OR (role = 'admin'::text)))
);


--
-- Name: pool_outputs; Type: TABLE; Schema: public; Owner: -; Tablespace: 
--

CREATE TABLE pool_outputs (
    tx_hash text NOT NULL,
    index integer NOT NULL,
    asset_id text NOT NULL,
    issuance_id text,
    script bytea NOT NULL,
    amount bigint NOT NULL,
    spent boolean DEFAULT false NOT NULL,
    addr_index bigint NOT NULL,
    account_id text NOT NULL,
    manager_node_id text NOT NULL,
    reserved_until timestamp with time zone DEFAULT '1979-12-31 16:00:00-08'::timestamp with time zone NOT NULL
);


--
-- Name: pool_txs; Type: TABLE; Schema: public; Owner: -; Tablespace: 
--

CREATE TABLE pool_txs (
    tx_hash text NOT NULL,
    data bytea NOT NULL
);


--
-- Name: projects; Type: TABLE; Schema: public; Owner: -; Tablespace: 
--

CREATE TABLE projects (
    id text DEFAULT next_chain_id('proj'::text) NOT NULL,
    name text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: rotations; Type: TABLE; Schema: public; Owner: -; Tablespace: 
--

CREATE TABLE rotations (
    id text DEFAULT next_chain_id('rot'::text) NOT NULL,
    manager_node_id text NOT NULL,
    keyset text[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: users; Type: TABLE; Schema: public; Owner: -; Tablespace: 
--

CREATE TABLE users (
    id text DEFAULT next_chain_id('u'::text) NOT NULL,
    email text NOT NULL,
    password_hash bytea NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    pwreset_secret_hash bytea,
    pwreset_expires_at timestamp with time zone
);


--
-- Name: utxos; Type: TABLE; Schema: public; Owner: -; Tablespace: 
--

CREATE TABLE utxos (
    txid text NOT NULL,
    index integer NOT NULL,
    asset_id text NOT NULL,
    amount bigint NOT NULL,
    addr_index bigint NOT NULL,
    account_id text NOT NULL,
    manager_node_id text NOT NULL,
    reserved_until timestamp with time zone DEFAULT '1979-12-31 16:00:00-08'::timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    block_hash text,
    block_height bigint
);


--
-- Name: key_index; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY issuer_nodes ALTER COLUMN key_index SET DEFAULT nextval('issuer_nodes_key_index_seq'::regclass);


--
-- Name: key_index; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY manager_nodes ALTER COLUMN key_index SET DEFAULT nextval('manager_nodes_key_index_seq'::regclass);


--
-- Name: accounts_pkey; Type: CONSTRAINT; Schema: public; Owner: -; Tablespace: 
--

ALTER TABLE ONLY accounts
    ADD CONSTRAINT accounts_pkey PRIMARY KEY (id);


--
-- Name: activity_pkey; Type: CONSTRAINT; Schema: public; Owner: -; Tablespace: 
--

ALTER TABLE ONLY activity
    ADD CONSTRAINT activity_pkey PRIMARY KEY (id);


--
-- Name: addresses_address_key; Type: CONSTRAINT; Schema: public; Owner: -; Tablespace: 
--

ALTER TABLE ONLY addresses
    ADD CONSTRAINT addresses_address_key UNIQUE (address);


--
-- Name: addresses_pkey; Type: CONSTRAINT; Schema: public; Owner: -; Tablespace: 
--

ALTER TABLE ONLY addresses
    ADD CONSTRAINT addresses_pkey PRIMARY KEY (id);


--
-- Name: assets_pkey; Type: CONSTRAINT; Schema: public; Owner: -; Tablespace: 
--

ALTER TABLE ONLY assets
    ADD CONSTRAINT assets_pkey PRIMARY KEY (id);


--
-- Name: invitations_pkey; Type: CONSTRAINT; Schema: public; Owner: -; Tablespace: 
--

ALTER TABLE ONLY invitations
    ADD CONSTRAINT invitations_pkey PRIMARY KEY (id);


--
-- Name: issuance_activity_pkey; Type: CONSTRAINT; Schema: public; Owner: -; Tablespace: 
--

ALTER TABLE ONLY issuance_activity
    ADD CONSTRAINT issuance_activity_pkey PRIMARY KEY (id);


--
-- Name: issuer_nodes_pkey; Type: CONSTRAINT; Schema: public; Owner: -; Tablespace: 
--

ALTER TABLE ONLY issuer_nodes
    ADD CONSTRAINT issuer_nodes_pkey PRIMARY KEY (id);


--
-- Name: manager_nodes_pkey; Type: CONSTRAINT; Schema: public; Owner: -; Tablespace: 
--

ALTER TABLE ONLY manager_nodes
    ADD CONSTRAINT manager_nodes_pkey PRIMARY KEY (id);


--
-- Name: members_project_id_user_id_key; Type: CONSTRAINT; Schema: public; Owner: -; Tablespace: 
--

ALTER TABLE ONLY members
    ADD CONSTRAINT members_project_id_user_id_key UNIQUE (project_id, user_id);


--
-- Name: pool_outputs_pkey; Type: CONSTRAINT; Schema: public; Owner: -; Tablespace: 
--

ALTER TABLE ONLY pool_outputs
    ADD CONSTRAINT pool_outputs_pkey PRIMARY KEY (tx_hash, index);


--
-- Name: pool_txs_pkey; Type: CONSTRAINT; Schema: public; Owner: -; Tablespace: 
--

ALTER TABLE ONLY pool_txs
    ADD CONSTRAINT pool_txs_pkey PRIMARY KEY (tx_hash);


--
-- Name: projects_pkey; Type: CONSTRAINT; Schema: public; Owner: -; Tablespace: 
--

ALTER TABLE ONLY projects
    ADD CONSTRAINT projects_pkey PRIMARY KEY (id);


--
-- Name: rotations_pkey; Type: CONSTRAINT; Schema: public; Owner: -; Tablespace: 
--

ALTER TABLE ONLY rotations
    ADD CONSTRAINT rotations_pkey PRIMARY KEY (id);


--
-- Name: users_pkey; Type: CONSTRAINT; Schema: public; Owner: -; Tablespace: 
--

ALTER TABLE ONLY users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


--
-- Name: utxos_pkey; Type: CONSTRAINT; Schema: public; Owner: -; Tablespace: 
--

ALTER TABLE ONLY utxos
    ADD CONSTRAINT utxos_pkey PRIMARY KEY (txid, index);


--
-- Name: accounts_manager_node_path; Type: INDEX; Schema: public; Owner: -; Tablespace: 
--

CREATE UNIQUE INDEX accounts_manager_node_path ON accounts USING btree (manager_node_id, key_index);


--
-- Name: activity_accounts_account_id_idx; Type: INDEX; Schema: public; Owner: -; Tablespace: 
--

CREATE INDEX activity_accounts_account_id_idx ON activity_accounts USING btree (account_id);


--
-- Name: activity_accounts_activity_id_account_id_idx; Type: INDEX; Schema: public; Owner: -; Tablespace: 
--

CREATE UNIQUE INDEX activity_accounts_activity_id_account_id_idx ON activity_accounts USING btree (activity_id, account_id);


--
-- Name: activity_manager_node_id_idx; Type: INDEX; Schema: public; Owner: -; Tablespace: 
--

CREATE INDEX activity_manager_node_id_idx ON activity USING btree (manager_node_id);


--
-- Name: activity_manager_node_id_txid_idx; Type: INDEX; Schema: public; Owner: -; Tablespace: 
--

CREATE UNIQUE INDEX activity_manager_node_id_txid_idx ON activity USING btree (manager_node_id, txid);


--
-- Name: addresses_account_id_idx; Type: INDEX; Schema: public; Owner: -; Tablespace: 
--

CREATE INDEX addresses_account_id_idx ON addresses USING btree (account_id);


--
-- Name: addresses_account_id_key_index_idx; Type: INDEX; Schema: public; Owner: -; Tablespace: 
--

CREATE UNIQUE INDEX addresses_account_id_key_index_idx ON addresses USING btree (account_id, key_index);


--
-- Name: addresses_manager_node_id_idx; Type: INDEX; Schema: public; Owner: -; Tablespace: 
--

CREATE INDEX addresses_manager_node_id_idx ON addresses USING btree (manager_node_id);


--
-- Name: assets_issuer_node_id_idx; Type: INDEX; Schema: public; Owner: -; Tablespace: 
--

CREATE INDEX assets_issuer_node_id_idx ON assets USING btree (issuer_node_id);


--
-- Name: assets_sort_id; Type: INDEX; Schema: public; Owner: -; Tablespace: 
--

CREATE INDEX assets_sort_id ON assets USING btree (sort_id);


--
-- Name: auth_tokens_user_id_idx; Type: INDEX; Schema: public; Owner: -; Tablespace: 
--

CREATE INDEX auth_tokens_user_id_idx ON auth_tokens USING btree (user_id);


--
-- Name: issuance_activity_assets_asset_id_idx; Type: INDEX; Schema: public; Owner: -; Tablespace: 
--

CREATE INDEX issuance_activity_assets_asset_id_idx ON issuance_activity_assets USING btree (asset_id);


--
-- Name: issuance_activity_assets_issuance_activity_id_asset_id_idx; Type: INDEX; Schema: public; Owner: -; Tablespace: 
--

CREATE UNIQUE INDEX issuance_activity_assets_issuance_activity_id_asset_id_idx ON issuance_activity_assets USING btree (issuance_activity_id, asset_id);


--
-- Name: issuance_activity_issuer_node_id_idx; Type: INDEX; Schema: public; Owner: -; Tablespace: 
--

CREATE INDEX issuance_activity_issuer_node_id_idx ON issuance_activity USING btree (issuer_node_id);


--
-- Name: issuance_activity_issuer_node_id_txid_idx; Type: INDEX; Schema: public; Owner: -; Tablespace: 
--

CREATE UNIQUE INDEX issuance_activity_issuer_node_id_txid_idx ON issuance_activity USING btree (issuer_node_id, txid);


--
-- Name: issuer_nodes_project_id_idx; Type: INDEX; Schema: public; Owner: -; Tablespace: 
--

CREATE INDEX issuer_nodes_project_id_idx ON issuer_nodes USING btree (project_id);


--
-- Name: manager_nodes_project_id_idx; Type: INDEX; Schema: public; Owner: -; Tablespace: 
--

CREATE INDEX manager_nodes_project_id_idx ON manager_nodes USING btree (project_id);


--
-- Name: members_user_id_idx; Type: INDEX; Schema: public; Owner: -; Tablespace: 
--

CREATE INDEX members_user_id_idx ON members USING btree (user_id);


--
-- Name: users_lower_idx; Type: INDEX; Schema: public; Owner: -; Tablespace: 
--

CREATE UNIQUE INDEX users_lower_idx ON users USING btree (lower(email));


--
-- Name: utxos_account_id_asset_id_reserved_at_idx; Type: INDEX; Schema: public; Owner: -; Tablespace: 
--

CREATE INDEX utxos_account_id_asset_id_reserved_at_idx ON utxos USING btree (account_id, asset_id, reserved_until);


--
-- Name: utxos_manager_node_id_asset_id_reserved_at_idx; Type: INDEX; Schema: public; Owner: -; Tablespace: 
--

CREATE INDEX utxos_manager_node_id_asset_id_reserved_at_idx ON utxos USING btree (manager_node_id, asset_id, reserved_until);


--
-- Name: accounts_manager_node_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY accounts
    ADD CONSTRAINT accounts_manager_node_id_fkey FOREIGN KEY (manager_node_id) REFERENCES manager_nodes(id) ON DELETE NO ACTION;


--
-- Name: activity_accounts_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY activity_accounts
    ADD CONSTRAINT activity_accounts_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE NO ACTION;


--
-- Name: activity_accounts_activity_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY activity_accounts
    ADD CONSTRAINT activity_accounts_activity_id_fkey FOREIGN KEY (activity_id) REFERENCES activity(id);


--
-- Name: activity_manager_node_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY activity
    ADD CONSTRAINT activity_manager_node_id_fkey FOREIGN KEY (manager_node_id) REFERENCES manager_nodes(id) ON DELETE NO ACTION;


--
-- Name: addresses_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY addresses
    ADD CONSTRAINT addresses_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE NO ACTION;


--
-- Name: addresses_manager_node_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY addresses
    ADD CONSTRAINT addresses_manager_node_id_fkey FOREIGN KEY (manager_node_id) REFERENCES manager_nodes(id) ON DELETE NO ACTION;


--
-- Name: assets_issuer_node_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY assets
    ADD CONSTRAINT assets_issuer_node_id_fkey FOREIGN KEY (issuer_node_id) REFERENCES issuer_nodes(id) ON DELETE NO ACTION;


--
-- Name: auth_tokens_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY auth_tokens
    ADD CONSTRAINT auth_tokens_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id);


--
-- Name: invitations_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY invitations
    ADD CONSTRAINT invitations_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects(id);


--
-- Name: issuance_activity_assets_asset_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY issuance_activity_assets
    ADD CONSTRAINT issuance_activity_assets_asset_id_fkey FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE NO ACTION;


--
-- Name: issuance_activity_assets_issuance_activity_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY issuance_activity_assets
    ADD CONSTRAINT issuance_activity_assets_issuance_activity_id_fkey FOREIGN KEY (issuance_activity_id) REFERENCES issuance_activity(id);


--
-- Name: issuance_activity_issuer_node_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY issuance_activity
    ADD CONSTRAINT issuance_activity_issuer_node_id_fkey FOREIGN KEY (issuer_node_id) REFERENCES issuer_nodes(id) ON DELETE NO ACTION;


--
-- Name: manager_nodes_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY manager_nodes
    ADD CONSTRAINT manager_nodes_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects(id);


--
-- Name: members_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY members
    ADD CONSTRAINT members_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects(id);


--
-- Name: members_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY members
    ADD CONSTRAINT members_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id);


--
-- Name: pool_outputs_tx_hash_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY pool_outputs
    ADD CONSTRAINT pool_outputs_tx_hash_fkey FOREIGN KEY (tx_hash) REFERENCES pool_txs(tx_hash) ON DELETE CASCADE;


--
-- Name: rotations_manager_node_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY rotations
    ADD CONSTRAINT rotations_manager_node_id_fkey FOREIGN KEY (manager_node_id) REFERENCES manager_nodes(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--

