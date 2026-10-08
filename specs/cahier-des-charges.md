# OpenMenu — cahier des charges

Application web de planification de repas : à partir d'une liste de menus pondérés,
elle tire au sort les repas d'une semaine en tenant compte de la saison.

Priorités : **O** obligatoire (v1), **I** important, **S** souhaité, **V2** prévu
pour une version ultérieure.

## 1. Contexte et périmètre

Un foyer veut arrêter de se demander « qu'est-ce qu'on mange ? ». Il entretient une
liste de menus (au départ un simple nom : « Gratin dauphinois », « Salade niçoise »),
indique pour chacun s'il l'aime plus ou moins (poids général) et à quelle saison il
convient (poids par saison), puis laisse l'application proposer la semaine.

Hors périmètre v1 : comptes utilisateurs, partage multi-foyers, ingrédients et liste
de courses (V2, cf. §7), API publique.

L'application est mono-foyer. Elle n'embarque **pas d'authentification** : en
déploiement exposé, elle se place derrière un reverse proxy qui authentifie (basic
auth, SSO…). C'est documenté dans le README.

## 2. Vocabulaire

| Terme | Sens |
|---|---|
| Menu | Un plat proposable (« Lasagnes »). Entité `Dish` dans le code. |
| Repas | Un créneau d'une journée : midi (`lunch`) ou soir (`dinner`). |
| Semaine | 7 jours consécutifs commençant au jour de début de semaine paramétré. |
| Planning | L'affectation d'un menu à un couple (jour, repas). Entité `PlanEntry`. |
| Poids général | Préférence globale pour un menu, entier de 0 à 10. |
| Poids saisonnier | Adéquation d'un menu à une saison, entier de 0 à 10, un par saison. |

## 3. Exigences fonctionnelles

### 3.1 Page d'accueil — la semaine

| Réf | Exigence | Prio |
|---|---|---|
| EF-01 | La page d'accueil affiche la semaine **courante** : 7 jours à partir du jour de début de semaine, chaque jour présentant ses repas actifs (midi, soir). | O |
| EF-02 | Le jour de début de semaine (lundi… dimanche) est **paramétrable directement sur la page d'accueil**. Le choix est persistant. Défaut : lundi. | O |
| EF-03 | Si aucun repas de la semaine affichée n'a de menu, la page affiche un bouton **« Générer »** qui tire au sort toute la semaine. | O |
| EF-04 | Si la semaine est partiellement remplie (ex. après un changement de jour de début), un bouton **« Compléter »** remplit uniquement les repas vides. | I |
| EF-05 | Une semaine remplie propose **« Régénérer »** (re-tire tous les repas). | I |
| EF-06 | Chaque repas peut être **relancé** individuellement (nouveau tirage pour ce seul repas), **choisi** manuellement dans la liste des menus, ou **vidé**. | I |
| EF-07 | Navigation semaine précédente / suivante / retour à la semaine courante, pour préparer la semaine à venir ou consulter l'historique. | I |
| EF-08 | Le jour courant est mis en évidence ; la saison de chaque jour est visible. | S |

### 3.2 Page de paramétrage

| Réf | Exigence | Prio |
|---|---|---|
| EF-10 | Liste de tous les menus avec, pour chacun, son nom, son poids général et ses 4 poids saisonniers, **modifiables en place** et enregistrés en une fois. | O |
| EF-11 | Formulaire d'**ajout** d'un menu : nom obligatoire, poids pré-remplis à la valeur par défaut (5). | O |
| EF-12 | **Suppression** d'un menu. Elle est logique : le menu disparaît de la liste et des tirages, mais reste affiché dans les plannings passés. | I |
| EF-13 | Choix des **repas planifiés** (midi, soir ; au moins un). Défaut : midi et soir. | I |
| EF-14 | Le nom d'un menu est unique (insensible à la casse) parmi les menus non supprimés, 1 à 100 caractères. | O |

### 3.3 Règles de tirage

| Réf | Règle |
|---|---|
| RG-01 | Poids effectif d'un menu pour un jour J = `poids général × poids de la saison de J`. Un poids effectif nul exclut le menu ce jour-là. |
| RG-02 | Chaque repas est tiré avec une probabilité proportionnelle au poids effectif. |
| RG-03 | Un même menu n'apparaît pas deux fois dans la semaine **tant qu'il reste des candidats** ; les repas déjà remplis de la semaine comptent. |
| RG-04 | Si tous les candidats sont épuisés, le tirage autorise la répétition, en évitant si possible le menu du repas immédiatement précédent. |
| RG-05 | Si aucun menu n'a de poids effectif positif, le repas reste vide et l'utilisateur en est informé. |
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
| ENF-05 | **Internationalisation dès la v1** : aucune chaîne en dur dans les gabarits ou le code. Clés anglaises `écran.élément` (`home.generate`, `settings.add_dish`) ; segments transverses sous leurs propres racines (`weekday.*`, `month.*`, `season.*`, `meal.*`, `validation.*`, `flash.*`, `date.*`). Langues : français (défaut) et anglais, catalogues alignés. Le JS reçoit ses libellés via des attributs `data-`. |
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

- Tables `ingredients (id, name, unit)` et `dish_ingredients (dish_id, ingredient_id,
  quantity)`, ajoutées par une migration `002`. La table `dishes` n'est pas modifiée.
- Fiche menu dédiée (`/settings/dishes/{id}`) pour éditer les ingrédients.
- Liste de courses de la semaine : agrégation des ingrédients des menus planifiés.
- Pistes : nombre de convives, pénalité des menus servis la semaine précédente,
  contraintes (« poisson le vendredi »).

## 8. Architecture

```
main.go                      câblage, go:embed, middlewares, arrêt propre
internal/config              Config = fragments appconf (Web, Logging, I18n, Database)
internal/planner             logique pure : saisons, bornes de semaine, tirage pondéré
internal/store               SQLite : migrations, menus, planning, réglages
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
| GET | `/settings` | Paramétrage |
| POST | `/settings/dishes` | Ajout d'un menu |
| POST | `/settings/dishes/save` | Enregistrement groupé des menus |
| POST | `/settings/dishes/{id}/delete` | Suppression logique |
| POST | `/settings/meals` | Repas planifiés |

## 9. Modèle de données (v1)

```sql
dishes        (id, name, weight, weight_spring, weight_summer, weight_autumn,
               weight_winter, created_at, deleted_at)
plan_entries  (day TEXT 'AAAA-MM-JJ', meal TEXT, dish_id → dishes.id,
               PRIMARY KEY (day, meal))
settings      (key PRIMARY KEY, value)    -- week_start, meals
```

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
