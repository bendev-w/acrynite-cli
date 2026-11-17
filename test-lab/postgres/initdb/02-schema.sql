-- Runs once, on first start, after 01-roles.sh. Creates the application database,
-- the changing `events` table and the static `customers` table.

CREATE DATABASE lab_app OWNER lab_writer;

\connect lab_app

-- events: one row every few seconds from the writer service. created_at uses
-- clock_timestamp() (the real insert time), NOT now() (the transaction start), so a
-- restore to a chosen minute can be checked against each row's true time. seq is taken
-- from the table, so it has no gaps even across writer restarts.
CREATE TABLE events (
    id         bigserial PRIMARY KEY,
    seq        bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    note       text
);

-- customers: 100 seeded rows that never change. This is the table to "accidentally" drop.
CREATE TABLE customers (
    id    bigint PRIMARY KEY,
    name  text NOT NULL,
    email text NOT NULL
);

INSERT INTO customers (id, name, email)
SELECT g, 'Customer ' || g, 'customer' || g || '@example.test'
FROM generate_series(1, 100) AS g;

-- Hand both tables (and the serial sequence) to lab_writer so the writer can insert.
ALTER TABLE events    OWNER TO lab_writer;
ALTER TABLE customers OWNER TO lab_writer;
ALTER SEQUENCE events_id_seq OWNER TO lab_writer;
