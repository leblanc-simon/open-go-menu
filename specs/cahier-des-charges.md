# OpenMenu — cahier des charges

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

Hors périmètre v1 : comptes utilisateurs, partage multi-foyers, ingrédients et liste
de courses (V2, cf. §7), API publique.

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
| EF-13 | Onglet Repas : choix des **repas planifiés** (midi, soir ; au moins un). Défaut : midi et soir. | I |
| EF-14 | Le nom d'un élément est unique (insensible à la casse) parmi les éléments non supprimés **de la même liste**, 1 à 100 caractères. | O |
| EF-15 | Onglet Repas : **part de repas composés**, de 0 à 100 %. Défaut : 50 %. | O |

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

## 4. Exigences d'interface

| Réf | Exigence |
|---|---|
| EI-01 | Interface responsive, utilisable sur téléphone (cas d'usage principal : en cuisine, au marché). |
| EI-02 | Fonctionne **sans JavaScript** ; le JS n'apporte que du confort (envoi automatique du sélecteur de jour de début). |
| EI-03 | Accessibilité : libellés de formulaires, focus clavier visible, contrastes AA, `prefers-reduced-motion` respecté. |
| EI-04 | Toute action mutante suit le schéma POST → redirection → GET ; le résultat est confirmé par un message flash. |
| EI-05 | Les erreurs de saisie réaffichent le formulaire avec les valeurs saisies et un message par champ fautif. |

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

## 7. Évolutions prévues (V2) — ingrédients

La v1 doit les rendre possibles sans refonte :

- Tables `ingredients (id, name, unit)` et `item_ingredients (item_id, ingredient_id,
  quantity)`, ajoutées par une nouvelle migration. La table `items` n'est pas modifiée.
- Fiche élément dédiée (`/settings/items/{id}`) pour éditer les ingrédients.
- Liste de courses de la semaine : agrégation des ingrédients des éléments planifiés.
- Pistes : nombre de convives, pénalité des menus servis la semaine précédente,
  contraintes (« poisson le vendredi »).

## 8. Architecture

```
main.go                      câblage, go:embed, middlewares, arrêt propre
internal/config              Config = fragments appconf (Web, Logging, I18n, Database)
internal/planner             logique pure : saisons, bornes de semaine, tirage pondéré
internal/store               SQLite : migrations, éléments, planning, réglages
internal/web                 handlers, rendu des gabarits, vues, flash, langue
templates/  static/  locales/
```

Pile de middlewares (extérieur → intérieur) : en-têtes de sécurité → langue →
`/static/` hors limiteur | limiteur → CSRF → routeur.

Routes :

| Méthode | Chemin | Rôle |
|---|---|---|
| GET | `/` | Semaine (`?week=AAAA-MM-JJ` : n'importe quel jour de la semaine voulue) |
| POST | `/week-start` | Change le jour de début de semaine |
| POST | `/plan/generate` | Génère / complète / régénère (`mode=fill|all`) la semaine |
| POST | `/plan/{date}/{meal}` | Action sur un repas (`action=reroll|set|clear`) |
| POST | `/plan/{date}/{meal}` | `action=reroll&kind=starch|protein|vegetable` : relance d'un seul composant |
| GET | `/settings` | Paramétrage, onglet Plats complets |
| GET | `/settings/{list}` | Onglet d'une liste : `dishes`, `starches`, `proteins`, `vegetables` |
| POST | `/settings/{list}` | Ajout d'un élément à la liste |
| POST | `/settings/{list}/save` | Enregistrement groupé de la liste |
| POST | `/settings/items/{id}/delete` | Suppression logique d'un élément |
| GET, POST | `/settings/meals` | Onglet Repas : repas planifiés et part de repas composés |

## 9. Modèle de données

```sql
items       (id, kind, name, weight, weight_spring, weight_summer, weight_autumn,
             weight_winter, created_at, deleted_at)
             -- kind : dish | starch | protein | vegetable
             -- nom unique par (kind, name) parmi les éléments non supprimés
plan_items  (day TEXT 'AAAA-MM-JJ', meal TEXT, kind TEXT, item_id → items.id,
             PRIMARY KEY (day, meal, kind))
             -- une ligne kind=dish pour un plat complet, sinon une ligne par composant
settings    (key PRIMARY KEY, value)    -- week_start, meals, composed_share
```

La migration `002` renomme `dishes` en `items` (sorte `dish` pour l'existant) et
convertit `plan_entries` en `plan_items` sans perte.

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
