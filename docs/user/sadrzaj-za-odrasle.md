# Biblioteka za odrasle

Streamero može prikazivati scene, filmove i galerije slika za odrasle, npr. one koje preuzima Locarr. Ta je mogućnost
zadano isključena i skrivena od svih računa, i od administratora, dok je izričito ne uključiš.
Tehnička pozadina je u `docs/DECISIONS.md` (D-053, galerije i navigacija D-058).

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

## Galerije slika

Galerija je **mapa sa slikama u kojoj nema videa**, ili zip/cbz arhiva slika. Slike pokraj videa
(npr. naslovnica u mapi filma) nisu galerija. Formati: JPG, PNG, WebP, GIF, AVIF i HEIC/HEIF. Mapa galerije
zove se kao scena i stoji pokraj scena, u mapi studija:

```
adult/
  Studio/
    Studio - 2024-01-15 - Naslov scene [Izvođač Jedan] WEBDL-1080p.mp4
    Studio - 2024-01-15 - Naslov scene [Izvođač Jedan]/      ← galerija te scene
      01.jpg
      02.jpg
    Studio - 2024-02-01 - Drugi set [Izvođač Dva].zip        ← galerija u arhivi
  Odmor na moru/                                             ← obična mapa slika, naslov je ime mape
    a.jpg
```

- Studio, datum, naslov i izvođači čitaju se iz imena mape kao kod scena; obična mapa daje samo naslov.
- Galerija s istim studijem i datumom kao scena (a ako je tog dana više scena, i istim naslovom) veže se
  uz tu scenu: na stranici scene vidiš njezine galerije, a galerija preuzima oznake scene.
- Arhive se nikad ne raspakiravaju na disk; Streamero ih čita u memoriji s ograničenjima veličine.
- Sličice napravi server pri prvom pregledu i čuva ih u svojoj mapi za odrasle (`/cache/adult-images`),
  ne u javnoj predmemoriji. Preglednik ih nikad ne sprema (`no-store`).

## Gledanje

U izborniku računa i gornjem izborniku pojavljuje se **Za odrasle** (samo računima s pristupom). Odjeljak
ima vlastitu bočnu traku (na mobitelu traku na vrhu) i polje za pretragu koje traži samo unutar odjeljka:

- **Početna**: nastavi gledati (samo napredak iz ovog odjeljka), nedavno dodano, novo od omiljenih
  izvođača, nove galerije, nedavno objavljeno.
- **Scene** i **Filmovi**: pretraga, filtar gledano, poredak (datum objave, najstarije, dodano, naslov,
  najdulje, nedavno gledano).
- **Fotografije**: sve galerije, s pretragom i poretkom (datum, najstarije, dodano, naslov, najviše slika).
- **Izvođači** i **Studiji** s brojem scena i galerija; stranica izvođača ima podatke (spol, rođenje,
  država, druga imena, opis) i kartice **Scene i filmovi** i **Fotografije**. Srce dodaje izvođača u
  omiljene; na popisu izvođača gumb **Omiljeni** prikazuje samo njih.
- **Oznake** dolaze iz ThePornDB-a i StashDB-a. Klik na oznaku prikazuje sve scene i galerije s njom.
  Ikona oka pokraj oznake je skriva samo za tebe; u postavkama odjeljka možeš uključiti **Sakrij i ono što
  ima skrivenu oznaku**, pa nestanu i takve scene i galerije.
- Iznad naslova je putokaz (Za odrasle › Izvođači › Ime); tipka Natrag preglednika vraća korak natrag.
- **Galerija** se otvara gumbom **Pregledaj** ili klikom na sliku: strelice ili ← →, Esc zatvara, + − 0 i
  kotačić ili dvoklik za povećanje, razmaknica pokreće prezentaciju, na mobitelu prelazak prstom i prema
  dolje za zatvaranje. Gumb za preuzimanje izvornika vidi se samo računu s dozvolom za preuzimanje.
- Slike su **zamućene dok ih ne klikneš**. Zamućivanje se isključuje u postavkama odjeljka (ikona klizača).
- **PIN**: u postavkama odjeljka možeš postaviti PIN (4–12 znamenki). Tada ga svaka nova prijava traži prije
  otvaranja odjeljka; otključano vrijedi do 4 sata, do odjave ili dok ne klikneš lokot. Pogrešni PIN-ovi
  računaju se kao neuspjele prijave.
- Tab preglednika uvijek se zove samo „Streamero".

Na Androidu je odjeljak u kartici Biblioteke, s istim mjestima (Početna, Scene, Filmovi, Fotografije,
Izvođači, Studiji, Oznake), pretragom i preglednikom slika (prelazak prstom, zoom prstima ili dvostrukim
dodirom, prezentacija; na televizoru D-pad lijevo/desno i srednja tipka). Android ne dopušta snimke zaslona
tih ekrana i ne sprema slike na uređaj, pa nema ni preuzimanja izvornika. Starija iPhone aplikacija
prikazuje samo scene, studije i izvođače.

## Ograničenja

- Biblioteka za odrasle nema titlove, poglavlja, preuzimanja ni zajedničko gledanje.
- Administratori vide biblioteku za odrasle (naziv i mapu) u postavkama biblioteka, ali ne i njen sadržaj
  dok sami sebi ne daju pristup.
- Restart servera zaključava odjeljak u sesijama koje koriste PIN.
- CBR i 7z arhive nisu galerije; galerije se ne traže u ThePornDB-u/StashDB-u, nego se vežu uz scenu.
