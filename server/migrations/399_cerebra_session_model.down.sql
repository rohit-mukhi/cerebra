-- Rollback: Remove session_model columns

ALTER TABLE issue
  DROP COLUMN IF EXISTS session_model;

ALTER TABLE chat_session
  DROP COLUMN IF EXISTS session_model;
