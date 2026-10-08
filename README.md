# OpenMenu

Planificateur de repas : à partir d'une liste de menus pondérés, OpenMenu tire au
sort les repas de la semaine en tenant compte de la saison.

- **Semaine** (page d'accueil) : la semaine courante, jour par jour, midi et soir.
  Le jour de début de semaine se règle sur la page. Une semaine vide propose
  « Générer » ; chaque repas peut ensuite être relancé, choisi ou vidé.
- **Paramètres** : la liste des menus, leur poids général et leurs poids par
  saison (0 à 10), l'ajout et la suppression de menus, les repas à planifier.

Les chances d'un menu un jour donné valent `poids général × poids de la saison`
(saisons météorologiques : hiver = déc.–fév., etc.). Un menu ne revient pas dans
la même semaine tant qu'il reste d'autres candidats.

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
