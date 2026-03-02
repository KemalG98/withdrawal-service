CREATE TABLE IF NOT EXISTS balances (
                                        user_id     UUID PRIMARY KEY,
                                        amount      NUMERIC(20, 8) NOT NULL DEFAULT 0 CHECK (amount >= 0),
    currency    VARCHAR(10)    NOT NULL DEFAULT 'USDT',
    updated_at  TIMESTAMPTZ    NOT NULL DEFAULT now()
    );

CREATE TABLE IF NOT EXISTS withdrawals (
                                           id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID         NOT NULL,
    amount           NUMERIC(20, 8) NOT NULL CHECK (amount > 0),
    currency         VARCHAR(10)  NOT NULL,
    destination      TEXT         NOT NULL,
    status           VARCHAR(20)  NOT NULL DEFAULT 'pending',
    idempotency_key  TEXT         NOT NULL,
    payload_hash     TEXT         NOT NULL,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CONSTRAINT withdrawals_idempotency_key_user_id_uq UNIQUE (user_id, idempotency_key)
    );

CREATE TABLE IF NOT EXISTS ledger_entries (
                                              id             UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID         NOT NULL,
    withdrawal_id  UUID         REFERENCES withdrawals(id),
    amount         NUMERIC(20, 8) NOT NULL,
    direction      VARCHAR(10)  NOT NULL, -- 'debit' | 'credit'
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now()
    );

-- Seed one test user with 1000 USDT
INSERT INTO balances (user_id, amount, currency)
VALUES ('00000000-0000-0000-0000-000000000001', 1000, 'USDT')
    ON CONFLICT DO NOTHING;