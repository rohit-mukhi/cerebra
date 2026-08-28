package cerebra

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/google/uuid"
)

// UnavailabilityCache tracks models that are temporarily unavailable due to quota/rate limits
// Uses in-memory cache backed by DB for persistence
type UnavailabilityCache struct {
	db    *sql.DB
	cache sync.Map // map[string]unavailabilityEntry (key: runtimeID:model)
	mu    sync.RWMutex
}

// unavailabilityEntry represents a single unavailability record
type unavailabilityEntry struct {
	RuntimeID  uuid.UUID
	Model      string
	MarkedAt   time.Time
	ExpiresAt  time.Time
	TTLSeconds int
}

// NewUnavailabilityCache creates a new unavailability cache
func NewUnavailabilityCache(db *sql.DB) *UnavailabilityCache {
	cache := &UnavailabilityCache{
		db: db,
	}
	
	// Load existing unavailabilities from DB on startup
	cache.loadFromDB(context.Background())
	
	// Start background cleanup goroutine
	go cache.cleanupLoop()
	
	return cache
}

// IsAvailable checks if a model is currently available (not in unavailability cache)
func (uc *UnavailabilityCache) IsAvailable(ctx context.Context, runtimeID uuid.UUID, model string) bool {
	key := makeKey(runtimeID, model)
	
	if entry, exists := uc.cache.Load(key); exists {
		e := entry.(unavailabilityEntry)
		if time.Now().Before(e.ExpiresAt) {
			return false // Still unavailable
		}
		// Entry expired, remove it
		uc.cache.Delete(key)
		go uc.deleteFromDB(ctx, runtimeID, model)
	}
	
	return true
}

// MarkUnavailable marks a model as unavailable for the specified TTL
func (uc *UnavailabilityCache) MarkUnavailable(ctx context.Context, runtimeID uuid.UUID, model string, ttlSeconds int) error {
	if ttlSeconds <= 0 {
		ttlSeconds = 3600 // Default 1 hour
	}

	now := time.Now()
	entry := unavailabilityEntry{
		RuntimeID:  runtimeID,
		Model:      model,
		MarkedAt:   now,
		ExpiresAt:  now.Add(time.Duration(ttlSeconds) * time.Second),
		TTLSeconds: ttlSeconds,
	}

	key := makeKey(runtimeID, model)
	uc.cache.Store(key, entry)

	// Persist to DB
	return uc.upsertToDB(ctx, entry)
}

// MarkAvailable explicitly marks a model as available (removes from cache)
func (uc *UnavailabilityCache) MarkAvailable(ctx context.Context, runtimeID uuid.UUID, model string) error {
	key := makeKey(runtimeID, model)
	uc.cache.Delete(key)
	return uc.deleteFromDB(ctx, runtimeID, model)
}

// GetUnavailableModels returns all currently unavailable models for a runtime
func (uc *UnavailabilityCache) GetUnavailableModels(ctx context.Context, runtimeID uuid.UUID) []string {
	var models []string
	now := time.Now()

	uc.cache.Range(func(key, value interface{}) bool {
		entry := value.(unavailabilityEntry)
		if entry.RuntimeID == runtimeID && now.Before(entry.ExpiresAt) {
			models = append(models, entry.Model)
		}
		return true
	})

	return models
}

// loadFromDB loads existing unavailability records from the database
func (uc *UnavailabilityCache) loadFromDB(ctx context.Context) {
	query := `
		SELECT runtime_id, model, marked_at, ttl_seconds
		FROM cerebra_model_unavailability
		WHERE marked_at + (ttl_seconds || ' seconds')::interval > now()
	`

	rows, err := uc.db.QueryContext(ctx, query)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var entry unavailabilityEntry
		if err := rows.Scan(&entry.RuntimeID, &entry.Model, &entry.MarkedAt, &entry.TTLSeconds); err != nil {
			continue
		}
		entry.ExpiresAt = entry.MarkedAt.Add(time.Duration(entry.TTLSeconds) * time.Second)
		
		key := makeKey(entry.RuntimeID, entry.Model)
		uc.cache.Store(key, entry)
	}
}

// upsertToDB persists an unavailability entry to the database
func (uc *UnavailabilityCache) upsertToDB(ctx context.Context, entry unavailabilityEntry) error {
	query := `
		INSERT INTO cerebra_model_unavailability (runtime_id, model, marked_at, ttl_seconds)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (runtime_id, model)
		DO UPDATE SET marked_at = EXCLUDED.marked_at, ttl_seconds = EXCLUDED.ttl_seconds
	`
	_, err := uc.db.ExecContext(ctx, query, entry.RuntimeID, entry.Model, entry.MarkedAt, entry.TTLSeconds)
	return err
}

// deleteFromDB removes an unavailability entry from the database
func (uc *UnavailabilityCache) deleteFromDB(ctx context.Context, runtimeID uuid.UUID, model string) error {
	query := `DELETE FROM cerebra_model_unavailability WHERE runtime_id = $1 AND model = $2`
	_, err := uc.db.ExecContext(ctx, query, runtimeID, model)
	return err
}

// cleanupLoop periodically removes expired entries from the cache
func (uc *UnavailabilityCache) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		uc.cleanup(context.Background())
	}
}

// cleanup removes expired entries from both cache and DB
func (uc *UnavailabilityCache) cleanup(ctx context.Context) {
	now := time.Now()

	// Clean in-memory cache
	uc.cache.Range(func(key, value interface{}) bool {
		entry := value.(unavailabilityEntry)
		if now.After(entry.ExpiresAt) {
			uc.cache.Delete(key)
		}
		return true
	})

	// Clean database
	query := `
		DELETE FROM cerebra_model_unavailability
		WHERE marked_at + (ttl_seconds || ' seconds')::interval <= now()
	`
	uc.db.ExecContext(ctx, query)
}

// makeKey creates a cache key from runtime ID and model
func makeKey(runtimeID uuid.UUID, model string) string {
	return runtimeID.String() + ":" + model
}

// Stats returns statistics about the unavailability cache
type CacheStats struct {
	TotalEntries    int
	ActiveEntries   int
	ExpiredEntries  int
}

// GetStats returns current cache statistics
func (uc *UnavailabilityCache) GetStats() CacheStats {
	stats := CacheStats{}
	now := time.Now()

	uc.cache.Range(func(key, value interface{}) bool {
		stats.TotalEntries++
		entry := value.(unavailabilityEntry)
		if now.Before(entry.ExpiresAt) {
			stats.ActiveEntries++
		} else {
			stats.ExpiredEntries++
		}
		return true
	})

	return stats
}
