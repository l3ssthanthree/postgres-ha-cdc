INSERT INTO order_event_log (order_id, event_type, actor_type, payload, created_at)
SELECT
    (((series.n + $1) - 1) % $2) + 1,
    'load_test_event',
    'workload_service',
    jsonb_build_object(
        'batch_size', $3,
        'sequence', series.n,
        'seed_offset', $1
    ),
    NOW() - make_interval(secs => series.n % 30)
FROM generate_series(1, $3) AS series(n);
