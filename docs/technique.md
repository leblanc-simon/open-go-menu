# Documentation technique

Ce document décrit le fonctionnement interne d'OpenGoMenu pour qui le maintient.
Les exigences (références EF, RG, EI, ENF, SEC, TR) sont dans le
[cahier des charges](../specs/cahier-des-charges.md), qui fait foi ; le déploiement
est décrit dans [deploiement.md](deploiement.md).

## Vue d'ensemble

- Go, bibliothèque standard pour le HTTP (`http.ServeMux`, `html/template`), sans
  framework web, CSS ou JS (ENF-01).
- Socle [`leblanc.io/open-go-base`](https://leblanc.io/open-go-base) pour la
  configuration (`appconf`), les journaux (`logx`), la limite de débit
  (`ratelimit`), les traductions (`i18n`) et le CSRF (`csrfx`). Ces briques ne sont
  pas ré-implémentées ici (ENF-02).
- SQLite en pur Go (`modernc.org/sqlite`) : le binaire se compile avec
  `CGO_ENABLED=0` et n'a aucune dépendance système (ENF-04).
- Gabarits, ressources statiques, traductions et migrations sont embarqués avec
  `go:embed` (ENF-03).

```
main.go              câblage, go:embed, pile de middlewares, arrêt propre
internal/config      Config = fragments appconf (Web, Logging, I18n, Database), préfixe OGM_
internal/planner     logique pure : saisons, bornes de semaine, tirage pondéré
internal/shopping    logique pure : lignes de la liste de courses, rayons, cases cochées
internal/store       SQLite : migrations, éléments, planning, réglages, articles, liste
internal/web         handlers, vues, rendu, flash, langue, en-têtes de sécurité
templates/           layout.html, home.html, settings.html, shopping.html, error.html
static/              css/main.css, js/app.js, logos opengomenu-logo*.svg (favicon, horizontaux avec accroche, en-tête sans accroche clair et sombre)
locales/             fr.yaml, en.yaml
deploy/              unité systemd, site nginx
```

## Cycle d'une requête

Pile de middlewares, de l'extérieur vers l'intérieur (`main.go`) :

```
SecurityHeaders ─▶ RememberLanguage ─┬─ GET /static/*  ─▶ http.FileServer (embed)
                                     └─ /*  ─▶ ratelimit ─▶ csrfx ─▶ routes (internal/web)
```

1. **`SecurityHeaders`** (`internal/web/middleware.go`) : CSP stricte
   `default-src 'self'` sans `unsafe-inline`, `X-Frame-Options: DENY`,
   `X-Content-Type-Options`, `Referrer-Policy` (SEC-02). Conséquence : ni `style=`
   ni `<script>` inline dans les gabarits.
2. **`RememberLanguage`** : un `?lang=` valide est mémorisé dans le cookie `lang`
   (ENF-06).
3. **`/static/`** échappe au limiteur : un chargement de page en demande plusieurs.
4. **`ratelimit`** : quota par IP cliente et par minute. L'IP réelle vient de
   `X-Forwarded-For` (stratégie « rightmost trusted ») puis `X-Real-IP`, seulement
   si la connexion vient d'un proxy de `trusted_proxies` (SEC-03).
5. **`csrfx`** : jeton par double soumission. Le cookie `csrf_token` est posé sur
   les GET et le champ `csrf_token` est exigé sur les POST ; sinon 403 (SEC-01).
6. **Routes** (`App.Routes`, `internal/web/app.go`).

Le serveur a des délais d'en-tête, de lecture et d'écriture, et s'arrête proprement
sur `SIGINT` / `SIGTERM` (15 s au plus).

### Routes

| Méthode | Chemin | Handler | Rôle |
|---|---|---|---|
| GET | `/` | `home` | Semaine ; `?week=AAAA-MM-JJ` = n'importe quel jour de la semaine voulue |
| GET | `/manifest.webmanifest` | `manifest` | Manifeste de l'application installable (EI-06) |
| GET | `/sw.js` | `main.go` | Service worker (`static/sw.js`), servi à la racine, hors limiteur |
| POST | `/week-start` | `setWeekStart` | Jour de début de semaine (EF-02) |
| POST | `/plan/generate` | `generate` | Génère, complète (`mode=fill`) ou régénère (`mode=all`) la semaine |
| POST | `/plan/{day}/{meal}` | `editSlot` | `action=reroll` (avec `kind=` : un seul composant), `set`, `clear`, `move` |
| GET | `/settings`, `/settings/{list}` | `settings`, `settingsList` | Onglet d'une liste : `dishes`, `starches`, `proteins`, `vegetables` |
| POST | `/settings/{list}` | `addItem` | Ajout d'un élément |
| POST | `/settings/{list}/save` | `saveItems` | Enregistrement groupé d'une liste |
| POST | `/settings/items/{id}/delete` | `deleteItem` | Suppression logique |
| GET, POST | `/settings/meals` | `mealsSettings`, `saveMeals` | Repas planifiés par jour (`slot=wednesday/lunch`), part de repas composés |
| GET, POST | `/settings/meals/{weekday}/{meal}` | `fixedMeal`, `saveFixedMeal` | Repas fixe (`action=set` / `clear`, EF-18) |
| GET | `/settings/items/{id}` | `dishIngredients` | Ingrédients d'un plat complet (EF-19) |
| POST | `/settings/items/{id}/ingredients` | `addIngredient` | Ajout d'un ingrédient |
| POST | `/settings/items/{id}/ingredients/save` | `saveQuantities` | Quantités des ingrédients |
| POST | `/settings/items/{id}/ingredients/{article}/delete` | `removeIngredient` | Retrait d'un ingrédient |
| GET, POST | `/settings/articles`, `…/save`, `…/{id}/delete` | `catalog.go` | Onglet Articles (EF-21) |
| GET, POST | `/settings/aisles`, `…/save`, `…/order`, `…/{id}/move`, `…/{id}/delete` | `catalog.go` | Onglet Rayons (EF-20) ; `order` reçoit l'ordre complet du glisser-déposer |
| GET | `/shopping`, `/shopping/new` | `shopping`, `newShopping` | Liste en cours, choix de la période (EF-22) |
| POST | `/shopping` | `startShopping` | Nouvelle liste (`from`, `to`) |
| POST | `/shopping/extras` | `addExtra` | Ajout à la main (EF-23) |
| POST | `/shopping/articles/{id}` | `shoppingLine` | `action=tick` (avec `needs`), `untick`, `remove`, `aisle` |

`action=move&to=AAAA-MM-JJ/meal` échange les assiettes des deux repas, ou déplace
simplement l'assiette si la destination est vide (`Store.SwapPlates`, une seule
transaction).

