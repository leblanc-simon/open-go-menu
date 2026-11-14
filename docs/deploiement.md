# Déploiement en production

OpenGoMenu est un binaire Go autonome : gabarits, CSS/JS, traductions et migrations
SQL sont embarqués. En production, il tourne comme service **systemd** et écoute en
local ; **nginx** se place devant pour TLS. L'application authentifie elle-même
les familles (cahier des charges §3.5) : une base des comptes, et une base SQLite
par famille.

```
navigateur ──HTTPS──▶ nginx ──HTTP──▶ open-go-menu ──▶ /var/lib/open-go-menu/data/
                      TLS               127.0.0.1:8080   accounts.db
                      X-Forwarded-*     (systemd)        families/<id>.db
```

Les fichiers prêts à l'emploi sont dans [`deploy/`](../deploy) :

| Fichier | Destination |
|---|---|
| `deploy/open-go-menu.service` | `/etc/systemd/system/open-go-menu.service` |
| `deploy/open-go-menu-purge.service`, `deploy/open-go-menu-purge.timer` | `/etc/systemd/system/` |
| `deploy/nginx.conf` | `/etc/nginx/sites-available/open-go-menu` (ou `conf.d/`) |

## 1. Construire le binaire

Sur une machine avec Go (version de `go.mod`) :

```bash
git checkout v1.2.0          # la version vient du dernier tag git
make build-linux             # build/open-go-menu-<version>-linux-{amd64,arm64}
```

Le binaire est statique (`CGO_ENABLED=0`, SQLite en pur Go) : il n'a besoin d'aucune
bibliothèque sur le serveur. Vérifier la version embarquée :

```bash
./build/open-go-menu-v1.2.0-linux-amd64 --version
```

Installer sur le serveur :

```bash
install -m 0755 open-go-menu-v1.2.0-linux-amd64 /usr/local/bin/open-go-menu
```

## 2. Configuration

```bash
install -d -m 0755 /etc/open-go-menu
install -m 0644 config.yaml.dist /etc/open-go-menu/config.yaml
```

Puis l'adapter. Configuration recommandée en production :

```yaml
web:
  host: 127.0.0.1          # jamais exposé directement : nginx est devant
  port: 8080
  rate_limit: 100          # requêtes/minute/IP ; 0 désactive
  trusted_proxies:         # adresses d'où nginx se connecte
    - 127.0.0.1/32
    - ::1/128

logging:
  level: info
  format: json             # plus facile à exploiter depuis journald
  source: false

i18n:
  default_language: fr

families:
  data_dir: data           # relatif au répertoire courant : /var/lib/open-go-menu/data
  signup: invite           # open | invite | closed
  inactive_months: 12
  family_invites: false    # true : les familles invitent depuis leur Compte
  family_invite_days: 7
  family_invite_max: 3

auth:
  cookie_secure: auto      # Secure dès que nginx transmet X-Forwarded-Proto: https
  bcrypt_cost: 12
```

Le fichier ne contient aucun secret : il peut rester lisible par tous.

### Clés et variables d'environnement

Chaque clé peut aussi venir d'une variable d'environnement préfixée `OGM_`, qui
**l'emporte sur le fichier**. Sans fichier, seules les variables et les valeurs par
défaut s'appliquent. `open-go-menu --help` liste tout.

| Clé YAML | Variable | Défaut | Rôle |
|---|---|---|---|
| `web.host` | `OGM_HOST` | `127.0.0.1` | Adresse d'écoute |
| `web.port` | `OGM_PORT` | `8080` | Port d'écoute |
| `web.rate_limit` | `OGM_RATE_LIMIT` | `100` | Requêtes par minute et par IP ; `0` désactive |
| `web.trusted_proxies` | `OGM_TRUSTED_PROXIES` | aucun | CIDR des proxys dont on croit `X-Forwarded-For` / `X-Real-IP` (séparés par des virgules en variable) |
| `logging.level` | `OGM_LOG_LEVEL` | `error` | `debug`, `info`, `warn`, `error` |
| `logging.format` | `OGM_LOG_FORMAT` | `text` | `text` ou `json` |
| `logging.source` | `OGM_LOG_SOURCE` | `false` | Ajoute fichier:ligne aux journaux |
| `i18n.default_language` | `OGM_I18N_DEFAULT_LANGUAGE` | `en` | Langue de repli (`fr`, `en`) |
| `families.data_dir` | `OGM_DATA_DIR` | `data` | Répertoire de `accounts.db` et de `families/` ; un chemin relatif part du répertoire courant |
| `families.signup` | `OGM_SIGNUP` | `invite` | `open` (tout le monde), `invite` (code de `open-go-menu invite`), `closed` (seulement `create-family`) |
| `families.inactive_months` | `OGM_INACTIVE_MONTHS` | `12` | Mois sans visite avant suppression par `purge` |
| `families.family_invites` | `OGM_FAMILY_INVITES` | `false` | Avec `signup: invite`, les familles génèrent des codes depuis leur Compte |
| `families.family_invite_days` | `OGM_FAMILY_INVITE_DAYS` | `7` | Validité, en jours, d'un code généré par une famille |
| `families.family_invite_max` | `OGM_FAMILY_INVITE_MAX` | `3` | Codes en attente au plus par famille |
| `auth.cookie_name` | `OGM_AUTH_COOKIE_NAME` | `session` | Nom du cookie de session |
| `auth.cookie_secure` | `OGM_AUTH_COOKIE_SECURE` | `auto` | Attribut `Secure` des cookies de session et CSRF : `auto`, `on`, `off` |
| `auth.bcrypt_cost` | `OGM_AUTH_BCRYPT_COST` | `12` | Coût bcrypt des mots de passe |

