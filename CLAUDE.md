# CLAUDE.md — Streamero

Streamero je self-hosted media server (alternativa Jellyfinu) s web klijentom i iOS appom (.ipa za Feather). Brand: SCYDE.
Plan: `docs/PLAN.md`. Feature checklist: `docs/FEATURES.md`. Stanje: `docs/PROGRESS.md`. Odluke: `docs/DECISIONS.md`.

## Neprekršiva pravila
- **Clean-room:** ne kopiraj, ne prevodi i ne otvaraj izvorni kod Jellyfina, Jellyfin-weba, Swiftfina ili Emby-ja. Dozvoljeno: javna dokumentacija, javna OpenAPI specifikacija, konvencije imenovanja medija.
- Nigdje u UI-ju, ikonama ili identifikatorima ne piše „Jellyfin".
- `api/openapi.yaml` je izvor istine. Promjena API-ja = prvo spec, zatim `scripts/gen-api.sh`, zatim implementacija. Nikad ručno ne uređuj generirani kod.
- Svaka promjena sheme = nova goose migracija (nikad ne mijenjaj postojeću koja je u `main`).
- Kod, komentari, commitovi, API na engleskom. Svi UI stringovi kroz i18n (HR + EN), nikad hardkodirani.
- Tajne nikad u repo. Samo `.env.example`.
- iOS: ne koristi APNs, UDP broadcast ni entitlemente koji ne prežive Feather resign (vidi PLAN.md §6). CarPlay i widgeti samo iza feature flaga.
- Ne briši korisničke medijske datoteke osim kroz eksplicitne opt-in značajke s košem (media cleaner).
- Sigurnost: svaki endpoint koji prima putanju/ID datoteke mora proći kroz `library.ResolveSafePath` (zaštita od path traversal). Stream URL-ovi su HMAC-potpisani.

## Komande
```bash
# dev stack na serveru
docker compose -f deploy/docker-compose.yml -f deploy/compose.dev.yml up -d --build
docker compose -f deploy/docker-compose.yml -f deploy/compose.dev.yml logs -f streamero

# server
cd server && go test ./... -race && golangci-lint run
cd server && sqlc generate && go run ./cmd/streamero migrate

# API codegen (server + web + iOS)
./scripts/gen-api.sh

# web
cd web && pnpm i && pnpm dev && pnpm typecheck && pnpm test && pnpm e2e

# demo: build trenutnog brancha + provjera na https://demo.streamero.net
./scripts/deploy-demo.sh

# test mediji
./scripts/gen-fixtures.sh ./data/fixtures
./scripts/seed-big-library.sh 50000

# iOS (samo u CI na macOS-u; lokalno na Linuxu samo uređuj project.yml i Swift kod)
cd ios && xcodegen generate
```

## Način rada
1. Na početku sesije pročitaj `docs/PROGRESS.md` i nastavi od zadnjeg otvorenog koraka.
2. Jedna faza = jedan branch `phase/PX-ime`. Mali, logični commitovi (Conventional Commits: `feat(scanner): …`).
3. Prije implementacije napiši kratak plan koraka; za veće odluke dodaj zapis u `docs/DECISIONS.md` (kontekst → odluka → posljedice).
4. Svaki feature dolazi s testovima: unit (Go/TS/Swift), integracijski protiv stvarnog Postgresa (testcontainers ili compose), e2e (Playwright) za korisničke tokove.
5. Feature se označava `[x]` u `FEATURES.md` tek kad radi end-to-end i testovi prolaze.
6. Svaka izmjena mora biti vidljiva na https://demo.streamero.net: nakon svakog commita pokreni `./scripts/deploy-demo.sh` (builda trenutni branch i provjerava da demo vraća taj commit u `/api/v1/system/info`). Ne javljaj da je nešto gotovo dok skripta ne prođe.
7. Na kraju sesije: ažuriraj `PROGRESS.md` (što je gotovo, što je sljedeće, poznati problemi).
8. Ako nešto iz plana nije izvedivo ili postoji bolji pristup — napiši zašto u `DECISIONS.md` i nastavi s boljim pristupom; ne tiho odstupaj.

## Performanse (ciljevi koji se testiraju)
- Home API < 200 ms, lista biblioteke (100 stavki) < 100 ms na 50k stavki.
- Re-scan bez promjena: < 30 s za 50k stavki.
- Start servera do `/readyz` < 5 s (bez migracija).
- Nijedan upit u hot pathu bez indeksa (`EXPLAIN` u testu za ključne upite).

## Dizajn
Premium, tamni, „cinematic" stil; brand SCYDE. Referenca: `docs/DESIGN.md` (dodaje korisnik). Do tada: neutralni tamni dizajn sustav na CSS varijablama (web) i `DesignSystem/` tokenima (iOS), da se tema kasnije zamijeni bez diranja komponenti.