### Schéma POST → redirection → GET

Toute action mutante répond `303 See Other` vers la page d'origine (EI-04). Le
message de confirmation voyage dans un cookie flash qui contient une **clé**
`flash.*`, jamais un texte : la page suivante la traduit dans la langue du
moment. Les redirections après une action sur un repas visent l'ancre du jour
(`#day-AAAA-MM-JJ`).

Les erreurs de saisie du paramétrage ré-affichent le formulaire en `422` avec les
valeurs saisies et un message par champ (EI-05). Les requêtes mal formées (date ou
repas inconnus, identifiant d'une autre liste) répondent `400` avec la page
d'erreur.

## Logique de tirage (`internal/planner`)

Package pur, sans E/S, entièrement couvert par les tests.

- **Jours** : `time.Time` à minuit UTC (`planner.Day`), format `2006-01-02`. Cela
  évite tout souci d'heure d'été dans l'arithmétique de dates. « Aujourd'hui » est
  calculé dans le fuseau du processus puis ramené à ce format.
- **Saisons** météorologiques, évaluées jour par jour (RG-06) : une semaine à
  cheval sur deux saisons est gérée.
- **Semaine** : 7 jours à partir du dernier jour de début de semaine ≤ au jour
  demandé (`WeekStart`, `WeekDays`).
- **Sortes** (`Kind`) : `Dish` (plat complet), `Starch`, `Protein`, `Vegetable`.
  Une assiette (`Plate`) est un tableau indexé par `Kind` d'identifiants (0 =
  rien) : soit un plat complet, soit des composants.
