import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import App from './App';
import { AuthProvider } from './context/AuthContext';
import { ConsentProvider } from './context/ConsentContext';
import { FavoritesProvider } from './context/FavoritesContext';
import { NotificationsProvider } from './context/NotificationsContext';
import { MessagesProvider } from './context/MessagesContext';
import { RealtimeProvider } from './context/RealtimeContext';
import { I18nProvider } from './i18n/I18nContext';
import { registerServiceWorker, pushSupported } from './push/webPush';
import './styles.css';

// Mount the web-push service worker eagerly (S98) so the browser delivers
// `push` events whenever the user has opted in — even when the SPA tab is
// closed. Silent no-op in unsupported browsers (Safari < 16, in-app webviews).
if (pushSupported()) {
  // Defer to the next tick so the React tree mounts first — registering a
  // worker is async and we don't want to compete with the first paint.
  setTimeout(() => {
    registerServiceWorker();
  }, 0);
}

ReactDOM.createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <BrowserRouter>
      <I18nProvider>
        <ConsentProvider>
          <AuthProvider>
            <FavoritesProvider>
              <NotificationsProvider>
                <MessagesProvider>
                  <RealtimeProvider>
                    <App />
                  </RealtimeProvider>
                </MessagesProvider>
              </NotificationsProvider>
            </FavoritesProvider>
          </AuthProvider>
        </ConsentProvider>
      </I18nProvider>
    </BrowserRouter>
  </React.StrictMode>,
);
