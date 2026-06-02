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
import './styles.css';

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