`auth.session_ttl` n'est pas lu : la session glisse à chaque visite et dure 400
jours, le plafond des navigateurs. Une famille reste connectée jusqu'à « Se
déconnecter ».

Attention aux défauts du socle : sans configuration, les journaux sont au niveau
`error` et la langue de repli est l'anglais.

Le **fuseau horaire** du processus décide de « aujourd'hui » et donc de la semaine
courante et des saisons. Le serveur tournant souvent en UTC, l'unité systemd fixe
`TZ=Europe/Paris`.

## 3. Service systemd

```bash
install -m 0644 deploy/open-go-menu.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now open-go-menu
systemctl status open-go-menu
```

Ce que fait l'unité :

- **`DynamicUser=yes`**, **`User=open-go-menu`** : un utilisateur système éphémère,
  sans compte à créer, nommé pour être partagé avec la purge et les commandes
  d'administration.
- **`StateDirectory=open-go-menu`** : systemd crée `/var/lib/open-go-menu` (en
  réalité `/var/lib/private/open-go-menu`, lien symbolique), le donne à cet
  utilisateur et en fait le seul endroit inscriptible. C'est aussi le répertoire
  courant : `data/accounts.db` et `data/families/*.db`, avec leurs fichiers
  `-wal` / `-shm`, y vivent.
- **Arrêt** : `SIGTERM` déclenche l'arrêt propre de l'application (requêtes en cours
  terminées, au plus 15 s) ; `TimeoutStopSec=20` laisse cette marge.
- **`Restart=on-failure`** : relance après un plantage, pas après un arrêt demandé.
- **Durcissement** : pas de capacités, système de fichiers en lecture seule hors
  `StateDirectory`, `/home` invisible, familles réseau limitées à IPv4/IPv6/Unix,
  filtre d'appels système `@system-service`. Un appel filtré échoue en `EPERM` au
  lieu de tuer le processus, ce qui rend un éventuel problème lisible dans les
  journaux. `systemd-analyze security open-go-menu` donne le score obtenu.

Pour changer un réglage sans toucher l'unité livrée, utiliser une surcharge :

```bash
systemctl edit open-go-menu
```

```ini
[Service]
Environment=TZ=America/Montreal
Environment=OGM_PORT=8090
```

Journaux (l'application écrit sur sa sortie d'erreur, recueillie par journald) :

```bash
journalctl -u open-go-menu -f
journalctl -u open-go-menu --since today -p warning
```

## 4. Familles

Les commandes d'administration se lancent avec le binaire du service, **sous son
utilisateur** : lancées en root, elles laisseraient des fichiers que le service ne
pourrait plus écrire.

```bash
ogm() {
  systemd-run --quiet --pty --wait --collect \
    -p DynamicUser=yes -p User=open-go-menu -p StateDirectory=open-go-menu \
    -p WorkingDirectory=/var/lib/open-go-menu \
    /usr/local/bin/open-go-menu -c /etc/open-go-menu/config.yaml "$@"
}

ogm invite                         # code d'invitation à usage unique, 30 jours
ogm invite -days 7
ogm create-family dupont           # crée la famille, affiche son mot de passe
ogm reset-password dupont          # nouveau mot de passe, tous les appareils déconnectés
ogm families                       # familles, dernière visite, qui les a invitées
ogm purge -dry-run                 # familles qui seraient supprimées
```

- **Inscription** (`families.signup`) : avec `invite`, on crée une famille sur
  `/signup` avec un code donné par `ogm invite` ; avec `open`, sans code ; avec
  `closed`, seul `ogm create-family` en crée. Avec `family_invites: true` (et
  `signup: invite`), chaque famille génère aussi des codes depuis son Compte et
  envoie le lien d'inscription, qui préremplit le code ; `ogm families` dit qui a
  invité qui.
