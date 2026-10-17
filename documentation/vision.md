# Vision avant la v1.0

Étude du 16 octobre 2026, après la v0.10, puis décisions prises le même jour (9 séries de
questions). Le découpage en versions reste à confronter au test complet de la démo
(développeur, chef de projet, admin plateforme) : les irritants relevés passeront en
premier.

## La phrase à tenir

> **Symphony, c'est un Cycloid open source qui s'administre comme un Backstage facile.**
> L'équipe plateforme publie des *stacks* (une appli et son infra, avec un formulaire
> borné) dans Git. Les développeurs les remplissent pour obtenir un service qui tourne,
> de staging à la production. Tout passe par les outils que l'entreprise a déjà
> (GitLab ou GitHub, leur CI, Kubernetes ou Docker, Vault ou OpenBao, son SSO).

Chaque fonctionnalité se juge à cette phrase : « est-ce qu'elle garde l'installation
simple et l'administration sans code ? ».

**La v1.0** : Symphony est ouvert aux utilisateurs, qui peuvent l'essayer puis
l'intégrer dans leur contexte. On a le droit de l'utiliser (licence), on sait le brancher
sur ses outils sans lire le code, et une mise à jour ne casse rien (SemVer strict).

**Cible** : des PME pour la v1, avec les ETI comme vraie cible (une IDP prend tout son
sens à partir de plusieurs équipes). **Le produit passe avant le portfolio** : un vrai
utilisateur vaut plus qu'une démo brillante. **Pas de date** : la v1 sort quand ses
critères sont remplis. **Français d'abord** : un produit assumé pour le marché français
(ETI, souveraineté) ; l'anglais viendra plus tard.

## Le paysage

| Outil | Ce que c'est | Ce qu'on en retient |
|---|---|---|
| **Backstage** (CNCF, Apache 2.0) | Un *framework* pour construire un portail : catalogue, templates, TechDocs, plugins en TypeScript | Le modèle est le bon (tout dans Git, `catalog-info.yaml`), mais il coûte cher : une équipe dédiée, des plugins à maintenir à chaque version, une adoption interne souvent faible hors de Spotify. Sa version sans code, **Spotify Portal**, est un SaaS payant. |
| **Port, Cortex, OpsLevel, Compass** | Portails en SaaS | Faciles à démarrer, mais on dépend du fournisseur. Ce n'est pas notre créneau. |
| **Cycloid** | Plateforme propriétaire : *Stacks* (infra, pipeline et workflows packagés), *StackForms* (formulaires bornés), repos de catalogue et de config dans Git, InfraPolicies, inventaire, FinOps | Le modèle « l'équipe plateforme borne, le développeur remplit un formulaire ». Seuls des outils annexes sont open source : **il n'existe pas de Cycloid open source**, et c'est la place que vise Symphony. |
| **Devtron, Otomi (Akamai App Platform)** | Plateformes centrées sur Kubernetes, qui installent leur propre pile | Lourds. Symphony se branche sur l'existant au lieu d'installer une pile. |
| **Kratix** | Un framework pour construire une plateforme | Pour les équipes plateforme qui codent, l'inverse du « facile ». |

## Décisions

**Stacks et infrastructure**
- Une **stack** est une appli **et son infra**, ensemble : un seul formulaire crée le
  repo, la CI, la base de données et le déploiement.
- L'infra entre **avant la v1**. Elle est appliquée par **la CI du repo** (OpenTofu en
  plan puis apply, ou des manifests d'opérateurs en GitOps) : Symphony déclenche et suit,
  et ne détient jamais d'identifiants cloud.
- **Première cible de bout en bout : Kubernetes avec des opérateurs** (PostgreSQL avec
  CloudNativePG, testable dans la démo k3s et en CI). Viennent ensuite AWS et les clouds
  français ou européens (Scaleway, OVHcloud) via OpenTofu, selon le besoin du client.
