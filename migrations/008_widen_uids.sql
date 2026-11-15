-- 业务 UID 带可读前缀（art_/job_/cli_），统一放宽到 32 位
ALTER TABLE api_clients MODIFY uid CHAR(32) NOT NULL;
ALTER TABLE articles MODIFY uid CHAR(32) NOT NULL;
ALTER TABLE publish_jobs MODIFY uid CHAR(32) NOT NULL;
