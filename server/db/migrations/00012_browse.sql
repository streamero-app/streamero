-- +goose Up
ALTER TABLE user_item_data ADD COLUMN favorite boolean NOT NULL DEFAULT false;
CREATE INDEX user_item_data_favorite_idx ON user_item_data (owner_id, last_played_at DESC) WHERE favorite;

-- Typo-tolerant title search and people search (pg_trgm, enabled in 00001).
CREATE INDEX items_title_trgm_idx ON items USING gin ((lower(COALESCE(metadata_title, title))) gin_trgm_ops);
CREATE INDEX item_people_name_trgm_idx ON item_people USING gin (lower(name) gin_trgm_ops);
CREATE INDEX items_genres_idx ON items USING gin (genres);

-- +goose Down
DROP INDEX items_genres_idx;
DROP INDEX item_people_name_trgm_idx;
DROP INDEX items_title_trgm_idx;
DROP INDEX user_item_data_favorite_idx;
ALTER TABLE user_item_data DROP COLUMN favorite;
