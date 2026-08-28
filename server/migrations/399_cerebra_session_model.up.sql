-- Add session_model to issue and chat_session for Cerebra session affinity
-- Tracks which model was chosen for the first turn of a conversation

ALTER TABLE issue
  ADD COLUMN IF NOT EXISTS session_model TEXT;

ALTER TABLE chat_session
  ADD COLUMN IF NOT EXISTS session_model TEXT;

COMMENT ON COLUMN issue.session_model IS 'Model chosen for first turn, maintained by Cerebra session affinity';
COMMENT ON COLUMN chat_session.session_model IS 'Model chosen for first turn, maintained by Cerebra session affinity';
