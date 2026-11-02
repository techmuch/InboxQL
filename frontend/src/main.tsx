import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { initializeShell } from 'nexus-shell'
import './index.css'
import App from './App.tsx'

// Initialize core shell services
initializeShell();

// Installable as an app (Chrome's "Install app"), and able to open when the
// computer serving it is out of reach. Only on a secure origin — localhost, or
// an HTTPS address such as Tailscale's — and only in a built app: the
// development server reloads itself and a worker would get in its way.
if ('serviceWorker' in navigator && window.isSecureContext && import.meta.env.PROD) {
  window.addEventListener('load', () => { navigator.serviceWorker.register('/sw.js').catch(() => { /* still a web page */ }); });
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
