INSERT IGNORE INTO settings (setting_key, setting_value) VALUES
('publish.tick_limit', '20'),
('publish.jitter_minutes', '10'),
('publish.timezone', '"Asia/Shanghai"'),
('publish.max_attempts', '3'),
('publish.lease_seconds', '1800'),
('publish.retry_base_seconds', '60'),
('media.max_mb', '10'),
('media.fetch_max_mb', '15'),
('api.rate_per_min', '120'),
('api.articles_per_hour', '20'),
('api.idempotency_hours', '24');
