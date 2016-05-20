ALTER TABLE account_utxos ADD COLUMN spent_in_pool boolean DEFAULT false NOT NULL;

UPDATE account_utxos a SET spent_in_pool=true
FROM pool_inputs p
WHERE (a.tx_hash, a.index) = (p.tx_hash, p.index);

DROP FUNCTION reserve_utxos(text, text, text, bigint, bigint, timestamp with time zone, text);

CREATE FUNCTION reserve_utxos(inp_asset_id text, inp_account_id text, inp_tx_hash text, inp_out_index bigint, inp_amt bigint, inp_expiry timestamp with time zone, inp_idempotency_key text) RETURNS record
    LANGUAGE plpgsql
    AS $$
DECLARE
    res RECORD;
    row RECORD;
    ret RECORD;
    available BIGINT := 0;
    unavailable BIGINT := 0;
BEGIN
    SELECT * FROM create_reservation(inp_asset_id, inp_account_id, inp_expiry, inp_idempotency_key) INTO STRICT res;
    IF res.already_existed THEN
      SELECT res.reservation_id, res.already_existed, res.existing_change, CAST(0 AS BIGINT) AS amount, FALSE AS insufficient INTO ret;
      RETURN ret;
    END IF;

    LOOP
        SELECT tx_hash, index, amount INTO row
            FROM account_utxos u
            WHERE asset_id = inp_asset_id
                  AND inp_account_id = account_id
                  AND (inp_tx_hash IS NULL OR inp_tx_hash = tx_hash)
                  AND (inp_out_index IS NULL OR inp_out_index = index)
                  AND reservation_id IS NULL
                  AND NOT spent_in_pool
            LIMIT 1
            FOR UPDATE
            SKIP LOCKED;
        IF FOUND THEN
            UPDATE account_utxos SET reservation_id = res.reservation_id
                WHERE (tx_hash, index) = (row.tx_hash, row.index);
            available := available + row.amount;
            IF available >= inp_amt THEN
                EXIT;
            END IF;
        ELSE
            EXIT;
        END IF;
    END LOOP;

    IF available < inp_amt THEN
        SELECT SUM(change) AS change INTO STRICT row
            FROM reservations
            WHERE asset_id = inp_asset_id AND account_id = inp_account_id;
        unavailable := row.change;
        PERFORM cancel_reservation(res.reservation_id);
        res.reservation_id := 0;
    ELSE
        UPDATE reservations SET change = available - inp_amt
            WHERE reservation_id = res.reservation_id;
    END IF;

    SELECT res.reservation_id, res.already_existed, CAST(0 AS BIGINT) AS existing_change, available AS amount, (available+unavailable < inp_amt) AS insufficient INTO ret;
    RETURN ret;
END;
$$;
