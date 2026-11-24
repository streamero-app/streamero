# Biblioteka za odrasle

Streamero može prikazivati scene i filmove za odrasle, npr. one koje preuzima Locarr. Ta je mogućnost
zadano isključena i skrivena od svih računa, i od administratora, dok je izričito ne uključiš.
Tehnička pozadina je u `docs/DECISIONS.md` (D-053).

## Što je zaštićeno

- Sadržaj biblioteke za odrasle vide samo računi kojima administrator da **pristup sadržaju za odrasle**.
  Taj pristup nije dio ostalih dozvola: ne daje ga „pristup svim bibliotekama", pozivnica, prijava preko
  SSO-a ni administratorska uloga.
- Za sve ostale (i za dječje profile na računu koji ima pristup) taj sadržaj ne postoji: nema ga na
  početnoj, u pretrazi, „Nastavi gledati", kolekcijama, preporukama, statistici i `/api/stats`, aktivnosti,
  obavijestima i webhookovima, na televizorima preko DLNA-a, u aplikacijama trećih strana (compat API),
  u zajedničkom gledanju, preuzimanjima ni u Otkrij.
- API ključevi (nadzorne ploče, Locarr) nikad ne vide sadržaj, čak ni ključ administratora koji ima pristup.
  Locarr smije samo pokrenuti skeniranje biblioteke.
- Streamero nema javnih poveznica ni dijeljenja, pa sadržaj za odrasle ne može izaći van na taj način.
- Datoteke se nikad ne mijenjaju ni brišu.

## Postavljanje

1. **Mapa.** Mapa sa sadržajem za odrasle mora biti vidljiva Streameru, unutar mape s medijima
   (`STREAMERO_MEDIA_ROOTS`, zadano `/media`). Ako je na serveru `MEDIA_PATH=/mnt/jellyfin/data`, Locarrova
   mapa `/mnt/jellyfin/data/adult` je u Streameru `/media/adult`.
2. **Biblioteka.** Postavke → Biblioteke → Dodaj biblioteku, vrsta **Za odrasle**, mapa `/media/adult`.
   Streamero traži potvrdu. Biblioteka za odrasle smije biti unutar mape druge biblioteke (ta je onda
   preskače i odmah uklanja ono što je prije našla u njoj), ali ne smije sadržavati mapu druge biblioteke.
   Vrsta se kasnije ne može promijeniti.
3. **Pristup.** Postavke → Korisnici → kod računa **Daj pristup za odrasle**, uz potvrdu. Daj ga i
   sebi ako želiš gledati. Daj ga samo računima odraslih osoba. Pristup se uklanja istim gumbom i
   vrijedi odmah.
4. **Metapodaci (neobavezno).** Postavke → Metapodaci → **Metapodaci za odrasle**: upiši ThePornDB API token
   (theporndb.net, profil → API) i/ili StashDB API ključ (stashdb.org, profil korisnika). Ključ se provjeri,
   spremi šifrirano (ključ za šifriranje je `/config/secret.key` ili `STREAMERO_SECRET_KEY`) i više se ne
   prikazuje. Bez ključeva podaci dolaze iz naziva datoteka, a slika je kadar iz videa.
5. **Locarr.** U Locarru, u postavkama sadržaja za odrasle, uključi **Javi Streameru za uvoze** (Locarr mora
   već biti povezan sa Streamerom u postavkama medijskog servera). Locarr koristi isti administratorski API ključ i iste mapiranja putanja kao za
   filmove; Streamero nakon uvoza skenira biblioteku za odrasle. Ako Locarr javi da ni jedna biblioteka ne
   sadrži putanju, provjeri mapiranje putanja (npr. `/mnt/jellyfin/data` → `/media`).

## Nazivi datoteka

Streamero čita Locarrove zadane nazive i slične:

- `Studio/Studio - 2024-01-15 - Naslov scene [Izvođač Jedan, Izvođač Dva] WEBDL-1080p.mp4`
- `Studio/Studio 2024-01-15 Naslov scene Izvođač Jedan, Izvođač Dva.mp4`
- `Studio.24.01.15.Naslov.Scene.1080p.mp4` (nazivi izdanja)
- `Naslov filma (2019)/Naslov filma (2019) WEBDL-1080p.mkv` (filmovi)

Ako izvođači nisu u uglatim zagradama ni iza crtice, Streamero ih pogađa iz zareza; ThePornDB/StashDB to
ispravi kad pronađe scenu.

## Gledanje

U izborniku računa i gornjem izborniku pojavljuje se **Za odrasle** (samo računima s pristupom):

- **Scene**: pretraga po naslovu, studiju i izvođaču, filtri (scene/filmovi, gledano, poredak, oznaka),
  „Nastavi gledati" samo za ovaj dio.
- **Studiji** i **Izvođači** s brojem scena i stranicom sa svim scenama.
- Slike su **zamućene dok ih ne klikneš**. Zamućivanje se isključuje u postavkama odjeljka (ikona klizača).
- **PIN**: u postavkama odjeljka možeš postaviti PIN (4–12 znamenki). Tada ga svaka nova prijava traži prije
  otvaranja odjeljka; otključano vrijedi do 4 sata, do odjave ili dok ne klikneš lokot. Pogrešni PIN-ovi
  računaju se kao neuspjele prijave.
- Tab preglednika uvijek se zove samo „Streamero".

Na Androidu i iPhoneu odjeljak je u kartici Biblioteke. Android ne dopušta snimke zaslona tih ekrana, a
iPhone skriva sadržaj u prikazu aplikacija i ne prikazuje ga na zaključanom zaslonu.

## Ograničenja

- Biblioteka za odrasle nema titlove, poglavlja, preuzimanja ni zajedničko gledanje.
- Administratori vide biblioteku za odrasle (naziv i mapu) u postavkama biblioteka, ali ne i njen sadržaj
  dok sami sebi ne daju pristup.
- Restart servera zaključava odjeljak u sesijama koje koriste PIN.
