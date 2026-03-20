CREATE TABLE IF NOT EXISTS USER (
    id text PRIMARY KEY,
    first_name text NOT NULL,
    last_name text NOT NULL,
    json_data text NOT NULL
);

CREATE TABLE IF NOT EXISTS address(
    id text PRIMARY KEY,
    user_id text NOT NULL,
    street text NOT NULL,
    city text NOT NULL,
    state text NOT NULL,
    zip_code text NOT NULL
);