- **Mot de passe oublié** : aucun e-mail n'existe, la seule issue est
  `ogm reset-password`. Transmettre le mot de passe affiché à la famille, qui le
  change ensuite dans son Compte.
- **Purge** : `ogm purge` supprime les familles sans aucune visite depuis
  `inactive_months` mois (identifiant et base), les sessions expirées et les codes
  d'invitation expirés. Le timer
  livré la lance chaque nuit :

  ```bash
  install -m 0644 deploy/open-go-menu-purge.{service,timer} /etc/systemd/system/
  systemctl daemon-reload
  systemctl enable --now open-go-menu-purge.timer
  journalctl -u open-go-menu-purge     # familles supprimées
  ```

  Aucune famille n'est prévenue (il n'y a pas d'e-mail) : la règle est affichée à
  l'inscription et dans le Compte.

## 5. nginx

### Site

```bash
cp deploy/nginx.conf /etc/nginx/sites-available/open-go-menu
sed -i 's/menu.example.org/menu.mondomaine.fr/g' /etc/nginx/sites-available/open-go-menu
ln -s ../sites-available/open-go-menu /etc/nginx/sites-enabled/
```

Certificat Let's Encrypt, le bloc HTTP servant le défi ACME :

```bash
mkdir -p /var/www/letsencrypt
certbot certonly --webroot -w /var/www/letsencrypt -d menu.mondomaine.fr
nginx -t && systemctl reload nginx
```

Le bloc HTTPS référence le certificat : pour le tout premier certificat, commenter
ce bloc le temps d'exécuter `certbot`, ou utiliser `certbot --nginx`.

### Points à respecter

Ces réglages ne sont pas cosmétiques, l'application en dépend :

| Réglage nginx | Pourquoi |
|---|---|
| `X-Forwarded-For` / `X-Real-IP` | La limite de débit compte par IP cliente. Ces en-têtes ne sont crus que si nginx se connecte depuis une adresse de `trusted_proxies`. Sans ça, toutes les familles partagent le quota de l'IP de nginx. |
| `X-Forwarded-Proto $scheme` | Les cookies de session et CSRF reçoivent l'attribut `Secure` quand la requête d'origine est en HTTPS. |
| Pas de `add_header` CSP, `X-Frame-Options`… | L'application les envoie déjà ; des doublons divergents seraient combinés par le navigateur (une CSP en double ne peut que restreindre davantage). HSTS, lié à TLS, est ajouté par nginx. |
| `client_max_body_size 64k` | Les formulaires font quelques centaines d'octets. |
| `location /static/` | Les ressources statiques échappent déjà à la limite de débit côté application ; nginx ajoute un cache d'une heure. Elles changent avec le binaire, d'où un délai court. |

## 6. Vérifier le déploiement

```bash
H=menu.mondomaine.fr

curl -sI http://$H/ | head -1                          # 301 vers https
curl -s -o /dev/null -w '%{http_code} %{redirect_url}\n' https://$H/   # 303 vers /login
curl -s -D - -o /dev/null https://$H/login \
  | grep -iE '^(HTTP|strict-transport|content-security|set-cookie|x-ratelimit)'
```

Attendu sur `/login` : `200`, HSTS, la CSP de l'application, un cookie
`csrf_token` avec `Secure; HttpOnly; SameSite=Lax`, et `X-RateLimit-Remaining`
qui décroît. Après connexion, le cookie `session` porte les mêmes attributs.

## 7. Sauvegarde et restauration

Les bases sont en mode WAL : copier les seuls fichiers `.db` pendant que le service
tourne peut donner une sauvegarde incohérente. On passe par l'API de sauvegarde de
SQLite (`.backup`, sûre à chaud ; paquet `sqlite3`), base par base : `accounts.db`
puis chaque famille.

La sauvegarde tourne sous **le même utilisateur dynamique** que le service
(`User=open-go-menu`) : lancée en root, `sqlite3` pourrait créer des fichiers
`-wal` / `-shm` appartenant à root, que le service ne pourrait plus écrire.

```sh
#!/bin/sh
# /usr/local/bin/open-go-menu-backup : une copie datée de chaque base.
set -eu
src=/var/lib/open-go-menu/data
dest=/var/lib/open-go-menu-backups/$(date +%F-%H%M)
mkdir -p "$dest/families"
sqlite3 "$src/accounts.db" ".backup $dest/accounts.db"
for db in "$src"/families/*.db; do
  [ -e "$db" ] || continue
  sqlite3 "$db" ".backup $dest/families/$(basename "$db")"
done
find /var/lib/open-go-menu-backups -mindepth 1 -maxdepth 1 -type d -mtime +30 -exec rm -r {} +
```

