# OpenGoMenu — cahier des charges

Application web de planification de repas : à partir de listes pondérées (plats
complets, féculents, viandes/poissons, légumes), elle tire au sort les repas d'une
semaine en tenant compte de la saison.

Priorités : **O** obligatoire (v1), **I** important, **S** souhaité, **V2** prévu
pour une version ultérieure.

## 1. Contexte et périmètre

Un foyer veut arrêter de se demander « qu'est-ce qu'on mange ? ». Il entretient une
liste de menus (au départ un simple nom : « Gratin dauphinois », « Salade niçoise »),
indique pour chacun s'il l'aime plus ou moins (poids général) et à quelle saison il
convient plus ou moins (bonus ou malus par saison), puis laisse l'application proposer la semaine.

Un repas est soit un **plat complet** (« Lasagnes »), soit un **repas composé**
d'un féculent, d'une viande ou d'un poisson et d'un légume (« Riz, saumon, haricots
verts »), chacun tiré dans sa propre liste.

Les plats complets ont leurs ingrédients (EF-19) et l'application dresse la liste
de courses d'une période (§3.4).

L'application est **multi-familles** (§3.5) : chaque famille a un identifiant et un
mot de passe partagés par ses membres, et ses données dans **sa propre base**.
Aucune donnée personnelle n'est demandée : ni e-mail, ni nom, donc pas de
récupération de mot de passe hors de l'administrateur du serveur. On reste connecté
jusqu'à « Se déconnecter » ; une famille sans visite pendant 12 mois est supprimée.

Hors périmètre : plusieurs identifiants par famille, partage entre familles,
récupération de mot de passe par e-mail, double authentification, API publique.

## 2. Vocabulaire

| Terme | Sens |
|---|---|
| Élément | Une entrée d'une des quatre listes, avec sa **sorte** (`Kind`) : plat complet (`dish`), féculent (`starch`), viande ou poisson (`protein`), légume (`vegetable`). Entité `Item`. |
| Plat complet | Élément qui fait à lui seul un repas (« Lasagnes »). Appelé « menu » dans la v1 initiale. |
| Composant | Féculent, viande/poisson ou légume. |
| Repas | Un créneau d'une journée : midi (`lunch`) ou soir (`dinner`). |
| Assiette | Le contenu d'un repas planifié : un plat complet, ou jusqu'à un composant de chaque sorte. Type `Plate`. |
| Semaine | 7 jours consécutifs commençant au jour de début de semaine paramétré. |
| Planning | Les assiettes affectées aux couples (jour, repas). |
| Poids général | Préférence globale pour un élément, entier de 0 à 10. |
| Bonus saisonnier | Adéquation d'un élément à une saison : entier de -10 à +10, un par saison, ajouté au poids général (malus quand il est négatif). 0 est neutre. |
| Part de repas composés | Probabilité (0–100 %) qu'un repas tiré soit composé plutôt qu'un plat complet. |
| Repas de la semaine | Un repas indépendamment de la date : « mercredi midi ». Type `Weekly`. |
| Repas fixe | Assiette servie chaque semaine sur un repas de la semaine, au lieu d'un tirage. |
| Article | Tout ce qui s'achète : ingrédient, composant planifié, ou article ajouté à la main (« Papier toilette »). Catalogue distinct des listes de composants. |
| Ingrédient | Article d'une recette de plat complet, avec une quantité en texte libre (« 200 g »). |
| Rayon | Rayon du magasin ; les rayons sont ordonnés comme le parcours dans le magasin. |
| Liste de courses | Les articles à acheter pour les repas d'une période, plus les ajouts à la main. Une seule liste à la fois. |
| Famille | Un compte : un identifiant, un mot de passe, et une base de données à elle seule. Toutes les données décrites ici appartiennent à une famille. |
| Identifiant | Le login d'une famille, partagé par ses membres : 3 à 40 caractères `a-z 0-9 . _ -`, insensible à la casse. |

## 3. Exigences fonctionnelles

### 3.1 Page d'accueil — la semaine

