-- Create cerebra_model_unavailability table for tracking quota-exhausted models
-- Cerebra uses this to avoid repeatedly routing to unavailable models

CREATE TABLE cerebra_model_unavailability (
  runtime_id  UUID        NOT NULL,
  model       TEXT        NOT NULL,
  marked_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  ttl_seconds INT         NOT NULL DEFAULT 3600,
  PRIMARY KEY (runtime_id, model)
);

COMMENT ON TABLE cerebra_model_unavailability IS 'Tracks models temporarily unavailable due to quota/rate limits';
COMMENT ON COLUMN cerebra_model_unavailability.marked_at IS 'When the model was marked unavailable';
COMMENT ON COLUMN cerebra_model_unavailability.ttl_seconds IS 'How long to consider the model unavailable (default 1 hour)';
