-- Fails if any document has a name that is not a UUID, on purpose: those
-- documents cannot be represented in the old schema.
ALTER TABLE operations DROP CONSTRAINT operations_doc_id_fkey;
ALTER TABLE snapshots DROP CONSTRAINT snapshots_doc_id_fkey;
ALTER TABLE documents DROP CONSTRAINT documents_id_format;

ALTER TABLE documents ALTER COLUMN id TYPE UUID USING id::uuid;
ALTER TABLE operations ALTER COLUMN doc_id TYPE UUID USING doc_id::uuid;
ALTER TABLE snapshots ALTER COLUMN doc_id TYPE UUID USING doc_id::uuid;

ALTER TABLE operations
    ADD CONSTRAINT operations_doc_id_fkey
    FOREIGN KEY (doc_id) REFERENCES documents (id) ON DELETE CASCADE;
ALTER TABLE snapshots
    ADD CONSTRAINT snapshots_doc_id_fkey
    FOREIGN KEY (doc_id) REFERENCES documents (id) ON DELETE CASCADE;
