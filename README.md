# OpenMenu

Planificateur de repas : à partir de listes pondérées, OpenMenu tire au sort les
repas de la semaine en tenant compte de la saison. Un repas est soit un **plat
complet** (« Lasagnes »), soit un **repas composé** d'un féculent, d'une viande ou
d'un poisson et d'un légume.

- **Semaine** (page d'accueil) : la semaine courante, jour par jour, midi et soir.
  Le jour de début de semaine se règle sur la page. Une semaine vide propose
  « Générer » ; chaque repas peut ensuite être relancé, choisi ou vidé, et chaque
  composant d'un repas composé relancé seul (↻).
- **Paramètres** : un onglet par liste (plats complets, féculents, viandes et
  poissons, légumes) avec poids général et poids par saison (0 à 10), ajout et
  suppression ; un onglet Repas pour les repas à planifier et la part de repas
  composés (0 à 100 %).

Les chances d'un élément un jour donné valent `poids général × poids de la saison`
(saisons météorologiques : hiver = déc.–fév., etc.). Un élément ne revient pas dans
la même semaine tant qu'il reste d'autres candidats dans sa liste.

Le cahier des charges complet est dans [`specs/cahier-des-charges.md`](specs/cahier-des-charges.md).

## Lancer

```bash
cp config.yaml.dist config.yaml
make run            # ou : go run . -c config.yaml
```

Puis ouvrir <http://127.0.0.1:8080>. La base SQLite (`open-menu.db`) est créée et
migrée au démarrage. Le binaire embarque gabarits, CSS/JS et traductions.

```bash
make debug          # build/open-menu
make release        # binaires linux/darwin/windows, version = dernier tag git
make test           # go test -race ./...
open-menu --help    # options et variables d'environnement OGM_*
```

## Configuration

Fichier YAML (`-c`, défaut `config.yaml`) ou variables d'environnement préfixées
`OGM_` : `OGM_HOST`, `OGM_PORT`, `OGM_RATE_LIMIT`, `OGM_TRUSTED_PROXIES`,
`OGM_LOG_LEVEL`, `OGM_LOG_FORMAT`, `OGM_I18N_DEFAULT_LANGUAGE`, `OGM_DB_DRIVER`
(`sqlite` uniquement), `OGM_DB_DSN`.

« Aujourd'hui » suit le fuseau du processus : positionner `TZ` si besoin.

## Sécurité

L'application est mono-foyer et **n'a pas d'authentification**. Exposée au-delà
du réseau local, elle doit se placer derrière un reverse proxy qui authentifie
(basic auth, SSO…). Elle protège ses formulaires contre le CSRF, limite le débit
par IP et envoie une CSP stricte.

## Langues

Français et anglais. La langue se choisit en pied de page (`?lang=`, mémorisée en
cookie), sinon d'après le navigateur. Ajouter une langue : un fichier
`locales/<tag>.yaml` aligné sur les autres (le test `TestLocalesAligned` le vérifie).

## Licence

[WTFPL](https://www.wtfpl.net/)
