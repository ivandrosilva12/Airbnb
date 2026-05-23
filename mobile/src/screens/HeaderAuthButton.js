import { Pressable, Text } from 'react-native';
import { useNavigation } from '@react-navigation/native';
import { useAuth } from '../auth/AuthContext';

export function HeaderAuthButton() {
  const { authenticated, login, logout, canLogin } = useAuth();
  const navigation = useNavigation();

  if (authenticated) {
    return (
      <Pressable
        onPress={() => navigation.navigate('Trips')}
        onLongPress={logout}
        hitSlop={10}
      >
        <Text style={{ color: '#ff385c', fontWeight: '600' }}>Trips</Text>
      </Pressable>
    );
  }
  return (
    <Pressable disabled={!canLogin} onPress={login} hitSlop={10}>
      <Text style={{ color: canLogin ? '#ff385c' : '#bbb', fontWeight: '600' }}>Sign in</Text>
    </Pressable>
  );
}
