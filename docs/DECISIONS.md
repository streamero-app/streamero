# DECISIONS

Format: `## D-001 Naslov (datum)` → Kontekst → Odluka → Posljedice.

## D-001 Backend u Go-u, Postgres umjesto SQLite-a (2026-11-06)
**Kontekst:** Najveći prigovori na Jellyfin su performanse velikih (posebno glazbenih) biblioteka i zaključavanje SQLite baze.
**Odluka:** Go server + PostgreSQL 17 (pgvector, pg_trgm) + River job queue + Valkey.
**Posljedice:** Jedan dodatni kontejner (Postgres), ali skalabilnost, pravi indeksi, AI pretraga i bez DB lockova.

## D-002 OpenAPI kao izvor istine (2026-11-06)
**Odluka:** `api/openapi.yaml` generira Go server sučelja (ogen), TS klijent i Swift klijent.
**Posljedice:** Web i iOS nikad ne zaostaju za serverom; svaka promjena API-ja ide kroz spec.

## D-003 iOS: AVPlayer primarno, MPVKit fallback; bez APNs-a (2026-11-06)
**Kontekst:** App se sideloada kroz Feather; neki entitlementi ne prežive resign.
**Odluka:** AVPlayer + HLS za najbolji HDR/DV/PiP/AirPlay; MPVKit za direct play egzotičnih formata; notifikacije preko WebSocketa/ntfy umjesto APNs-a; discovery preko Bonjoura.
