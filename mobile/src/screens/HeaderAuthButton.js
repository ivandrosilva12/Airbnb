import { Pressable, Text } from 'react-native';
import { useNavigation } from '@react-navigation/native';
import { useAuth } from '../auth/AuthContext';
import { useT } from '../i18n/I18nContext';

// HeaderAuthButton is the right-hand affordance on every Stack header.
// Authed: tap → Account screen, long-press → sign out (kept as a power-user
// shortcut). Unauthed: tap → start the OIDC flow. Labels go through t() so
// the chrome flips language alongside the rest of the app.
export function HeaderAuthButton() {
  const { authenticated, login, logout, canLogin } = useAuth();
  const { t } = useT();
  const navigation = useNavigation();

  if (authenticated) {
    return (
      <Pressable
        onPress={() => navigation.navigate('Account')}
        onLongPress={logout}
        hitSlop={10}
      >
        <Text style={{ color: '#ff385c', fontWeight: '600' }}>{t('nav.account')}</Text>
      </Pressable>
    );
  }
  return (
    <Pressable disabled={!canLogin} onPress={login} hitSlop={10}>
      <Text style={{ color: canLogin ? '#ff385c' : '#bbb', fontWeight: '600' }}>{t('nav.signIn')}</Text>
    </Pressable>
  );
}
