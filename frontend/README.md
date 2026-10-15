# Front de Symphony

SPA Vue 3 + Vite, compilée dans `internal/web/dist` et embarquée dans le binaire Go
(`go:embed`) : en production, Symphony sert le front et l'API sur la même origine.

```bash
npm install
npm run dev     # http://localhost:5173, relaie /api et /auth vers http://localhost:8080
npm run build   # compile dans ../internal/web/dist (embarqué au prochain go build)
```

- `src/api.js` : un appel par route de l'API (`/api/v1`) ; la session est un cookie
  `HttpOnly` posé par Symphony, le front ne manipule aucun jeton.
- `src/stores/` : session (utilisateur, rôle) et tâches (flux Server-Sent Events).
- `src/views/` : Projets, Catalogue (golden paths), Déploiements, Activité.
- `VITE_API_URL` (`.env.local`) vise une autre instance de Symphony.
