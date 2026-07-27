import React from 'react';
import ReactDOM from 'react-dom/client';
import { initApiBase } from './api';
import './styles.css';

function resolveShell(): 'legacy' | 'new' {
  const params = new URLSearchParams(window.location.search);
  if (params.get('shell') === 'legacy') return 'legacy';
  try {
    if (localStorage.getItem('codeflow.shell') === 'legacy') return 'legacy';
  } catch {
    /* ignore */
  }
  return 'new';
}

const rootElement = document.getElementById('root');
if (!rootElement) {
  throw new Error('Could not find root element to mount to');
}

const shell = resolveShell();

initApiBase().then(() => {
  const splash = document.getElementById('cf-splash');

  if (shell === 'legacy') {
    document.body.classList.add('bg-slate-50', 'text-slate-900', 'font-sans');
    splash?.remove();
    import('./App').then(({ default: App }) => {
      ReactDOM.createRoot(rootElement).render(
        <React.StrictMode>
          <App />
        </React.StrictMode>,
      );
    });
  } else {
    document.body.classList.add('cf-app');
    import('./src/AppRoot').then(({ default: AppRoot }) => {
      splash?.remove();
      ReactDOM.createRoot(rootElement).render(
        <React.StrictMode>
          <AppRoot />
        </React.StrictMode>,
      );
    });
  }
});
