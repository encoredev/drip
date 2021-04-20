CREATE TABLE "user" (
    email TEXT NOT NULL PRIMARY KEY,
    optin BOOLEAN NOT NULL,
    optin_changed TIMESTAMP WITH TIME ZONE NOT NULL
);

CREATE TABLE "email" (
    id BIGSERIAL PRIMARY KEY,
    mailgun_id TEXT NULL,
    email TEXT NOT NULL REFERENCES "user" (email),
    template_id TEXT NOT NULL,
    created TIMESTAMP WITH TIME ZONE NOT NULL,
    sent TIMESTAMP WITH TIME ZONE NULL
);

CREATE TABLE "user_series" (
    email TEXT NOT NULL REFERENCES "user" (email),
    series_id TEXT NOT NULL,
    next_step_id TEXT NULL,
    next_step_ts TIMESTAMP WITH TIME ZONE NULL,
    started TIMESTAMP WITH TIME ZONE NOT NULL,
    PRIMARY KEY (email, series_id)
);

CREATE INDEX idx_user_series_next_step_ts ON user_series
USING BRIN (next_step_ts) WITH (pages_per_range = 128)
WHERE next_step_ts IS NOT NULL;

CREATE TABLE "user_series_step" (
    email TEXT NOT NULL REFERENCES "user" (email),
    series_id TEXT NOT NULL,
    step_id TEXT NOT NULL,
    email_id BIGINT NOT NULL REFERENCES "email" (id),

    executed TIMESTAMP WITH TIME ZONE NOT NULL,
    PRIMARY KEY (email, series_id, step_id)
);

CREATE TABLE "unsubscribe_event" (
    email TEXT NOT NULL REFERENCES "user" (email),
    email_id BIGINT NOT NULL REFERENCES "email" (id),
    event_time TIMESTAMP WITH TIME ZONE NOT NULL
);