- **`Drawer.Draw`** : pour chaque repas visé, choisit la forme (composé avec la
  probabilité `ComposedShare`, repli sur l'autre forme si l'une n'a aucun candidat
  ce jour-là, RG-08), puis tire chaque élément avec un poids effectif
  `poids général × poids de la saison` (RG-01, RG-02). Un élément ne revient pas
  dans la semaine tant qu'il reste des candidats dans sa liste ; ensuite le tirage
  évite l'élément du repas précédent (RG-03, RG-04).
- **`DrawComponent`** relance un seul composant d'un repas composé (EF-09).
- **Repas de la semaine** : `Weekly` (« mercredi midi ») ; `Schedule` dit quels
  repas sont planifiés chaque jour et produit les créneaux d'une semaine
  (`Schedule.Slots`, EF-13).
- **Repas fixes** : `Drawer.Fixed` sert, sur les repas visés, l'assiette fixée pour
  leur repas de la semaine avant de tirer les autres, qui évitent ses éléments
  (RG-10). Seul `generate` le renseigne : relancer un repas fixe le tire au sort.

Le générateur aléatoire est injecté (`Drawer.Rng`), ce qui rend les tests
déterministes.

## Stockage (`internal/store`)

- **Connexion** : pragmas `foreign_keys`, `busy_timeout(5000)` et
  `journal_mode(WAL)` ajoutés au DSN, pour que chaque connexion du pool les
  reçoive ; `SetMaxOpenConns(1)`, puisque SQLite sérialise les écritures de toute
  façon.
- **Migrations** : `migrations/NNN_*.sql`, embarquées, appliquées dans l'ordre au
  démarrage et tracées dans `schema_migrations`. **Ne jamais modifier une
  migration publiée** : en ajouter une nouvelle.
- **Modèle** :

  ```sql
  items      (id, kind, name, weight, weight_spring, weight_summer, weight_autumn,
              weight_winter, created_at, deleted_at)
  plan_items (day, meal, kind, item_id → items.id, PRIMARY KEY (day, meal, kind))
  settings   (key PRIMARY KEY, value)   -- week_start, composed_share
  weekly_meals (weekday, meal)          -- repas planifiés, 0 = dimanche
  fixed_items  (weekday, meal, kind, item_id → items.id)
  articles     (id, name, aisle_id)     -- nom unique, COLLATE NOCASE
  aisles       (id, name, position)
  dish_ingredients   (item_id, article_id, position, quantity)
  component_articles (item_id, article_id)
  shopping_extras    (article_id, quantity)
  shopping_checks    (article_id, needs)
  ```

  Un plat complet occupe une ligne `kind = 'dish'` dans `plan_items` ; un repas
  composé, une ligne par composant.
- **Suppression logique** (`deleted_at`) : l'élément quitte les listes et les
  tirages, mais les plannings passés gardent son nom (EF-12). L'unicité du nom ne
  porte que sur les éléments vivants d'une même sorte (index partiel,
  `COLLATE NOCASE`, EF-14).
- **Repas planifiés** par jour de la semaine (`Store.Schedule`, EF-13) ; la migration
  `004` a reporté l'ancien réglage global `meals` sur chaque jour. Table vide → tous
  les repas.
- **Repas fixes** (`Store.FixedPlates`, EF-18) : seuls les éléments vivants sont
  renvoyés ; un repas fixe dont tout a été supprimé redevient tiré au sort.
