# Korisnici i pristup bibliotekama

Sve se radi u **Postavke → Korisnici** (samo administrator).

## Uređivanje korisnika
Kod korisnika klikni **Uredi** da otvoriš uređivač. U njemu su:
- **korisničko ime** i **prikazano ime** (ime koje se prikazuje uz korisničko; prazno ga briše);
- **Administrator** i **Omogućen** (vlastiti račun ne možeš onemogućiti; zadnji administrator se ne može ukloniti ni onemogućiti);
- **Pristup bibliotekama** (vidi dolje);
- **Dozvole** (preuzimanje, transkodiranje, Live TV, zajedničko gledanje, kolekcije, titlovi…);
- **Spremi promjene** sprema sve gore odjednom;
- radnje: **Postavi lozinku**, **Poništi 2FA** (kad je uključen), **Odjavi sa svih uređaja**, **Daj pristup za odrasle** i **Obriši**.

Administrator uvijek vidi sve biblioteke, pa umjesto izbora piše „Administratori vide sve biblioteke."

## Pristup bibliotekama
- **Sve biblioteke** — zadano za nove i postojeće račune. Račun vidi i biblioteke koje dodaš kasnije.
- **Samo odabrane biblioteke** — označi biblioteke koje račun smije vidjeti. Ostale za njega ne postoje: nema ih na početnoj, u pretrazi, „Nastavi gledati", „Sljedeće", kolekcijama i popisima, preuzimanjima, zajedničkom gledanju, na televizoru (DLNA) ni u aplikacijama trećih strana (compat API). Izravna poveznica na skrivenu stavku vraća „nije pronađeno".
  - **Daj i biblioteke stvorene kasnije** — uključi ako račun treba automatski dobiti svaku novu biblioteku. Zadano je isključeno, jer je ograničen račun obično namjerno ograničen (npr. za dijete).
- Promjena vrijedi odmah, i za već otvorene sesije. Preuzeto za offline gledanje iz skrivene biblioteke aplikacija uklanja pri sljedećoj sinkronizaciji.
- **Profili** (i dječji) na računu vide točno biblioteke računa, nikad više.

## Biblioteka za odrasle
Biblioteka vrste **Za odrasle** nikad nije dio ovog izbora i ne vidi je ni račun sa „Sve biblioteke". Za nju postoji zasebna dozvola **Pristup za odrasle** (vidi [sadržaj za odrasle](sadrzaj-za-odrasle.md)). Dok takva biblioteka ne postoji, gumb **Daj pristup za odrasle** je onemogućen uz napomenu „Najprije stvori biblioteku za odrasle (Postavke → Biblioteke)." Isto tako kartica **Metapodaci za odrasle (ThePornDB / StashDB)** u Postavke → Metapodaci do tada samo pokazuje gdje početi.

## Novi korisnik i pozivnice
Pri dodavanju korisnika (**Dodaj korisnika**) i pri izradi pozivnice odmah biraš „Sve biblioteke" ili odabrane. Račun napravljen iz pozivnice dobiva taj izbor.

## Obavijesti
U **Postavke → Obavijesti** odredište (webhook, ntfy, Gotify, Discord) pod **Biblioteke** može slati obavijesti o novim stavkama i reprodukciji samo za odabrane biblioteke — npr. kanal koji prati dijete dobiva samo obavijesti iz dječje biblioteke.
