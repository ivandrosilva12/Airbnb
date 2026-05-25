-- AirHost demo: historical completed stays + multi-criteria reviews.
--
-- The booking "Complete" rule forbids completing a stay before check-out, so a
-- realistic review history (which needs *completed* stays) cannot be created
-- through the API quickly. This script seeds it directly, then refreshes the
-- denormalised listing rating aggregates and the host's Superhost flag exactly
-- as the review application service would.
--
-- Idempotent guard: only runs when there are no seeded reviews yet.
-- Apply with:
--   docker exec -i airbnb-postgres-1 psql -U airhost -d airhost < scripts/seed_history.sql

DO $$
DECLARE
    v_host    uuid := (SELECT id FROM users WHERE email = 'host@airhost.dev');
    v_guest   uuid := (SELECT id FROM users WHERE email = 'guest@airhost.dev');
    super_pid uuid;  -- listing that earns the host Superhost (>=10 five-star stays)
    resp_pid  uuid;  -- listing with a review awaiting a host response
    v_bid     uuid;
    g         integer;
    v_rating  integer;
BEGIN
    IF v_host IS NULL OR v_guest IS NULL THEN
        RAISE NOTICE 'host/guest users not found — run scripts/seed.py first'; RETURN;
    END IF;
    IF EXISTS (SELECT 1 FROM reviews) THEN
        RAISE NOTICE 'reviews already present — skipping history seed'; RETURN;
    END IF;

    SELECT id INTO super_pid FROM properties WHERE host_id = v_host ORDER BY created_at LIMIT 1;
    SELECT id INTO resp_pid  FROM properties WHERE host_id = v_host ORDER BY created_at OFFSET 1 LIMIT 1;
    IF super_pid IS NULL THEN
        RAISE NOTICE 'no properties for host — run scripts/seed.py first'; RETURN;
    END IF;

    -- 11 completed five-star stays on the Superhost listing.
    FOR g IN 1..11 LOOP
        INSERT INTO bookings (id, property_id, guest_id, check_in, check_out, guests,
            total_cents, currency, status, created_at, updated_at,
            subtotal_cents, cleaning_fee_cents, service_fee_cents, discount_cents, tax_cents)
        VALUES (gen_random_uuid(), super_pid, v_guest,
            now() - ((g * 14 + 6) || ' days')::interval,
            now() - ((g * 14 + 3) || ' days')::interval,
            2, 30500, 'EUR', 'completed',
            now() - ((g * 14 + 10) || ' days')::interval,
            now() - ((g * 14 + 3) || ' days')::interval,
            28500, 2000, 0, 0, 0)
        RETURNING id INTO v_bid;

        INSERT INTO reviews (id, property_id, booking_id, guest_id, author_id, rating, comment, kind,
            created_at, rating_cleanliness, rating_accuracy, rating_communication, rating_location, rating_checkin, rating_value)
        VALUES (gen_random_uuid(), super_pid, v_bid, v_guest, v_guest, 5,
            'Outstanding stay — spotless, great location and a wonderful host.',
            'guest_to_property', now() - interval '2 days', 5, 5, 5, 5, 5, 5);
    END LOOP;

    -- 3 completed stays on a second listing, ratings 5/4/5, multi-criteria. None
    -- carry a host response yet, so any can be answered live.
    IF resp_pid IS NOT NULL THEN
        FOR g IN 1..3 LOOP
            v_rating := CASE WHEN g = 2 THEN 4 ELSE 5 END;
            INSERT INTO bookings (id, property_id, guest_id, check_in, check_out, guests,
                total_cents, currency, status, created_at, updated_at,
                subtotal_cents, cleaning_fee_cents, service_fee_cents, discount_cents, tax_cents)
            VALUES (gen_random_uuid(), resp_pid, v_guest,
                now() - ((g * 9 + 4) || ' days')::interval,
                now() - ((g * 9 + 1) || ' days')::interval,
                2, 24900, 'EUR', 'completed',
                now() - ((g * 9 + 8) || ' days')::interval,
                now() - ((g * 9 + 1) || ' days')::interval,
                23400, 1500, 0, 0, 0)
            RETURNING id INTO v_bid;

            INSERT INTO reviews (id, property_id, booking_id, guest_id, author_id, rating, comment, kind,
                created_at, rating_cleanliness, rating_accuracy, rating_communication, rating_location, rating_checkin, rating_value)
            VALUES (gen_random_uuid(), resp_pid, v_bid, v_guest, v_guest, v_rating,
                CASE WHEN g = 2 THEN 'Lovely flat, a little noisy at night but great value.'
                     ELSE 'Highly recommended — would book again!' END,
                'guest_to_property', now() - (g || ' days')::interval,
                5, v_rating, 5, 4, 5, v_rating);
        END LOOP;
    END IF;

    -- Refresh denormalised listing rating aggregates (as refreshPropertyRating does).
    UPDATE properties p SET average_rating = s.avg, review_count = s.cnt
    FROM (
        SELECT property_id, AVG(rating)::double precision AS avg, COUNT(*) AS cnt
        FROM reviews WHERE kind = 'guest_to_property' GROUP BY property_id
    ) s
    WHERE p.id = s.property_id;

    -- Recompute the host Superhost flag from their review-weighted rating across
    -- all listings (>= 10 reviews and >= 4.8 average), fanned across the listings.
    UPDATE properties p SET host_is_superhost = a.qualifies
    FROM (
        SELECT host_id AS hid,
               (SUM(average_rating * review_count) / NULLIF(SUM(review_count), 0) >= 4.8
                AND SUM(review_count) >= 10) AS qualifies
        FROM properties GROUP BY host_id
    ) a
    WHERE a.hid = p.host_id;

    RAISE NOTICE 'history seed complete: superhost listing=% response listing=%', super_pid, resp_pid;
END $$;