- **Articles** (EF-19, EF-21) : un ingrédient ou un ajout à la main retrouve
  l'article de son nom (insensible à la casse, ASCII seulement comme `NOCASE`) ou le
  crée. Un article reste au catalogue hors de toute recette ; sa suppression est
  refusée tant qu'il est ingrédient (`ErrInUse`). Les suggestions réunissent
  articles et composants vivants (`ArticleSuggestions`) ; un article ne devient
  jamais un composant.
- **Liste de courses** : sa période est dans `settings` (`shopping_from`,
  `shopping_to`) ; `StartShopping` vide ajouts et cases. `ShoppingRows` relie
  d'abord chaque composant planifié encore sans article à l'article de son nom
  (`component_articles`), puis renvoie les besoins. Le handler écarte les repas non
  planifiés et confie le regroupement à `internal/shopping`.
- **Rayons** : `MoveAisle` échange deux voisins et renumérote tout, ce qui répare
  d'éventuelles positions en double.
- **Planning par date**, jamais par semaine (RG-07) : changer le jour de début de
  semaine ne fait que décaler la fenêtre affichée.
- **Échange de repas** : `SwapPlates` gare temporairement les lignes du premier
  repas sous un nom de repas fictif, le temps de déplacer celles du second, ce que
  la clé primaire impose.

## Rendu et traductions (`internal/web`)

- Les gabarits sont analysés une fois au démarrage, puis **clonés à chaque
  requête** pour y injecter `T` / `Tn` (le traducteur de la langue de la requête)
  et `csrfField`.
- **Aucune chaîne en dur** : clés anglaises `écran.élément` dans
  `locales/fr.yaml` et `locales/en.yaml`. Les segments transverses ont leurs
  propres racines : `weekday.*`, `month.*`, `season.*`, `meal.*`, `kind.*`,
  `date.*`, `validation.*`, `flash.*`, `error.*`.
- Les libellés composés côté serveur passent par le traducteur
  (`dayMonth`, `home.week_range`, `home.move_target`…) : l'ordre des fragments
  appartient à la traduction.
- Un texte qui varie selon la sorte (genre en français) a une clé par sorte
  (`settings.add.<kind>`), jamais une concaténation.
- `TestLocalesAligned` vérifie que les catalogues ont exactement les mêmes clés.
  Ajouter une langue revient à ajouter `locales/<tag>.yaml`.
- Langue choisie : `?lang=`, puis le cookie, puis `Accept-Language`, puis
  `i18n.default_language`.

## Interface et JavaScript

Tout fonctionne **sans JavaScript** (EI-02). `static/js/app.js` n'apporte que du
confort, sans dépendance, et reçoit ses libellés déjà traduits par des attributs
`data-` :

| Amélioration | Repli sans JS |
|---|---|
| Le sélecteur du jour de début s'envoie seul (`data-autosubmit`) | Bouton « Appliquer » |
| Les formulaires de la semaine sont postés en `fetch` ; la page renvoyée remplace `<main>`, en gardant le repas concerné au même endroit à l'écran et le focus sur le même bouton. La confirmation s'affiche en bas d'écran (`flash--toast`) au lieu de décaler la page | Rechargement complet, ancre sur le jour |
| Glisser-déposer des rayons par leur poignée ⠿ (pointeur précis uniquement) : la ligne suit le pointeur, l'ordre complet est envoyé au lâcher par le formulaire `#aisle-order` ; lâcher hors de la liste ou Échap rétablit l'ordre | Boutons ↑/↓ de chaque rayon |
| Les cases et formulaires de la liste de courses (section `[data-in-place]`, comme la semaine) sont postés de même, à position de défilement constante | Rechargement complet, ancre sur le rayon |
| Glisser-déposer d'un repas sur un autre (pointeur précis uniquement) | Menu « Déplacer vers… » de chaque repas |
| « Déplacer » prend le repas : les autres repas deviennent des boutons « Mettre ici » / « Échanger ici », une barre en bas propose « Annuler » (Échap aussi) | Même menu « Déplacer vers… » |

Les deux façons de déplacer un repas remplissent puis soumettent le formulaire
« Déplacer » du repas d'origine. Le serveur n'a donc qu'un seul chemin, celui
qu'utilise aussi le repli sans JS.

