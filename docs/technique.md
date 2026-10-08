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
internal/store       SQLite : migrations, éléments, planning, réglages
internal/web         handlers, vues, rendu, flash, langue, en-têtes de sécurité
templates/           layout.html, home.html, settings.html, error.html
static/              css/main.css, js/app.js, favicon.svg
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
| POST | `/week-start` | `setWeekStart` | Jour de début de semaine (EF-02) |
| POST | `/plan/generate` | `generate` | Génère, complète (`mode=fill`) ou régénère (`mode=all`) la semaine |
| POST | `/plan/{day}/{meal}` | `editSlot` | `action=reroll` (avec `kind=` : un seul composant), `set`, `clear`, `move` |
| GET | `/settings`, `/settings/{list}` | `settings`, `settingsList` | Onglet d'une liste : `dishes`, `starches`, `proteins`, `vegetables` |
| POST | `/settings/{list}` | `addItem` | Ajout d'un élément |
| POST | `/settings/{list}/save` | `saveItems` | Enregistrement groupé d'une liste |
| POST | `/settings/items/{id}/delete` | `deleteItem` | Suppression logique |
| GET, POST | `/settings/meals` | `mealsSettings`, `saveMeals` | Repas planifiés, part de repas composés |

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
  settings   (key PRIMARY KEY, value)   -- week_start, meals, composed_share
  ```

  Un plat complet occupe une ligne `kind = 'dish'` dans `plan_items` ; un repas
  composé, une ligne par composant.
- **Suppression logique** (`deleted_at`) : l'élément quitte les listes et les
  tirages, mais les plannings passés gardent son nom (EF-12). L'unicité du nom ne
  porte que sur les éléments vivants d'une même sorte (index partiel,
  `COLLATE NOCASE`, EF-14).
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
| Glisser-déposer d'un repas sur un autre (pointeur précis uniquement) | Menu « Déplacer vers… » de chaque repas |
| « Déplacer » prend le repas : les autres repas deviennent des boutons « Mettre ici » / « Échanger ici », une barre en bas propose « Annuler » (Échap aussi) | Même menu « Déplacer vers… » |

Les deux façons de déplacer un repas remplissent puis soumettent le formulaire
« Déplacer » du repas d'origine. Le serveur n'a donc qu'un seul chemin, celui
qu'utilise aussi le repli sans JS.

Si l'envoi en `fetch` échoue (réseau, réponse sans `<main>`), le script retombe
sur un envoi classique du formulaire, en y recopiant le bouton pressé.

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
- `internal/store` : cycle de vie des éléments, unicité par sorte, assiettes et
  échange, migration `001 → 002` sans perte, réglages.
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

La V2 (ingrédients, liste de courses, cahier des charges §7) s'ajoute sans refonte :
une migration `003` créant `ingredients` et `item_ingredients`, une fiche
`/settings/items/{id}` et l'agrégation des ingrédients de la semaine. La table
`items` n'est pas modifiée.