- **Paramètres d'une stack** : en YAML, dans un format maison simple (`name`, `title`,
  `type` string/number/boolean/choice, `choices`, `default`, `required`, `when`, `locked`),
  traduit en JSON Schema en interne pour la validation. Il doit rester lisible par un
  admin DevOps qui n'est pas développeur.
- **Manifests Kubernetes** : fournis par la stack (chart Helm ou base Kustomize) quand
  elle en a, sinon générés par Symphony comme aujourd'hui.
- **Catalogue** : un catalogue officiel de stacks (repo Git public) **plus** celles du
  client ; l'admin choisit ce qu'il expose.

**Administration**
- **Git reste la source** de la configuration. L'interface montre, valide et explique
  les erreurs, mais n'écrit rien.
- **Installation** : Docker Compose sur une machine et Helm sur Kubernetes, à égalité.
- **Une instance par entreprise**, organisée en équipes, sans multi-organisation.
- Une seule instance, sans haute disponibilité avant la v1, mais avec **sauvegarde et
  restauration** documentées et testées.

**Développeur**
- **Services existants** : scan d'un groupe GitLab ou d'une organisation GitHub, et tout
  repo qui a une fiche entre dans le catalogue.
- **Fiches compatibles Backstage** (`backstage.io/v1alpha1`) : une ETI qui a déjà des
  fiches les garde. Nos ajouts (environnements, actions) passent par des annotations ou
  des champs étendus.
- **Retour arrière** en un clic vers la version précédente d'un environnement.
- **Secrets en self-service** : le développeur les saisit, Symphony les écrit dans le
  coffre sous `apps/<projet>/` et ne les affiche plus. Coffres : **Vault ou OpenBao**,
  et **Secrets Kubernetes / SOPS** pour un client tout Kubernetes.
- **Runtime Docker : une cible de production pour les PME** sans Kubernetes. Il doit
  devenir solide : hôtes distants, TLS, reverse proxy.

**Chef de projet et gouvernance**
- Un **rôle `lead` par équipe** : viewer < developer < lead < admin. Le lead approuve et
  pilote les projets de ses équipes.
- **Approbation par environnement**, dans la config (`approval: lead` ou `none`).
- **Notifications** : Slack ou Teams par webhook (un canal par équipe), et une
  notification dans Symphony pour le lead.
- **Matrice versions × environnements** : quelle version tourne où, ce qui attend une
  promotion ou une approbation, ce qui a échoué.
- **Provider « observability »** : Symphony lit Prometheus et Loki pour montrer l'état,
  les erreurs et la latence par service et par environnement, rapportés aux SLO de la
  fiche.

**Ouverture**
- **Licence Apache 2.0.**
- **SemVer strict à la v1** : configuration, fiches, stacks et API `/api/v1` versionnées,
  sans rupture avant une v2, et chaque dépréciation annoncée une version à l'avance avec
  son outil de migration.
- **Site de documentation** dédié : installation, configuration de référence, écrire
  une stack.
- **Essayer** : un quickstart léger (Symphony et PostgreSQL en Compose, branchés sur
  gitlab.com ou GitHub) **et** la démo complète, plus le mode visite.
- **Premiers utilisateurs** : un appel à bêta-testeurs avant la v1.

**Après la v1** : TechDocs, scorecards, indicateurs DORA, haute disponibilité,
multi-organisation, interface en anglais, stacks officielles AWS et Scaleway (si elles
ne sont pas prêtes avant).

**Encore ouvert**
- **Le nom** : aucun outil DevOps ne s'appelle Symphony, mais le nom est pris par une
  entreprise connue de messagerie pour la finance (Symphony et sa Software Foundation),
  et il se prononce comme le framework PHP Symfony. À trancher avant d'ouvrir.
