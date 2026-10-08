# Déploiement en production

OpenGoMenu est un binaire Go autonome : gabarits, CSS/JS, traductions et migrations
SQL sont embarqués. En production, il tourne comme service **systemd** et écoute en
local ; **nginx** se place devant pour TLS et l'authentification. L'application n'a
pas d'authentification à elle (cahier des charges §1).

```
navigateur ──HTTPS──▶ nginx ──HTTP──▶ open-go-menu ──▶ SQLite
                      TLS, basic auth   127.0.0.1:8080   /var/lib/open-go-menu/
                      X-Forwarded-*     (systemd)
```

Les fichiers prêts à l'emploi sont dans [`deploy/`](../deploy) :

| Fichier | Destination |
|---|---|
| `deploy/open-go-menu.service` | `/etc/systemd/system/open-go-menu.service` |
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

database:
  driver: sqlite
  dsn: file:/var/lib/open-go-menu/open-go-menu.db
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
| `database.driver` | `OGM_DB_DRIVER` | `sqlite` | Seul `sqlite` est accepté |
| `database.dsn` | `OGM_DB_DSN` | `file:open-go-menu.db` | Base SQLite ; un chemin relatif part du répertoire courant |

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

- **`DynamicUser=yes`** : un utilisateur système éphémère, sans compte à créer.
- **`StateDirectory=open-go-menu`** : systemd crée `/var/lib/open-go-menu` (en
  réalité `/var/lib/private/open-go-menu`, lien symbolique), le donne à cet
  utilisateur et en fait le seul endroit inscriptible. C'est aussi le répertoire
  courant : la base et ses fichiers `-wal` / `-shm` y vivent.
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

## 4. nginx

### Authentification

Créer le fichier de mots de passe (une ligne par membre du foyer) :

```bash
# avec apache2-utils / httpd-tools
htpasswd -c -B /etc/nginx/open-go-menu.htpasswd alice
htpasswd -B /etc/nginx/open-go-menu.htpasswd bob

# ou sans paquet supplémentaire
printf 'alice:%s\n' "$(openssl passwd -apr1)" >> /etc/nginx/open-go-menu.htpasswd

chown root:www-data /etc/nginx/open-go-menu.htpasswd   # nginx sur Debian/Ubuntu
chmod 0640 /etc/nginx/open-go-menu.htpasswd
```

Le basic auth peut être remplacé par un SSO (`auth_request` vers oauth2-proxy,
Authelia…) : l'application ne voit de toute façon que des requêtes déjà
authentifiées.

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
| `X-Forwarded-For` / `X-Real-IP` | La limite de débit compte par IP cliente. Ces en-têtes ne sont crus que si nginx se connecte depuis une adresse de `trusted_proxies`. Sans ça, tout le foyer partage le quota de l'IP de nginx. |
| `X-Forwarded-Proto $scheme` | Le cookie CSRF reçoit l'attribut `Secure` quand la requête d'origine est en HTTPS. |
| Pas de `add_header` CSP, `X-Frame-Options`… | L'application les envoie déjà ; des doublons divergents seraient combinés par le navigateur (une CSP en double ne peut que restreindre davantage). HSTS, lié à TLS, est ajouté par nginx. |
| `client_max_body_size 64k` | Les formulaires font quelques centaines d'octets. |
| `location /static/` | Les ressources statiques échappent déjà à la limite de débit côté application ; nginx ajoute un cache d'une heure. Elles changent avec le binaire, d'où un délai court. |

## 5. Vérifier le déploiement

```bash
H=menu.mondomaine.fr

curl -sI http://$H/ | head -1                          # 301 vers https
curl -s -o /dev/null -w '%{http_code}\n' https://$H/   # 401 sans identifiants
curl -s -u alice -D - -o /dev/null https://$H/ \
  | grep -iE '^(HTTP|strict-transport|content-security|set-cookie|x-ratelimit)'
```

Attendu avec identifiants : `200`, HSTS, la CSP de l'application, un cookie
`csrf_token` avec `Secure; HttpOnly; SameSite=Lax`, et `X-RateLimit-Remaining`
qui décroît.

## 6. Sauvegarde et restauration

La base est en mode WAL : copier le seul fichier `.db` pendant que le service tourne
peut donner une sauvegarde incohérente. On passe par l'API de sauvegarde de SQLite
(`.backup`, sûre à chaud ; paquet `sqlite3`).

La sauvegarde tourne sous **le même utilisateur dynamique** que le service
(`User=open-go-menu`) : lancée en root, `sqlite3` pourrait créer des fichiers
`-wal` / `-shm` appartenant à root, que le service ne pourrait plus écrire.

```ini
# /etc/systemd/system/open-go-menu-backup.service
[Unit]
Description=Sauvegarde de la base OpenGoMenu

[Service]
Type=oneshot
DynamicUser=yes
User=open-go-menu
StateDirectory=open-go-menu open-go-menu-backups
ExecStart=/bin/sh -c 'sqlite3 /var/lib/open-go-menu/open-go-menu.db ".backup /var/lib/open-go-menu-backups/open-go-menu-$(date +%%F-%%H%%M).db"'
ExecStartPost=/usr/bin/find /var/lib/open-go-menu-backups -name '*.db' -mtime +30 -delete
```

```ini
# /etc/systemd/system/open-go-menu-backup.timer
[Unit]
Description=Sauvegarde quotidienne de la base OpenGoMenu

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
rsync…).

Restauration :

```bash
systemctl stop open-go-menu
cd /var/lib/private/open-go-menu
rm -f open-go-menu.db-wal open-go-menu.db-shm
install -m 0600 -o "$(stat -c %u .)" -g "$(stat -c %g .)" /chemin/sauvegarde.db open-go-menu.db
systemctl start open-go-menu
```

## 7. Mise à jour

```bash
systemctl start open-go-menu-backup        # sauvegarde avant mise à jour
install -m 0755 open-go-menu-v1.3.0-linux-amd64 /usr/local/bin/open-go-menu
systemctl restart open-go-menu
journalctl -u open-go-menu -n 20
```

Les migrations SQL embarquées s'appliquent au démarrage et sont tracées dans
`schema_migrations`. Elles ne se défont pas : un **retour arrière** passe par l'ancien
binaire **et** la sauvegarde prise avant la mise à jour. Un ancien binaire démarré
sur une base migrée plus récente ne connaît pas les nouvelles tables.

## 8. Dépannage

| Symptôme | Cause probable |
|---|---|
| Tout POST répond `403` | Le cookie CSRF n'est pas renvoyé. Site servi en HTTP alors que `X-Forwarded-Proto` vaut `https` (cookie `Secure`), ou cookie bloqué par le navigateur. |
| `429` pour tout le foyer en même temps | `trusted_proxies` ne contient pas l'adresse d'où nginx se connecte : tout le monde est compté comme l'IP de nginx. |
| La semaine courante ou « Aujourd'hui » est décalée d'un jour | Fuseau du processus : vérifier `TZ` dans l'unité (`systemctl show open-go-menu -p Environment`). |
| `bind: address already in use` | Un autre processus écoute sur le port : `ss -ltnp 'sport = :8080'`. |
| `attempt to write a readonly database` | La base a été copiée à la main avec un autre propriétaire, ou le DSN pointe hors de `StateDirectory` (le reste du système est en lecture seule). |
| `operation not permitted` dans les journaux | Un appel système filtré par l'unité. Relancer avec `SystemCallFilter=` vide dans une surcharge pour confirmer, puis signaler. |
| Interface en anglais | `i18n.default_language` non réglé : le défaut du socle est `en`. |
