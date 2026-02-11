import React from 'react';
import ReactDOM from 'react-dom/client';
import App from './App';
import { initApiBase } from './api';

const rootElement = document.getElementById('root');
if (!rootElement) {
  throw new Error("Could not find root element to mount to");
}

// Resolve sidecar port before rendering (no-op in browser dev mode)
initApiBase().then(() => {
  const root = ReactDOM.createRoot(rootElement);
  root.render(
    <React.StrictMode>
      <App />
    </React.StrictMode>
  );
});