- **Le vocabulaire** : « golden path » (aujourd'hui) ou « stack » (Cycloid) ? Il faut un
  seul mot, dans l'interface, la doc et le format.

## Proposition de versions

Reprise dans [ROADMAP.md](../ROADMAP.md), qui fait foi ; à réordonner avec le retour du test
de la démo.

- **v0.11 — L'admin y voit clair** : licence Apache 2.0 ; page d'administration
  (providers et leur état, configuration appliquée et dernière erreur, stacks chargées
  ou refusées et pourquoi, audit) ; `symphony validate` (configuration et stacks, en CLI
  et en CI) ; configuration de référence commentée ; quickstart léger.
- **v0.12 — Stacks paramétrables** : `spec.parameters` rendus en formulaire et injectés
  dans le squelette ; manifests fournis par la stack (Helm ou Kustomize) ; catalogue
  officiel dans un repo public, à côté de celui du client.
- **v0.13 — L'infra avec l'appli** : la stack porte son infra, appliquée par la CI ;
  PostgreSQL par CloudNativePG de bout en bout (démo k3s, CI) ; un projet supprimé
  emporte son infra.
- **v0.14 — Les services existants** : fiches compatibles Backstage ; scan d'un groupe
  GitLab ou d'une organisation GitHub.
- **v0.15 — L'équipe et la gouvernance** : rôle `lead`, approbations par environnement,
  notifications (Slack ou Teams, et dans Symphony), matrice versions × environnements,
  retour arrière.
- **v0.16 — Exploiter ses services** : secrets en self-service (Vault ou OpenBao,
  Secrets Kubernetes / SOPS) ; provider d'observabilité (Prometheus, Loki, SLO) ;
  runtime Docker prêt pour la production (hôtes distants, TLS, reverse proxy).
- **v0.17 — Prêt à être adopté** : site de documentation, guide « brancher Symphony sur
  mes outils », sauvegarde et restauration, nom tranché, appel à bêta-testeurs.
- **v1.0** : contrat versionné (SemVer strict), montée de version testée d'une version à
  l'autre, guide d'exploitation, sécurité revue, au moins une installation par un
  bêta-testeur et ses retours traités.

## Sources

- [Backstage : coût, maintenance et adoption (DEV Community)](https://dev.to/jianreis/backstage-how-much-does-it-really-cost-31n5),
  [Spotify Backstage, potentiel et limites (ayedo)](https://ayedo.de/en/posts/spotify-backstage-potenziale-und-herausforderungen-interner-developer-plattformen/),
  [Adoption interne de Backstage (forum, anecdotique)](https://tianpan.co/forum/t/backstage-holds-89-market-share-but-only-10-internal-adoption-why/417)
- [Spotify Portal for Backstage (SD Times)](https://sdtimes.com/softwaredev/spotify-launches-portal-for-backstage-to-simplify-process-of-getting-developer-portals-setup/),
  [Spotify Portal](https://backstage.spotify.com/portal)
- [Documentation de Cycloid](https://docs.cycloid.io/),
  [Cycloid : gouvernance et StackForms](https://www.cycloid.io/blog/cycloid-platform-engineering-initiatives-that-scale-governance-without-scaling-headcount-in-2026/),
  [Cycloid : composants de stack (communiqué)](https://www.cycloid.io/press/cycloid-adds-ability-to-customize-components-of-a-software-stack-to-devops-platform/)
- [Alternatives à Backstage (Roadie)](https://roadie.io/blog/backstage-alternatives/),
  [Alternatives à Backstage (Cortex)](https://www.cortex.io/post/backstage-alternatives-what-engineering-leaders-need-to-know-in-2026)
- [Akamai App Platform, issue d'Otomi (SiliconANGLE)](https://siliconangle.com/2024/11/12/akamai-rolls-packaged-platform-distributed-kubernetes-applications/),
  [Devtron (StackShare)](https://stackshare.io/stackups/devops-stack-vs-devtron)
- [Symphony Software Foundation (FINOS)](https://www.finos.org/press/foundation-offers-open-developer-platform)

Les sources sur Backstage et ses alternatives viennent souvent d'éditeurs concurrents :
les ordres de grandeur (adoption, coût) sont indicatifs.