Si l'envoi en `fetch` échoue (réseau, réponse sans `<main>`), le script retombe
sur un envoi classique du formulaire, en y recopiant le bouton pressé.

### Application installable (EI-06)

- **Manifeste** `/manifest.webmanifest` (`internal/web/pwa.go`) : produit à chaque
  requête pour être traduit (nom, description, raccourcis Semaine et Courses). Ses
  couleurs reprennent les jetons clairs de `main.css` ; les `<meta name="theme-color">`
  du gabarit donnent aussi la variante sombre. Le lien porte
  `crossorigin="use-credentials"`, sans quoi le navigateur le demanderait sans les
  identifiants de l'authentification du reverse proxy.
- **Icônes** dans `static/icons/`, tirées des SVG du logo par Inkscape :
  `icon-192/512.png` (coins transparents), `icon-maskable-192/512.png` et
  `apple-touch-icon.png` (fond plein, dessin dans la zone sûre), à partir de
  `static/opengomenu-logo-maskable.svg`. Pour les régénérer :
  `inkscape --export-type=png --export-width=512 -o static/icons/icon-512.png static/opengomenu-logo.svg`.
- **Service worker** `static/sw.js`, servi en `/sw.js` pour couvrir tout le site :
  réseau d'abord pour toutes les requêtes GET de l'origine, copie mise en cache à
  chaque réponse 200, cache utilisé seulement si le réseau échoue. En ligne, rien ne
  change donc, même après une mise à jour du binaire. Les POST ne passent pas par
  le cache.
- **Hors ligne**, `app.js` affiche un bandeau (`data-offline` sur `<body>`) et
  bloque l'envoi des formulaires, qui finiraient sur la page d'erreur du navigateur.

CSS : nesting natif, modificateurs BEM écrits `&.bloc--modif` (le gabarit, ou le
script, porte les deux classes), jetons de couleur sur `:root`, mode sombre par
`prefers-color-scheme`, `prefers-reduced-motion` respecté (EI-03).

## Configuration

`internal/config.Config` assemble les fragments `appconf` avec le préfixe `OGM_`.
Le fichier YAML vient de `-c` (défaut `config.yaml`) ; s'il n'existe pas, seules les
variables d'environnement et les défauts s'appliquent. Liste complète :
`open-go-menu --help` ou [deploiement.md](deploiement.md#clés-et-variables-denvironnement).

## Tests

```bash
make test                                   # go test -race ./...
make vet
go test -run '^TestMoveMeal$' ./internal/web/
```

- `internal/planner` : saisons, bornes de semaine, pondération, non-répétition,
  formes de repas.
- `internal/shopping` : ordre des rayons et des lignes, fusion des besoins d'un
  article, décochage sur besoin nouveau (RG-11, RG-12).
- `internal/store` : cycle de vie des éléments, unicité par sorte, assiettes et
  échange, migrations `001 → 002`, `003 → 004` et `004 → 005` sans perte, réglages,
  repas fixes, ingrédients, rayons et articles, liste de courses.
- `internal/web` : la vraie pile (langue, CSRF, routes) derrière `httptest`, avec
  un client qui garde ses cookies et ne suit pas les redirections. « Aujourd'hui »
  y est figé au jeudi 8 octobre 2026. Les scénarios de recette TR-xx y sont
  référencés.

## Construire et publier

```bash
make run        # depuis les sources, avec config.yaml
make debug      # build/open-go-menu
make release    # linux/darwin (amd64, arm64) et windows (amd64)
```

La version vient du dernier tag git, injectée par
`-ldflags "-X main.version=…"` ; sans tag, `develop`. Les binaires de release sont
compilés avec `-trimpath -s -w` et `CGO_ENABLED=0`.

## Évolutions prévues

Voir le cahier des charges §7 (articles récurrents, nombre de convives…). La table
`items` n'est pas modifiée pour ce qui concerne l'achat.