| Réf | Exigence | Prio |
|---|---|---|
| EF-01 | La page d'accueil affiche la semaine **courante** : 7 jours à partir du jour de début de semaine, chaque jour présentant ses repas actifs (midi, soir). | O |
| EF-02 | Le jour de début de semaine (lundi… dimanche) est **paramétrable directement sur la page d'accueil**. Le choix est persistant. Défaut : lundi. | O |
| EF-03 | Si aucun repas de la semaine affichée n'a de menu, la page affiche un bouton **« Générer »** qui tire au sort toute la semaine. | O |
| EF-04 | Si la semaine est partiellement remplie (ex. après un changement de jour de début), un bouton **« Compléter »** remplit uniquement les repas vides. | I |
| EF-05 | Une semaine remplie propose **« Régénérer »** (re-tire tous les repas). | I |
| EF-06 | Chaque repas peut être **relancé** individuellement (nouveau tirage pour ce seul repas, qui peut passer de plat complet à composé et inversement), **choisi** manuellement (un plat complet, ou un féculent, une viande/poisson et un légume), ou **vidé**. | I |
| EF-09 | Dans un repas composé, chaque composant peut être **relancé seul** sans toucher aux deux autres. | I |
| EF-16 | Un repas peut être **déplacé** vers un autre créneau de la semaine ; si ce créneau est occupé, les deux repas sont **échangés**. Sur ordinateur par glisser-déposer, sur téléphone en touchant « Déplacer » puis la destination, sans JS par un menu « Déplacer vers… ». | I |
| EF-07 | Navigation semaine précédente / suivante / retour à la semaine courante, pour préparer la semaine à venir ou consulter l'historique. | I |
| EF-08 | Le jour courant est mis en évidence ; la saison de chaque jour est visible. | S |

### 3.2 Page de paramétrage

| Réf | Exigence | Prio |
|---|---|---|
Le paramétrage a un onglet par liste — **Plats complets**, **Féculents**, **Viandes
et poissons**, **Légumes** — et un onglet **Repas**.

