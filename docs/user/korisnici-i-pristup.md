# Korisnici i pristup bibliotekama

Sve se radi u **Postavke → Korisnici** (samo administrator).

## Uređivanje korisnika
Klikni korisnika da otvoriš uređivač. U njemu su:
- **korisničko ime** i **prikazno ime** (ime koje se prikazuje umjesto korisničkog);
- **administrator** (uključi/isključi) i **omogućen/onemogućen** račun; zadnji administrator se ne može ukloniti ni onemogućiti;
- **pristup bibliotekama** (vidi dolje);
- **dozvole** (preuzimanje, transkodiranje, Live TV, zajedničko gledanje, kolekcije, titlovi…);
- **nova lozinka**, **poništi 2FA** (kad je uključen), **odjavi sa svih uređaja** i **obriši račun**.

Administrator uvijek vidi sve biblioteke, pa umjesto izbora piše „Administratori vide sve biblioteke".

## Pristup bibliotekama
- **Sve biblioteke** — zadano za nove i postojeće račune. Račun vidi i biblioteke koje dodaš kasnije.
- **Odabrane biblioteke** — označi biblioteke koje račun smije vidjeti. Ostale za njega ne postoje: nema ih na početnoj, u pretrazi, „Nastavi gledati", „Sljedeće", kolekcijama i popisima, preuzimanjima, zajedničkom gledanju, na televizoru (DLNA) ni u aplikacijama trećih strana (compat API). Izravna poveznica na skrivenu stavku vraća „nije pronađeno".
  - **Dodaj i biblioteke napravljene kasnije** — uključi ako račun treba automatski dobiti svaku novu biblioteku. Zadano je isključeno, jer je ograničen račun obično namjerno ograničen (npr. za dijete).
- Promjena vrijedi odmah, i za već otvorene sesije. Preuzeto za offline gledanje iz skrivene biblioteke aplikacija uklanja pri sljedećoj sinkronizaciji.
- **Profili** (i dječji) na računu vide točno biblioteke računa, nikad više.

## Biblioteka za odrasle
Biblioteka vrste **Odrasli** nikad nije dio ovog izbora i ne vidi je ni račun sa „Sve biblioteke". Za nju postoji zasebna dozvola **Pristup za odrasle** (vidi [sadržaj za odrasle](sadrzaj-za-odrasle.md)). Dok takva biblioteka ne postoji, gumb je onemogućen uz napomenu „Najprije napravi biblioteku za odrasle".

## Novi korisnik i pozivnice
Pri stvaranju korisnika i pri izradi pozivnice odmah biraš „Sve biblioteke" ili odabrane. Račun napravljen iz pozivnice dobiva taj izbor.

## Obavijesti
U **Postavke → Obavijesti** odredište (webhook, ntfy, Gotify, Discord) može slati obavijesti o novim stavkama i reprodukciji samo za odabrane biblioteke — npr. kanal koji prati dijete dobiva samo obavijesti iz dječje biblioteke.
