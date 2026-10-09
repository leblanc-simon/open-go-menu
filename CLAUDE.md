# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Référence

`specs/cahier-des-charges.md` fait foi : exigences numérotées (EF fonctionnelles, RG règles
de tirage, EI interface, ENF non fonctionnelles, SEC sécurité, TR recette) et évolutions V2
(ingrédients, liste de courses). Rattacher toute décision à une référence de ce document.

## Commandes

- `make test` (`go test -race ./...`), `make vet`, `make debug`, `make run`.
- Un test : `go test -run '^TestGenerateWeek$' ./internal/web/`.

## Architecture

- `main.go` : câblage, `go:embed` de `templates/`, `static/`, `locales/` (binaire autonome).
  Chaîne : `SecurityHeaders` → `RememberLanguage` → `/static/` hors limiteur | `ratelimit` →
  `csrfx` → routes.
- `internal/planner` : logique **pure** et testée — saisons (météorologiques, évaluées jour par
  jour), bornes de semaine (jours en minuit UTC, pas de souci d'heure d'été), tirage pondéré.
  `Weekly` = repas de la semaine (« mercredi midi ») ; `Schedule` = repas planifiés par jour,
  dont `Slots(days)` tire les créneaux. `Drawer.Fixed` sert les repas fixes avant le tirage
  (uniquement à la génération de la semaine, pas à la relance d'un repas).
  Quatre sortes d'éléments (`Kind` : `Dish` plat complet, `Starch`, `Protein`, `Vegetable`) ;
  une assiette (`Plate`, tableau indexé par `Kind`, 0 = rien) est soit un plat complet, soit
  des composants. `Drawer.Draw` choisit la forme selon `ComposedShare` (repli sur l'autre forme
  si l'une n'a pas de candidat), puis tire chaque élément (poids général × poids saisonnier, pas
  de doublon par liste tant qu'il reste des candidats, puis repli en évitant le repas
  précédent). `DrawComponent` relance un seul composant.
- `internal/shopping` : logique **pure** de la liste de courses — regroupe les besoins
  (`Row` : article + repas ou ajout à la main) en lignes par article, rangées par rayon ;
  une ligne cochée retient la clé de ses besoins (`Line.Key`) et se décoche si un besoin
  nouveau apparaît.
- `internal/store` : SQLite (`modernc.org/sqlite`, pur Go). Migrations `migrations/NNN_*.sql`
  embarquées, appliquées au démarrage et tracées dans `schema_migrations` ; ne jamais modifier
  une migration publiée, en ajouter une. Table `items` (une colonne `kind`, nom unique par
  sorte) et `plan_items` (une ligne par `(day, meal, kind)`). Suppression **logique**
  (`deleted_at`) : les plannings passés gardent le nom. Planning stocké par date, jamais par
  semaine — changer le jour de début ne perd rien. `weekly_meals` (repas planifiés par jour,
  `weekday` 0 = dimanche), `fixed_items` (repas fixes, même forme que `plan_items`),
  `articles` (tout ce qui s'achète, avec rayon) + `dish_ingredients` (ingrédients des plats,
  quantité en texte libre), `component_articles` (article acheté pour un composant, relié par
  le nom à la première liste), `aisles` (ordonnés), `shopping_extras` / `shopping_checks` (la
  liste en cours, vidée à chaque nouvelle liste ; sa période dans `settings`).
- `internal/web` : handlers `net/http` (stdlib, pas de framework), `html/template` parsés une
  fois puis clonés par requête pour y injecter `T`/`Tn` (localizer) et `csrfField`. POST →
  redirection 303 → GET, message flash = **clé** `flash.*` en cookie (jamais de texte libre).
  Les erreurs de saisie ré-affichent le formulaire en 422 avec les valeurs saisies.

Socle `leblanc.io/open-go-base` (appconf, logx, ratelimit, i18n via `NewFS`, csrfx) : ne pas
ré-implémenter ces briques. Préfixe d'env `OGM_`.

## Conventions

- **Aucune chaîne en dur** : clés anglaises `écran.élément` dans `locales/fr.yaml` et
  `locales/en.yaml`, racines transverses `weekday.*`, `month.*`, `season.*`, `meal.*`, `kind.*`,
  `date.*`, `validation.*`, `flash.*`, `error.*`. Les libellés composés côté serveur passent
  par le localizer (ex. `dayMonth`), l'ordre des fragments appartient à la traduction.
  `TestLocalesAligned` vérifie que les catalogues ont les mêmes clés.
- CSS en **nesting natif** ; modificateurs BEM en `&.bloc--modif` (le gabarit porte les deux
  classes). Jetons de couleur sur `:root`, mode sombre via `prefers-color-scheme`.
- Le JS est un confort (envoi auto du sélecteur de jour de début) : tout marche sans.
- CSP stricte : ni style ni script inline.
- Les textes qui varient selon la sorte d'élément (genre en français : « un féculent », « une
  viande ») ont une clé par sorte (`settings.add.<kind>`), jamais une concaténation.
- Dans le formulaire groupé d'une liste, les boutons « Supprimer » visent des formulaires
  séparés via l'attribut `form=` : ainsi Entrée dans un champ déclenche « Enregistrer », pas
  une suppression.

## Liste de courses

Ponctuelle : une seule liste à la fois, sur une période ; une nouvelle liste efface cases et
ajouts. Calculée à chaque affichage depuis le planning (seuls les repas planifiés, EF-13). Les
formulaires des sections `[data-in-place]` (semaine, liste) sont postés en `fetch` par le JS.
Ne pas modifier la table `items` : ce qui concerne l'achat vit dans les tables d'articles.