```ini
# /etc/systemd/system/open-go-menu-backup.service
[Unit]
Description=Sauvegarde des bases OpenGoMenu

[Service]
Type=oneshot
DynamicUser=yes
User=open-go-menu
StateDirectory=open-go-menu open-go-menu-backups
ExecStart=/usr/local/bin/open-go-menu-backup
```

```ini
# /etc/systemd/system/open-go-menu-backup.timer
[Unit]
Description=Sauvegarde quotidienne des bases OpenGoMenu

[Timer]
OnCalendar=daily
Persistent=true

[Install]
WantedBy=timers.target
```

```bash
systemctl daemon-reload
systemctl enable --now open-go-menu-backup.timer
systemctl start open-go-menu-backup        # sauvegarde immédiate
ls -l /var/lib/open-go-menu-backups/
```

Copier ensuite ce répertoire hors du serveur avec l'outil habituel (restic, borg,
rsync…). Une sauvegarde restaure aussi les familles supprimées depuis par la purge
ou par elles-mêmes : la garder 30 jours au plus reste cohérent avec la promesse de
suppression.

Restauration complète :

```bash
systemctl stop open-go-menu
cd /var/lib/private/open-go-menu/data
rm -rf accounts.db* families
cp -r /chemin/sauvegarde/. .
chown -R "$(stat -c %u ..):$(stat -c %g ..)" . && chmod -R go= .
systemctl start open-go-menu
```

Une seule famille se restaure en remplaçant son fichier `families/<id>.db` (service
arrêté) ; son `<id>` est le nom du fichier, la correspondance avec l'identifiant
est dans `accounts.db` (`SELECT u.email, f.file FROM families f JOIN users u ON u.id = f.user_id`).

## 8. Mise à jour

```bash
systemctl start open-go-menu-backup        # sauvegarde avant mise à jour
install -m 0755 open-go-menu-v1.3.0-linux-amd64 /usr/local/bin/open-go-menu
systemctl restart open-go-menu
journalctl -u open-go-menu -n 20
```

Les migrations SQL embarquées s'appliquent au démarrage pour `accounts.db`, et à
l'ouverture de chaque famille pour sa base ; elles sont tracées dans
`schema_migrations`. Elles ne se défont pas : un **retour arrière** passe par l'ancien
binaire **et** la sauvegarde prise avant la mise à jour. Un ancien binaire démarré
sur une base migrée plus récente ne connaît pas les nouvelles tables.

### Depuis une installation mono-foyer

Les versions sans comptes avaient une seule base (`database.dsn`, par défaut
`open-go-menu.db`). Elle devient une famille ; la base d'origine n'est que lue :

```bash
systemctl stop open-go-menu
install -m 0755 open-go-menu-vX.Y.Z-linux-amd64 /usr/local/bin/open-go-menu
# config.yaml : remplacer la section database par families (et auth), cf. §2
ogm create-family -from /var/lib/open-go-menu/open-go-menu.db dupont
systemctl start open-go-menu
```

La commande affiche le mot de passe de la famille. Le basic auth de nginx n'a plus
lieu d'être : retirer `auth_basic` du site (cf. `deploy/nginx.conf`). Une fois la
famille vérifiée, l'ancienne base peut être archivée.

## 9. Dépannage

| Symptôme | Cause probable |
|---|---|
| Tout POST répond `403` | Le cookie CSRF n'est pas renvoyé. Site servi en HTTP alors que `X-Forwarded-Proto` vaut `https` (cookie `Secure`), ou cookie bloqué par le navigateur. |
| `429` pour tout le monde en même temps | `trusted_proxies` ne contient pas l'adresse d'où nginx se connecte : tout le monde est compté comme l'IP de nginx. |
| Connexion refusée en `429` pour un identifiant | Plus de 5 échecs de connexion en 15 minutes sur cet identifiant (SEC-07) : attendre, ou vérifier qu'un tiers ne tente pas de deviner le mot de passe. |
| Déconnecté à chaque visite | Le cookie `session` n'est pas renvoyé : site en HTTP avec `auth.cookie_secure: on`, ou `X-Forwarded-Proto` mal transmis. |
| La semaine courante ou « Aujourd'hui » est décalée d'un jour | Fuseau du processus : vérifier `TZ` dans l'unité (`systemctl show open-go-menu -p Environment`). |
| `bind: address already in use` | Un autre processus écoute sur le port : `ss -ltnp 'sport = :8080'`. |
| `attempt to write a readonly database` | Une base a été copiée ou créée à la main (commande lancée en root) avec un autre propriétaire, ou `families.data_dir` pointe hors de `StateDirectory` (le reste du système est en lecture seule). |
| `operation not permitted` dans les journaux | Un appel système filtré par l'unité. Relancer avec `SystemCallFilter=` vide dans une surcharge pour confirmer, puis signaler. |
| Interface en anglais | `i18n.default_language` non réglé : le défaut du socle est `en`. |
