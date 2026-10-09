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
convient (poids par saison), puis laisse l'application proposer la semaine.

Un repas est soit un **plat complet** (« Lasagnes »), soit un **repas composé**
d'un féculent, d'une viande ou d'un poisson et d'un légume (« Riz, saumon, haricots
verts »), chacun tiré dans sa propre liste.

Hors périmètre : comptes utilisateurs, partage multi-foyers, API publique. Les
plats complets ont leurs ingrédients (EF-19) et l'application dresse la liste de
courses d'une période (§3.4).

L'application est mono-foyer. Elle n'embarque **pas d'authentification** : en
déploiement exposé, elle se place derrière un reverse proxy qui authentifie (basic
auth, SSO…). C'est documenté dans le README.

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
| Poids saisonnier | Adéquation d'un élément à une saison, entier de 0 à 10, un par saison. |
| Part de repas composés | Probabilité (0–100 %) qu'un repas tiré soit composé plutôt qu'un plat complet. |
| Repas de la semaine | Un repas indépendamment de la date : « mercredi midi ». Type `Weekly`. |
| Repas fixe | Assiette servie chaque semaine sur un repas de la semaine, au lieu d'un tirage. |
| Article | Tout ce qui s'achète : ingrédient, composant planifié, ou article ajouté à la main (« Papier toilette »). Catalogue distinct des listes de composants. |
| Ingrédient | Article d'une recette de plat complet, avec une quantité en texte libre (« 200 g »). |
| Rayon | Rayon du magasin ; les rayons sont ordonnés comme le parcours dans le magasin. |
| Liste de courses | Les articles à acheter pour les repas d'une période, plus les ajouts à la main. Une seule liste à la fois. |

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
| EF-10 | Chaque onglet de liste affiche ses éléments avec, pour chacun, son nom, son poids général et ses 4 poids saisonniers, **modifiables en place** et enregistrés en une fois. | O |
| EF-11 | Formulaire d'**ajout** d'un élément dans la liste de l'onglet : nom obligatoire, poids pré-remplis à la valeur par défaut (5). | O |
| EF-12 | **Suppression** d'un élément. Elle est logique : il disparaît de la liste et des tirages, mais reste affiché dans les plannings passés. | I |
| EF-13 | Onglet Repas : choix des **repas planifiés jour par jour** (midi et/ou soir pour chaque jour de la semaine ; au moins un repas dans la semaine, un jour peut n'en avoir aucun). Défaut : midi et soir tous les jours. | I |
| EF-14 | Le nom d'un élément est unique (insensible à la casse) parmi les éléments non supprimés **de la même liste**, 1 à 100 caractères. | O |
| EF-15 | Onglet Repas : **part de repas composés**, de 0 à 100 %. Défaut : 50 %. | O |
| EF-18 | Onglet Repas : chaque repas de la semaine peut recevoir un **repas fixe** (un plat complet, ou des composants), choisi sur une page dédiée ; on peut le retirer pour revenir au tirage. | I |
| EF-19 | Chaque plat complet a une **fiche d'ingrédients** : ajout (nom, quantité libre ≤ 50 caractères, rayon facultatif), modification groupée des quantités, retrait, ordre de saisie. Les articles et les féculents, viandes/poissons et légumes vivants sont proposés à la saisie ; un ingrédient saisi n'est jamais ajouté à ces listes. Un ingrédient est unique (insensible à la casse) dans une recette ; l'article est partagé entre recettes et reste au catalogue quand plus aucune recette ne l'utilise. | I |
| EF-20 | Onglet **Rayons** : ajout, renommage groupé, suppression (ses articles restent sans rayon), ordre modifiable par boutons ↑/↓. | I |
| EF-21 | Onglet **Articles** : nom et rayon de chaque article, modifiables en place et enregistrés en une fois ; ajout ; suppression, refusée pour un ingrédient d'une recette. | I |

### 3.4 Liste de courses

| Réf | Exigence | Prio |
|---|---|---|
| EF-22 | Une liste de courses couvre une **période** (1 à 31 jours). Sans liste, ou par « Nouvelle liste », la page propose la **dernière semaine entièrement prévue** à partir de la semaine en cours (tous ses repas planifiés remplis) ; sinon, ou au choix, on saisit les dates de début et de fin. Une nouvelle liste **remplace** la précédente, ses cases cochées et ses ajouts : les courses sont ponctuelles. | I |
| EF-23 | On peut **ajouter à la main** un article à la liste (nom avec suggestions, quantité libre), puis le retirer. Aucun ajout n'est permanent ni récurrent. | I |
| EF-24 | Chaque ligne montre l'article, et pour chaque besoin la quantité éventuelle, le plat dont il est l'ingrédient, le jour et le repas (ou « Ajouté à la main »). Une **case** met l'article dans le panier : la ligne passe en fin de liste, atténuée, sous « Dans le panier ». Un article sans rayon peut être rangé depuis la liste. Les plats complets planifiés sans ingrédients sont signalés en tête de liste. | I |

### 3.3 Règles de tirage

| Réf | Règle |
|---|---|
| RG-01 | Poids effectif d'un élément pour un jour J = `poids général × poids de la saison de J`. Un poids effectif nul exclut l'élément ce jour-là. |
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
| EI-02 | Fonctionne **sans JavaScript** ; le JS n'apporte que du confort : envoi automatique du sélecteur de jour de début, actions sur la semaine sans rechargement ni déplacement de la page, glisser-déposer et déplacement par toucher des repas. |
| EI-03 | Accessibilité : libellés de formulaires, focus clavier visible, contrastes AA, `prefers-reduced-motion` respecté. |
| EI-04 | Toute action mutante suit le schéma POST → redirection → GET ; le résultat est confirmé par un message flash. |
| EI-05 | Les erreurs de saisie réaffichent le formulaire avec les valeurs saisies et un message par champ fautif. |
| EI-06 | L'application est **installable** sur téléphone et ordinateur (application web progressive) : manifeste traduit, icônes, raccourcis Semaine et Courses. Un service worker « réseau d'abord » garde les dernières pages vues consultables hors ligne, avec un bandeau qui le signale ; hors ligne, les formulaires ne sont pas envoyés. Installation possible en HTTPS (ou sur `localhost`). |

## 5. Exigences non fonctionnelles

| Réf | Exigence |
|---|---|
| ENF-01 | Go 1.26+, **bibliothèque standard** pour le HTTP (`http.ServeMux`), `html/template`. Aucun framework web, CSS ou JS. |
| ENF-02 | Socle **`leblanc.io/open-go-base`** : `appconf` (configuration, `--help`, `--version`), `logx`, `ratelimit`, `i18n` (`NewFS`), `csrfx`. Ne pas ré-implémenter ces briques. |
| ENF-03 | Binaire **autonome** : gabarits, CSS/JS et traductions embarqués (`go:embed`). |
| ENF-04 | Stockage **SQLite** via `modernc.org/sqlite` (pur Go, `CGO_ENABLED=0`). Migrations SQL embarquées, jouées au démarrage, idempotentes. |
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
| SEC-05 | Validation serveur de toutes les entrées (bornes des poids, longueur des noms, dates, repas connus). |

## 7. Évolutions possibles

- Articles récurrents (ajoutés à chaque nouvelle liste).
- Pistes : nombre de convives, pénalité des menus servis la semaine précédente,
  contraintes (« poisson le vendredi »).

## 8. Architecture

```
main.go                      câblage, go:embed, middlewares, arrêt propre
internal/config              Config = fragments appconf (Web, Logging, I18n, Database)
internal/planner             logique pure : saisons, bornes de semaine, tirage pondéré
internal/shopping            logique pure : regroupement de la liste de courses
internal/store               SQLite : migrations, éléments, planning, réglages,
                             articles, rayons, liste en cours
internal/web                 handlers, rendu des gabarits, vues, flash, langue
templates/  static/  locales/
```

Pile de middlewares (extérieur → intérieur) : en-têtes de sécurité → langue →
`/static/` hors limiteur | limiteur → CSRF → routeur.

Routes :

| Méthode | Chemin | Rôle |
|---|---|---|
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
| GET | `/shopping` | Liste de courses en cours, ou choix de la période s'il n'y en a pas |
| GET | `/shopping/new` | Choix de la période d'une nouvelle liste |
| POST | `/shopping` | Crée la liste (`from`, `to`) |
| POST | `/shopping/extras` | Ajout à la main (`name`, `quantity`) |
| POST | `/shopping/articles/{id}` | `action=tick&needs=…`, `untick`, `remove` (ajout à la main), `aisle&aisle=…` |

## 9. Modèle de données

```sql
items       (id, kind, name, weight, weight_spring, weight_summer, weight_autumn,
             weight_winter, created_at, deleted_at)
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
rayons, quantités et tables de la liste de courses, sans perte.

## 10. Recette

| Réf | Scénario |
|---|---|
| TR-01 | Base vide → l'accueil invite à ajouter des menus ; « Générer » informe qu'aucun menu n'est disponible. |
| TR-02 | 20 menus, semaine vide → « Générer » remplit 14 repas sans doublon. |
| TR-03 | Un menu au poids d'été 0 n'est jamais tiré pour un jour de juillet. |
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
