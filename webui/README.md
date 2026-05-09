# graph-go / webui

React + TypeScript frontend for graph-go. Built with Vite and React Flow.

## Development

```bash
npm install
npm run dev      # http://localhost:5173
npm run build    # Production build to dist/
npm run lint     # ESLint
```

The dev server proxies API requests to the backend at `localhost:8080`. The UI is a backend consumer, not a standalone mock shell, so it expects a running backend and shows the real loading or error state when the API is unavailable.