| Réf | Exigence | Prio |
|---|---|---|
| EF-10 | Chaque onglet de liste affiche ses éléments avec, pour chacun, son nom, son poids général et ses 4 bonus saisonniers, **modifiables en place** et enregistrés en une fois. | O |
| EF-11 | Formulaire d'**ajout** d'un élément dans la liste de l'onglet : nom obligatoire, poids général pré-rempli à 5, bonus saisonniers à 0. | O |
| EF-12 | **Suppression** d'un élément. Elle est logique : il disparaît de la liste et des tirages, mais reste affiché dans les plannings passés. | I |
| EF-13 | Onglet Repas : choix des **repas planifiés jour par jour** (midi et/ou soir pour chaque jour de la semaine ; au moins un repas dans la semaine, un jour peut n'en avoir aucun). Défaut : midi et soir tous les jours. | I |
| EF-14 | Le nom d'un élément est unique (insensible à la casse et aux accents) parmi les éléments non supprimés **de la même liste**, 1 à 100 caractères. | O |
| EF-15 | Onglet Repas : **part de repas composés**, de 0 à 100 %. Défaut : 50 %. | O |
| EF-18 | Onglet Repas : chaque repas de la semaine peut recevoir un **repas fixe** (un plat complet, ou des composants), choisi sur une page dédiée ; on peut le retirer pour revenir au tirage. | I |
| EF-19 | Chaque plat complet a une **fiche d'ingrédients** : ajout (nom, quantité libre ≤ 50 caractères, rayon facultatif), modification groupée des quantités, retrait, ordre de saisie. Les articles et les féculents, viandes/poissons et légumes vivants sont proposés à la saisie ; un ingrédient saisi n'est jamais ajouté à ces listes. Un ingrédient est unique (insensible à la casse) dans une recette ; l'article est partagé entre recettes et reste au catalogue quand plus aucune recette ne l'utilise. | I |
| EF-20 | Onglet **Rayons** : ajout, renommage groupé, suppression (ses articles restent sans rayon), ordre modifiable par boutons ↑/↓, et sur ordinateur par glisser-déposer. | I |
| EF-32 | **Suggestions de saisie** contre les doublons : en tapant le nom d'un élément ou d'un article, les entrées existantes au nom proche (sans tenir compte de la casse ni des accents) sont proposées, et en choisir une mène à elle ; un nom quasi identique (accents, casse, pluriel) est signalé. Pour un ingrédient ou un ajout à la liste, les articles sont proposés de même, et un nom saisi sans accents reprend l'article existant. | I |
| EF-21 | Onglet **Articles** : nom et rayon de chaque article, modifiables en place et enregistrés en une fois ; ajout ; suppression, refusée pour un ingrédient d'une recette. | I |

### 3.4 Liste de courses

| Réf | Exigence | Prio |
|---|---|---|
| EF-22 | Une liste de courses couvre une **période** (1 à 31 jours). Sans liste, ou par « Nouvelle liste », la page propose la **dernière semaine entièrement prévue** à partir de la semaine en cours (tous ses repas planifiés remplis) ; sinon, ou au choix, on saisit les dates de début et de fin. Une nouvelle liste **remplace** la précédente, ses cases cochées et ses ajouts : les courses sont ponctuelles. | I |
| EF-23 | On peut **ajouter à la main** un article à la liste (nom avec suggestions, quantité libre), puis le retirer. Aucun ajout n'est permanent ni récurrent. | I |
| EF-24 | Chaque ligne montre l'article, et pour chaque besoin la quantité éventuelle, le plat dont il est l'ingrédient, le jour et le repas (ou « Ajouté à la main »). Une **case** met l'article dans le panier : la ligne passe **aussitôt** en fin de liste, atténuée, sous « Dans le panier » (et retourne dans son rayon si on la décoche), sans attendre le serveur ; avec ou sans réseau, la coche est gardée sur l'appareil et envoyée dès que possible. Un article sans rayon peut être rangé depuis la liste. Les plats complets planifiés sans ingrédients sont signalés en tête de liste. | I |
| EF-33 | La liste est **partagée en direct** : quand plusieurs appareils de la famille l'affichent, une case cochée ou décochée sur l'un déplace aussitôt la ligne sur les autres, sans rechargement ; tout autre changement de la famille (ajout, rangement, planning, nouvelle liste…) recharge la liste des autres appareils, une fois la saisie en cours terminée. Un appareil qui a manqué des changements (réseau coupé, copie hors ligne) se remet à jour en se reconnectant. Sans JavaScript, la liste reste à jour à chaque affichage. | I |

### 3.5 Comptes et familles

| Réf | Exigence | Prio |
|---|---|---|
| EF-25 | **Connexion** par identifiant et mot de passe (`/login`). Toutes les pages, sauf connexion, inscription, manifeste et ressources statiques, exigent une famille connectée ; sinon redirection vers `/login`, qui ramène ensuite à la page demandée. Un échec ne dit pas si l'identifiant existe. | O |
| EF-26 | **Inscription** (`/signup`) selon le réglage `families.signup` : `open` (libre), `invite` (avec un code d'invitation à usage unique, défaut), `closed` (fermée : seul l'administrateur crée des familles). Identifiant, mot de passe (au moins 10 caractères) et confirmation ; la page prévient qu'un mot de passe oublié n'est pas récupérable et rappelle la suppression après inactivité. La famille créée est connectée aussitôt, sa base créée. | O |
| EF-27 | **Connexion permanente** : la session glisse à chaque visite et dure 400 jours (plafond des navigateurs) ; elle ne prend fin que par « Se déconnecter » (cet appareil seulement) ou par la déconnexion de tous les appareils. La déconnexion efface aussi les pages gardées hors ligne (EI-06). | O |
| EF-28 | Page **Compte** : identifiant et date de création, se déconnecter, changer le mot de passe (l'actuel est demandé ; les autres appareils sont déconnectés, celui-ci reste connecté), déconnecter tous les appareils. | O |
| EF-29 | **Suppression de la famille** depuis le Compte, confirmée par le mot de passe et une case : identifiant, sessions et base sont supprimés définitivement. | I |
| EF-30 | **Purge** : la commande `open-go-menu purge` supprime les familles sans aucune visite depuis `families.inactive_months` mois (12 par défaut), les sessions expirées et les codes d'invitation expirés ; un timer systemd la lance chaque nuit. Une visite est toute page vue par la famille connectée, notée au plus une fois par heure. `-dry-run` liste sans supprimer. | I |
| EF-31 | **Administration** en ligne de commande : `create-family [-from base.db] <identifiant>` (affiche le mot de passe généré ; `-from` importe la base d'une installation mono-foyer, sans la modifier), `reset-password <identifiant>` (nouveau mot de passe affiché, tous les appareils déconnectés), `invite [-days 30]` (code à usage unique), `families` (liste, dernières visites et famille qui a invité). | I |
| EF-34 | **Invitation par les familles** (réglage `families.family_invites`, avec `signup: invite`, désactivé par défaut) : depuis son Compte, toute famille, invitée ou non, génère un code à usage unique valable `family_invite_days` jours (7 par défaut), dans la limite de `family_invite_max` codes en attente (3 par défaut). Le code et son lien d'inscription (`/signup?invite=…`, qui préremplit le code) ne s'affichent qu'une fois, avec Copier et Partager quand l'appareil le permet. Le Compte liste les codes en attente (dates de création et d'expiration, Révoquer) et les familles inscrites avec un code de la famille. Les codes d'une famille supprimée disparaissent ; les familles qu'elle a invitées restent. `families` indique qui a invité chaque famille ; `purge` supprime les codes expirés. | I |

### 3.3 Règles de tirage

| Réf | Règle |
|---|---|
| RG-01 | Poids effectif d'un élément pour un jour J = `poids général + bonus de la saison de J`, ramené à 0 s'il est négatif. Le poids général prime : à 0, aucun bonus ne fait revenir l'élément ; un malus de -10 l'écarte de la saison quel que soit son poids général. Un poids effectif nul exclut l'élément ce jour-là. |
| RG-02 | Chaque élément est tiré dans sa liste avec une probabilité proportionnelle à son poids effectif. |
| RG-03 | Un même élément n'apparaît pas deux fois dans la semaine **tant qu'il reste des candidats dans sa liste** ; les repas déjà remplis de la semaine comptent. Chaque liste est suivie séparément. |
| RG-04 | Si une liste est épuisée, le tirage autorise la répétition, en évitant si possible l'élément du repas immédiatement précédent. |
| RG-05 | Si rien n'a de poids effectif positif, le repas reste vide et l'utilisateur en est informé. |
| RG-08 | Pour chaque repas, le tirage choisit d'abord sa forme : composé avec la probabilité « part de repas composés », plat complet sinon. Si une forme n'a aucun candidat ce jour-là, l'autre est retenue. |
| RG-09 | Un repas composé prend un élément de chaque sorte de composant qui a des candidats ce jour-là ; une sorte sans candidat est omise (ex. pas de féculent défini → viande/poisson + légume). |
| RG-06 | Saisons météorologiques (hémisphère nord) : hiver = déc.–fév., printemps = mars–mai, été = juin–août, automne = sept.–nov. La saison est évaluée **jour par jour** (une semaine à cheval sur deux saisons est gérée). |
| RG-07 | Le planning est stocké par date et par repas, pas par semaine : changer le jour de début de semaine ne perd aucune donnée, il décale seulement la fenêtre affichée. |
| RG-11 | Liste de courses : les besoins viennent des ingrédients des plats complets et des composants planifiés sur la période (repas planifiés seulement, EF-13), puis des ajouts à la main. Un composant est acheté comme l'article de son nom, relié une fois pour toutes (renommer le composant garde l'article et son rayon). Un même article n'a qu'**une ligne**, les quantités en texte libre ne sont pas additionnées. Ordre : rayons dans leur ordre, articles sans rayon à la fin, noms par ordre alphabétique dans un rayon. |
| RG-12 | Cocher une ligne retient les besoins qu'elle couvrait. Si un **nouveau besoin** apparaît ensuite (repas relancé, ajouté, déplacé…), la ligne se décoche ; un besoin disparu la laisse cochée. |
| RG-10 | « Générer », « Compléter » et « Régénérer » servent le repas fixe sur les repas visés qui en ont un, avant de tirer les autres ; ses éléments comptent comme déjà servis dans la semaine (RG-03). Les poids ne s'appliquent pas au repas fixe. Relancer un repas fixe le tire au sort ; « Choisir » le remplace pour cette semaine seulement. Un élément supprimé quitte le repas fixe. |

## 4. Exigences d'interface

| Réf | Exigence |
|---|---|
| EI-01 | Interface responsive, utilisable sur téléphone (cas d'usage principal : en cuisine, au marché). |
| EI-02 | Fonctionne **sans JavaScript** ; le JS n'apporte que du confort : envoi automatique du sélecteur de jour de début, actions sur la semaine sans rechargement ni déplacement de la page, glisser-déposer et déplacement par toucher des repas, glisser-déposer des rayons, cases de la liste instantanées et hors ligne, suggestions de saisie. |
| EI-03 | Accessibilité : libellés de formulaires, focus clavier visible, contrastes AA, `prefers-reduced-motion` respecté. |
| EI-04 | Toute action mutante suit le schéma POST → redirection → GET ; le résultat est confirmé par un message flash. |
| EI-05 | Les erreurs de saisie réaffichent le formulaire avec les valeurs saisies et un message par champ fautif. |
| EI-06 | L'application est **installable** sur téléphone et ordinateur (application web progressive) : manifeste traduit, icônes, raccourcis Semaine et Courses. Un service worker **hors ligne d'abord** : fichiers statiques servis du cache ; pages du réseau, mais sa copie enregistrée s'affiche aussitôt sans réseau ou quand il tarde (2,5 s), avec un bandeau qui le signale et propose la version à jour quand elle arrive ; la semaine et la liste de courses sont gardées même non visitées. Hors ligne, les formulaires ne sont pas envoyés, sauf les cases de la liste de courses (EF-24). Installation possible en HTTPS (ou sur `localhost`). |

## 5. Exigences non fonctionnelles

| Réf | Exigence |
|---|---|
| ENF-01 | Go 1.26+, **bibliothèque standard** pour le HTTP (`http.ServeMux`), `html/template`. Aucun framework web, CSS ou JS. |
| ENF-02 | Socle **`leblanc.io/open-go-base`** : `appconf` (configuration, `--help`, `--version`), `logx`, `ratelimit`, `i18n` (`NewFS`), `csrfx`, `authx` (mots de passe bcrypt, sessions glissantes, connexion, avec son stockage `authx/sqlstore`). Ne pas ré-implémenter ces briques. |
| ENF-03 | Binaire **autonome** : gabarits, CSS/JS et traductions embarqués (`go:embed`). |
| ENF-04 | Stockage **SQLite** via `modernc.org/sqlite` (pur Go, `CGO_ENABLED=0`) : une base des comptes (`accounts.db`) et **une base par famille** (`families/<id>.db`, `<id>` aléatoire), sous `families.data_dir`. Aucune requête ne filtre par famille : l'isolement vient du fichier. Migrations SQL embarquées et idempotentes, jouées au démarrage pour les comptes et à l'ouverture de chaque famille. Les bases inutilisées depuis 15 minutes sont refermées. |
| ENF-05 | **Internationalisation dès la v1** : aucune chaîne en dur dans les gabarits ou le code. Clés anglaises `écran.élément` (`home.generate`, `settings.add_dish`) ; segments transverses sous leurs propres racines (`weekday.*`, `month.*`, `season.*`, `meal.*`, `kind.*`, `validation.*`, `flash.*`, `date.*`). Langues : français (défaut) et anglais, catalogues alignés. Le JS reçoit ses libellés via des attributs `data-`. |
| ENF-06 | Langue choisie par `?lang=` (mémorisée en cookie), sinon `Accept-Language`, sinon langue par défaut configurée (`i18n.default_language`, `fr` dans `config.yaml.dist` ; le défaut d'`appconf` est `en`). |
| ENF-07 | CSS en **nesting natif**, sans préprocesseur. |
| ENF-08 | Configuration YAML ou variables d'environnement préfixées `OGM_` ; version injectée par `-ldflags "-X main.version=…"`. |
| ENF-09 | Tests unitaires sur la logique de tirage (saisons, bornes de semaine, pondération, non-répétition) et sur le stockage. |

## 6. Sécurité

| Réf | Exigence |
|---|---|
| SEC-01 | Protection CSRF sur tous les formulaires (`csrfx`, double-submit cookie). |
| SEC-02 | En-têtes : `Content-Security-Policy` stricte (`default-src 'self'`, pas d'inline), `X-Content-Type-Options`, `X-Frame-Options: DENY`, `Referrer-Policy`. |
| SEC-03 | Limitation de débit par IP (`ratelimit`), IP réelle résolue uniquement via les proxys de confiance. |
| SEC-04 | Requêtes SQL paramétrées exclusivement ; échappement HTML par `html/template`. |
| SEC-05 | Validation serveur de toutes les entrées (bornes des poids et des bonus, longueur des noms, dates, repas connus). |
| SEC-06 | **Isolement des familles** : la famille d'une requête vient uniquement de sa session, jamais de l'URL ni d'un champ ; elle désigne la seule base ouverte pour la requête. Le nom d'un fichier de famille est aléatoire (32 caractères hexadécimaux), vérifié avant toute ouverture. |
| SEC-07 | **Mots de passe** hachés par `authx` (bcrypt, coût `auth.bcrypt_cost`, pré-hachage SHA-256), au moins 10 caractères. Contre la force brute, en plus de la limite par IP : 5 échecs en 15 minutes sur un identifiant le bloquent jusqu'à la fin de la fenêtre (429). Le temps de réponse d'un identifiant inconnu est égalisé (`authx`). |
| SEC-08 | **Session** : jeton aléatoire de 256 bits en base, cookie `HttpOnly`, `SameSite=Lax`, `Secure` en HTTPS (`auth.cookie_secure`). Déconnexion côté serveur ; `Clear-Site-Data: "cache", "storage"` à la déconnexion et à la suppression, pour qu'une autre famille du même appareil ne voie pas les pages gardées hors ligne. Codes d'invitation conservés sous forme de SHA-256 seulement, à usage unique, expirant. |

## 7. Évolutions possibles

- Articles récurrents (ajoutés à chaque nouvelle liste).
- Pistes : nombre de convives, pénalité des menus servis la semaine précédente,
  contraintes (« poisson le vendredi »).

## 8. Architecture

```
main.go                      câblage, go:embed, middlewares, arrêt propre
cli.go                       commandes d'administration (EF-31, EF-30)
internal/config              Config = fragments appconf (Web, Logging, I18n, Auth) + Families
internal/accounts            base des comptes : familles, invitations (+ users, sessions d'authx)
internal/families            registre des bases de familles : ouverture, fermeture, suppression
internal/planner             logique pure : saisons, bornes de semaine, tirage pondéré
internal/shopping            logique pure : regroupement de la liste de courses
internal/store               SQLite : migrations, éléments, planning, réglages,
                             articles, rayons, liste en cours
internal/web                 handlers, rendu des gabarits, vues, flash, langue
templates/  static/  locales/
```

Pile de middlewares (extérieur → intérieur) : en-têtes de sécurité → langue →
`/static/`, `/sw.js` hors limiteur | limiteur → CSRF → session (`authx.LoadSession`)
→ routeur. Chaque page d'une famille passe par `RequireAuth` puis par l'ouverture
de sa base (SEC-06).

Routes :

| Méthode | Chemin | Rôle |
|---|---|---|
| GET, POST | `/login` | Connexion (`email` = identifiant, `password`, `next`) — public |
| GET, POST | `/signup` | Inscription (`login`, `password`, `password_confirm`, `invite`) — public |
| POST | `/logout` | Déconnexion de cet appareil |
| GET | `/account` | Compte de la famille |
| POST | `/account/password` | Changement de mot de passe (`current`, `password`, `password_confirm`) |
| POST | `/account/logout-all` | Déconnexion de tous les appareils |
| POST | `/account/delete` | Suppression de la famille (`password`, `confirm=yes`) |
| POST | `/account/invites` | Génère un code d'invitation de la famille, affiché une fois sur le Compte (EF-34) |
| POST | `/account/invites/{id}/revoke` | Révoque un code en attente de la famille |
| GET | `/` | Semaine (`?week=AAAA-MM-JJ` : n'importe quel jour de la semaine voulue) |
| GET | `/manifest.webmanifest` | Manifeste de l'application installable, dans la langue de la requête (EI-06) |
| GET | `/sw.js` | Service worker, servi à la racine pour couvrir tout le site (hors limiteur, `no-cache`) |
| POST | `/week-start` | Change le jour de début de semaine |
| POST | `/plan/generate` | Génère / complète / régénère (`mode=fill|all`) la semaine |
| POST | `/plan/{date}/{meal}` | Action sur un repas (`action=reroll|set|clear`) |
| POST | `/plan/{date}/{meal}` | `action=reroll&kind=starch|protein|vegetable` : relance d'un seul composant |
| POST | `/plan/{date}/{meal}` | `action=move&to=AAAA-MM-JJ/meal` : déplace ou échange le repas (EF-16) |
| GET | `/settings` | Paramétrage, onglet Plats complets |
| GET | `/settings/{list}` | Onglet d'une liste : `dishes`, `starches`, `proteins`, `vegetables` |
| POST | `/settings/{list}` | Ajout d'un élément à la liste |
| POST | `/settings/{list}/save` | Enregistrement groupé de la liste |
| POST | `/settings/items/{id}/delete` | Suppression logique d'un élément |
| GET, POST | `/settings/meals` | Onglet Repas : repas planifiés par jour (`slot=wednesday/lunch`, répété) et part de repas composés |
| GET, POST | `/settings/meals/{weekday}/{meal}` | Repas fixe d'un repas de la semaine (`action=set|clear`) |
| GET | `/settings/items/{id}` | Fiche des ingrédients d'un plat complet |
| POST | `/settings/items/{id}/ingredients` | Ajout d'un ingrédient (`name`) |
| POST | `/settings/items/{id}/ingredients/save` | Quantités des ingrédients (`quantity_<article>`) |
| POST | `/settings/items/{id}/ingredients/{article}/delete` | Retrait d'un ingrédient |
| GET, POST | `/settings/articles` | Onglet Articles ; ajout (`name`, `aisle`) |
| POST | `/settings/articles/save`, `/settings/articles/{id}/delete` | Enregistrement groupé, suppression |
| GET, POST | `/settings/aisles` | Onglet Rayons ; ajout |
| POST | `/settings/aisles/save`, `/settings/aisles/{id}/move`, `/settings/aisles/{id}/delete` | Renommage groupé, ordre (`dir=up|down`), suppression |
| POST | `/settings/aisles/order` | Ordre complet après un glisser-déposer (`id` répété, chaque rayon une fois) |
| GET | `/shopping` | Liste de courses en cours, ou choix de la période s'il n'y en a pas |
| GET | `/shopping/new` | Choix de la période d'une nouvelle liste |
| POST | `/shopping` | Crée la liste (`from`, `to`) |
| POST | `/shopping/extras` | Ajout à la main (`name`, `quantity`) |
| POST | `/shopping/articles/{id}` | `action=tick&needs=…`, `untick`, `remove` (ajout à la main), `aisle&aisle=…` |
| GET | `/shopping/events` | Flux d'événements (`text/event-stream`) des changements de la famille, pour la liste en direct (EF-33) ; `?since=` ou `Last-Event-ID` : dernier changement connu |

## 9. Modèle de données

Base des comptes, `accounts.db` :

```sql
users       (id, email, password_hash, role, is_active, must_change_pwd, created_at,
             last_login)                -- table d'authx ; email = identifiant de la famille
sessions    (token, user_id → users.id, purpose, expires_at, created_at, user_agent, ip)
families    (user_id PRIMARY KEY → users.id, file UNIQUE, created_at, last_active_at)
invites     (code_hash PRIMARY KEY, created_at, expires_at, used_at,
             created_by → users.id ON DELETE CASCADE,   -- NULL : code de l'administrateur
             used_by → users.id ON DELETE SET NULL)     -- famille inscrite avec le code (EF-34)
```

Base de chaque famille, `families/<file>.db` :

```sql
items       (id, kind, name, weight, weight_spring, weight_summer, weight_autumn,
             weight_winter, created_at, deleted_at)
             -- weight 0..10 ; weight_<saison> : bonus -10..+10 (RG-01)
             -- kind : dish | starch | protein | vegetable
             -- nom unique par (kind, name) parmi les éléments non supprimés
plan_items  (day TEXT 'AAAA-MM-JJ', meal TEXT, kind TEXT, item_id → items.id,
             PRIMARY KEY (day, meal, kind))
             -- une ligne kind=dish pour un plat complet, sinon une ligne par composant
settings    (key PRIMARY KEY, value)    -- week_start, composed_share
weekly_meals (weekday INTEGER 0 = dimanche, meal, PRIMARY KEY (weekday, meal))
             -- une ligne par repas planifié (EF-13)
fixed_items (weekday, meal, kind, item_id → items.id, PRIMARY KEY (weekday, meal, kind))
             -- repas fixes, sur le modèle de plan_items (EF-18)
articles    (id, name, aisle_id → aisles.id)   -- nom unique, COLLATE NOCASE
aisles      (id, name, position)        -- nom unique, COLLATE NOCASE
dish_ingredients (item_id → items.id, article_id → articles.id, position, quantity,
             PRIMARY KEY (item_id, article_id))
component_articles (item_id PRIMARY KEY → items.id, article_id → articles.id)
shopping_extras (article_id PRIMARY KEY, quantity)      -- ajouts à la main
shopping_checks (article_id PRIMARY KEY, needs)         -- cases cochées (RG-12)
             -- période de la liste en cours : settings shopping_from, shopping_to
```

La migration `002` renomme `dishes` en `items` (sorte `dish` pour l'existant) et
convertit `plan_entries` en `plan_items` sans perte. La migration `003` crée les
tables d'ingrédients ; la `004` crée `weekly_meals` à partir de l'ancien réglage
`meals` (appliqué à chaque jour), puis le supprime, et crée `fixed_items`. La `005`
renomme `ingredients` en `articles` (et `ingredient_id` en `article_id`), ajoute
rayons, quantités et tables de la liste de courses, sans perte. La `006` convertit
les poids saisonniers (0 à 10, multipliés) en bonus (-10 à +10, ajoutés) : 5
devient 0, chaque cran est conservé (`poids - 5`), et 0 — jamais dans la saison —
devient -10.

## 10. Recette

| Réf | Scénario |
|---|---|
| TR-01 | Base vide → l'accueil invite à ajouter des menus ; « Générer » informe qu'aucun menu n'est disponible. |
| TR-02 | 20 menus, semaine vide → « Générer » remplit 14 repas sans doublon. |
| TR-03 | Un menu au malus d'été -10 n'est jamais tiré pour un jour de juillet ; un menu au poids général 0 ne l'est jamais, même avec un bonus de saison. |
| TR-04 | Changer le jour de début de lundi à samedi conserve les menus déjà tirés aux mêmes dates ; « Compléter » remplit les seuls trous. |
| TR-05 | Relancer un repas change son menu sans toucher aux autres. |
| TR-06 | Supprimer un menu planifié : il disparaît du paramétrage, reste visible dans le planning. |
| TR-07 | `?lang=en` bascule toute l'interface en anglais, y compris jours, mois, saisons et messages. |
| TR-08 | Un POST sans jeton CSRF est rejeté en 403. |
| TR-09 | Part de repas composés à 100 %, 3 féculents, 3 viandes/poissons, 3 légumes → chaque repas a un composant de chaque sorte. |
| TR-10 | Aucun plat complet, part à 0 % → les repas sont quand même composés (RG-08). |
| TR-11 | Relancer le légume d'un repas composé ne change ni le féculent ni la viande/poisson. |
| TR-12 | « Riz » peut exister à la fois comme féculent et comme plat complet ; deux féculents « Riz » sont refusés. |
| TR-13 | Soir tous les jours, midi le mercredi seulement → « Générer » remplit 8 repas et la semaine est complète. |
| TR-14 | Mercredi midi fixé sur « Pizza » → chaque génération sert Pizza le mercredi midi, jamais ailleurs dans la semaine ; « Relancer » sur ce repas en tire un autre. |
| TR-15 | Ajouter « Bœuf » aux ingrédients des lasagnes alors que « Bœuf » est une viande : il est proposé à la saisie ; ajouter « Béchamel » ne crée aucun élément dans les listes. |
| TR-16 | Semaine suivante entièrement générée → la liste de courses la propose ; des dates inversées ou une période de plus de 31 jours sont refusées. |
| TR-17 | Bœuf viande le lundi midi et ingrédient des lasagnes le mardi soir → une seule ligne « Bœuf », sous le rayon Boucherie, avec les deux repas et la quantité de la recette. |
| TR-18 | Cocher « Bœuf », puis planifier du bœuf le vendredi → la ligne est de nouveau à acheter. |
| TR-19 | Ajouter « Papier toilette » à la main, créer une nouvelle liste → il n'y est plus. |
| TR-20 | Sans session, toute page de la famille redirige vers `/login` ; après connexion, on revient à la page demandée. |
| TR-21 | Une famille B ne voit ni ne peut supprimer un élément de la famille A, même en connaissant son identifiant numérique. |
| TR-22 | Six connexions ratées sur un identifiant → la sixième, même avec le bon mot de passe, répond 429 ; elle passe 15 minutes plus tard. |
| TR-31 | Avec `family_invites: true` : générer un code depuis le Compte l'affiche une fois avec son lien ; au-delà de `family_invite_max` codes en attente, le bouton disparaît. Le lien préremplit le code ; la famille inscrite apparaît dans le Compte de celle qui l'a invitée et peut inviter à son tour. Révoquer un code le rend inutilisable ; une famille ne peut pas révoquer celui d'une autre (404). Réglage désactivé : ni section ni route. |
| TR-23 | Inscription en mode `invite` : sans code, ou avec un code déjà utilisé ou expiré, refusée ; avec un code valide (saisi en minuscules), la famille est créée et connectée. En mode `closed`, `/signup` répond 404. |
| TR-24 | Changer le mot de passe déconnecte l'autre appareil connecté, pas celui-ci ; « Déconnecter tous les appareils » les déconnecte tous. |
| TR-25 | Supprimer la famille (mot de passe et case cochée) efface sa base ; l'identifiant ne se connecte plus et peut être repris. |
| TR-26 | `purge` sur une famille vue il y a 13 mois et une autre vue hier : seule la première est supprimée ; `-dry-run` ne supprime rien. |
| TR-28 | Cocher une ligne de la liste la fait passer immédiatement dans « Dans le panier » ; hors ligne, cocher deux lignes et recharger : elles restent cochées ; au retour du réseau, le serveur les a. |
| TR-30 | Deux appareils de la même famille sur la liste : cocher une ligne sur l'un la fait passer dans « Dans le panier » sur l'autre en moins d'une seconde ; un ajout à la main sur l'un apparaît sur l'autre. Une autre famille ne voit rien passer. |
| TR-29 | « Pâtes » existe : taper « pates » dans l'ajout de féculent la propose et signale un nom proche ; l'ajouter est refusé. Ajouter « creme fraiche » à la liste quand « Crème fraîche » existe reprend cet article. |
| TR-27 | `create-family -from` sur la base d'une installation mono-foyer : la famille retrouve ses éléments ; la base d'origine n'est pas modifiée. |
