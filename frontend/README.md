# Front de Symphony

SPA Vue 3 + Vite, compilée dans `internal/web/dist` et embarquée dans le binaire Go
(`go:embed`) : en production, Symphony sert le front et l'API sur la même origine.

```bash
npm install
npm run dev     # http://localhost:5173, relaie /api et /auth vers http://localhost:8080
npm run build   # compile dans ../internal/web/dist (embarqué au prochain go build)
npm run typecheck && npm run lint && npm test   # vue-tsc, ESLint, Vitest (aussi en CI)
```

TypeScript strict partout (`<script setup lang="ts">`).

- `src/api.ts` : un appel typé par route de l'API (`/api/v1`) ; la session est un cookie
  `HttpOnly` posé par Symphony, le front ne manipule aucun jeton.
- `src/types.ts` : les réponses de l'API (clés snake_case des structs Go).
- `src/lib/` : logique pure (filtres, URL, libellés), testée.
- `src/stores/` : session (rôle, équipes, droits) et tâches (flux Server-Sent Events).
- `src/components/` : carte et panneau d'un projet, action Day-2, avancement d'une tâche.
- `src/views/` : Projets, Catalogue (golden paths), Déploiements, Activité.
- `VITE_API_URL` (`.env.local`) vise une autre instance de Symphony.
