-- =============================================================
-- Seed: 10 test clients
-- All clients use password: TestPassword1!
-- is_verified = TRUE  →  they can log in immediately, no email needed
--
-- HOW TO RUN:
--   psql $DATABASE_URL -v coach_email="'your-coach@email.com'" -f seeds/test_clients.sql
--
-- Or interactively — replace the placeholder below and run:
--   psql $DATABASE_URL -f seeds/test_clients.sql
-- =============================================================

BEGIN;

DO $$
DECLARE
    v_coach_id          UUID;
    v_user_id           UUID;
    v_client_id         UUID;
    v_pw_hash           TEXT;

    v_coach_email       CITEXT := 'ptbydanielg@gmail.com';

    -- All test clients share this password
    v_password          TEXT   := 'TestPassword1!';

    -- Test client data: (email, full_name, phone_e164, sessions_per_month, timezone)
    -- 9 clients (Daniel G is already client #1 under this coach)
    type_clients        TEXT[][] := ARRAY[
        ARRAY['alice.walker@test.com',   'Alice Walker',   '+447700900001', '4',  'Europe/London'],
        ARRAY['ben.harris@test.com',     'Ben Harris',     '+447700900002', '8',  'Europe/London'],
        ARRAY['chloe.james@test.com',    'Chloe James',    '+447700900003', '4',  'Europe/London'],
        ARRAY['david.chen@test.com',     'David Chen',     '+447700900004', '12', 'Europe/London'],
        ARRAY['emma.taylor@test.com',    'Emma Taylor',    '+447700900005', '8',  'Europe/London'],
        ARRAY['finn.murphy@test.com',    'Finn Murphy',    '+447700900006', '4',  'Europe/London'],
        ARRAY['grace.liu@test.com',      'Grace Liu',      '+447700900007', '16', 'Europe/London'],
        ARRAY['harry.patel@test.com',    'Harry Patel',    '+447700900008', '8',  'Europe/London'],
        ARRAY['isla.brooks@test.com',    'Isla Brooks',    '+447700900009', '4',  'Europe/London']
    ];

    -- Preferred windows per client index (day_of_week 0=Mon..6=Sun, start_time, end_time)
    -- Each client gets 2 windows: a primary and a secondary day
    type_windows        TEXT[][] := ARRAY[
        -- alice: Mon+Wed mornings
        ARRAY['0', '07:00', '09:00',  '2', '07:00', '09:00'],
        -- ben: Tue+Thu evenings
        ARRAY['1', '18:00', '20:00',  '3', '18:00', '20:00'],
        -- chloe: Mon+Fri afternoons
        ARRAY['0', '12:00', '14:00',  '4', '12:00', '14:00'],
        -- david: Wed+Sat mornings
        ARRAY['2', '08:00', '10:00',  '5', '09:00', '11:00'],
        -- emma: Tue+Thu mornings
        ARRAY['1', '06:30', '08:30',  '3', '06:30', '08:30'],
        -- finn: Mon+Wed evenings
        ARRAY['0', '19:00', '21:00',  '2', '19:00', '21:00'],
        -- grace: Tue+Fri afternoons
        ARRAY['1', '13:00', '15:00',  '4', '13:00', '15:00'],
        -- harry: Wed+Sat afternoons
        ARRAY['2', '14:00', '16:00',  '5', '14:00', '16:00'],
        -- isla: Mon+Thu mornings
        ARRAY['0', '08:00', '10:00',  '3', '08:00', '10:00']
    ];

    i INT;
BEGIN
    -- Resolve the coach row
    SELECT c.id INTO v_coach_id
    FROM coaches c
    JOIN users u ON u.id = c.user_id
    WHERE u.email = v_coach_email
      AND u.deleted_at IS NULL
      AND c.deleted_at IS NULL;

    IF v_coach_id IS NULL THEN
        RAISE EXCEPTION 'Coach not found for email: %. Register as a coach first.', v_coach_email;
    END IF;

    -- Compute bcrypt hash once — reused for all clients
    v_pw_hash := crypt(v_password, gen_salt('bf', 10));

    FOR i IN 1..9 LOOP
        -- Skip if email already exists (safe to re-run)
        IF EXISTS (SELECT 1 FROM users WHERE email = type_clients[i][1]::CITEXT AND deleted_at IS NULL) THEN
            RAISE NOTICE 'Skipping % — already exists', type_clients[i][2];
            CONTINUE;
        END IF;

        -- Create user
        INSERT INTO users (email, password_hash, role, full_name, phone_e164, timezone, is_verified)
        VALUES (
            type_clients[i][1]::CITEXT,
            v_pw_hash,
            'client',
            type_clients[i][2],
            type_clients[i][3],
            type_clients[i][5],
            TRUE               -- skip email verification for test data
        )
        RETURNING id INTO v_user_id;

        -- Create client profile
        INSERT INTO clients (user_id, coach_id, sessions_per_month, priority_score)
        VALUES (v_user_id, v_coach_id, type_clients[i][4]::INT, 0)
        RETURNING id INTO v_client_id;

        -- Availability intake conversation (required for WhatsApp/SMS flow)
        INSERT INTO availability_intake_conversations (client_id, state, context)
        VALUES (v_client_id, 'idle', '{}');

        -- Two preferred windows per client
        INSERT INTO client_preferred_windows (client_id, day_of_week, start_time, end_time, source)
        VALUES
            (v_client_id, type_windows[i][1]::SMALLINT, type_windows[i][2]::TIME, type_windows[i][3]::TIME, 'manual'),
            (v_client_id, type_windows[i][4]::SMALLINT, type_windows[i][5]::TIME, type_windows[i][6]::TIME, 'manual');

        RAISE NOTICE 'Created client: % (user_id=%, client_id=%)', type_clients[i][2], v_user_id, v_client_id;
    END LOOP;
END $$;

COMMIT;
