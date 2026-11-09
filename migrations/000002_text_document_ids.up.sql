-- Documents are named in the URL (/editor?doc=demo), so their IDs are short
-- text names rather than UUIDs. The check mirrors what the web app accepts.
ALTER TABLE operations DROP CONSTRAINT operations_doc_id_fkey;
ALTER TABLE snapshots DROP CONSTRAINT snapshots_doc_id_fkey;

ALTER TABLE documents ALTER COLUMN id TYPE TEXT USING id::text;
ALTER TABLE operations ALTER COLUMN doc_id TYPE TEXT USING doc_id::text;
ALTER TABLE snapshots ALTER COLUMN doc_id TYPE TEXT USING doc_id::text;

ALTER TABLE documents
    ADD CONSTRAINT documents_id_format CHECK (id ~ '^[a-zA-Z0-9_-]{1,40}$');

ALTER TABLE operations
    ADD CONSTRAINT operations_doc_id_fkey
    FOREIGN KEY (doc_id) REFERENCES documents (id) ON DELETE CASCADE;
ALTER TABLE snapshots
    ADD CONSTRAINT snapshots_doc_id_fkey
    FOREIGN KEY (doc_id) REFERENCES documents (id) ON DELETE CASCADE;
