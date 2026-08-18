import { createStart } from '@tanstack/react-start'

export const startInstance = createStart(() => ({
  // Pure SPA: no route runs on the server. Loaders, beforeLoad and components
  // are all client-only.
  defaultSsr: false,
}))
