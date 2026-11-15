-- 011: platform is chosen by the publisher and recorded on receipt.
ALTER TABLE articles MODIFY platform VARCHAR(50) NOT NULL DEFAULT '';
ALTER TABLE publish_jobs MODIFY platform VARCHAR(50) NOT NULL DEFAULT '';
