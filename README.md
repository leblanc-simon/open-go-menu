# OpenGoMenu

Planificateur de repas : à partir de listes pondérées, OpenGoMenu tire au sort les
repas de la semaine en tenant compte de la saison. Un repas est soit un **plat
complet** (« Lasagnes »), soit un **repas composé** d'un féculent, d'une viande ou
d'un poisson et d'un légume.

- **Semaine** (page d'accueil) : la semaine courante, jour par jour, midi et soir.
  Le jour de début de semaine se règle sur la page. Une semaine vide propose
  « Générer » ; chaque repas peut ensuite être relancé, choisi ou vidé, et chaque
  composant d'un repas composé relancé seul (↻).
- **Paramètres** : un onglet par liste (plats complets, féculents, viandes et
  poissons, légumes) avec poids général (0 à 10) et bonus ou malus par saison
  (-10 à +10), ajout et
  suppression ; un onglet Repas pour les repas à planifier et la part de repas
  composés (0 à 100 %).

Les chances d'un élément un jour donné valent `poids général + bonus de la saison`
(saisons météorologiques : hiver = déc.–fév., etc.) : le poids général prime, la
saison ajoute ou retire des points ; un poids général à 0 ou un total nul écarte
l'élément. Un élément ne revient pas dans
la même semaine tant qu'il reste d'autres candidats dans sa liste.

Le cahier des charges complet est dans [`specs/cahier-des-charges.md`](specs/cahier-des-charges.md).

Documentation :

- [Documentation technique](docs/technique.md) : architecture, cycle d'une requête,
  tirage, stockage, traductions, JavaScript, tests.
- [Déploiement en production](docs/deploiement.md) : binaire, configuration,
  service systemd, reverse proxy nginx, sauvegardes, mises à jour, dépannage.
  Fichiers prêts à l'emploi dans [`deploy/`](deploy).

## Lancer

```bash
cp config.yaml.dist config.yaml
make run            # ou : go run . -c config.yaml
```

Puis créer une famille et ouvrir <http://127.0.0.1:8080> :

```bash
go run . -c config.yaml create-family dupont      # affiche son mot de passe
go run . -c config.yaml invite                    # ou : un code pour /signup
```

Les bases SQLite (`data/accounts.db`, puis une par famille dans `data/families/`)
sont créées et migrées au démarrage. Le binaire embarque gabarits, CSS/JS et
traductions.

```bash
make debug          # build/open-go-menu
make release        # binaires linux/darwin/windows, version = dernier tag git
make test           # go test -race ./...
open-go-menu --help    # options et variables d'environnement OGM_*
```

## Configuration

Fichier YAML (`-c`, défaut `config.yaml`) ou variables d'environnement préfixées
`OGM_` : `OGM_HOST`, `OGM_PORT`, `OGM_RATE_LIMIT`, `OGM_TRUSTED_PROXIES`,
`OGM_LOG_LEVEL`, `OGM_LOG_FORMAT`, `OGM_I18N_DEFAULT_LANGUAGE`, `OGM_DATA_DIR`,
`OGM_SIGNUP` (`open`, `invite`, `closed`), `OGM_INACTIVE_MONTHS`, `OGM_AUTH_*`.

« Aujourd'hui » suit le fuseau du processus : positionner `TZ` si besoin.

## Familles et sécurité

L'application est **multi-familles** : chaque famille a un identifiant et un mot
de passe partagés par ses membres, et ses données dans sa propre base SQLite.
Aucune donnée personnelle n'est demandée, pas même un e-mail : un mot de passe
oublié se réinitialise par `open-go-menu reset-password <identifiant>`. On reste
connecté jusqu'à « Se déconnecter ». Une famille sans visite pendant 12 mois est
supprimée par `open-go-menu purge` (timer systemd fourni).

L'inscription est ouverte, sur invitation (défaut) ou fermée. Mots de passe en
bcrypt, limite de tentatives par identifiant, protection CSRF, limite de débit par
IP, CSP stricte. Voir [docs/deploiement.md](docs/deploiement.md).

## Langues

Français et anglais. La langue se choisit en pied de page (`?lang=`, mémorisée en
cookie), sinon d'après le navigateur. Ajouter une langue : un fichier
`locales/<tag>.yaml` aligné sur les autres (le test `TestLocalesAligned` le vérifie).

## Licence

[WTFPL](https://www.wtfpl.net/)
