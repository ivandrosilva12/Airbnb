import { useEffect, useState } from 'react';
import { View, Text, Pressable, StyleSheet, Alert } from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';

const LINKS = [
  { screen: 'Trips', label: 'My trips', icon: '🧳' },
  { screen: 'Saved', label: 'Saved listings', icon: '♥' },
  { screen: 'Messages', label: 'Messages', icon: '💬' },
  { screen: 'Notifications', label: 'Notifications', icon: '🔔' },
  { screen: 'Splits', label: 'Split payments', icon: '🤝' },
  { screen: 'Verification', label: 'Identity verification', icon: '🪪' },
];

export default function AccountScreen({ navigation }) {
  const { authenticated, login, logout, setupTwoFactor, canSetupTwoFactor } = useAuth();
  const api = useApi();
  const [isHost, setIsHost] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!authenticated) return;
    api.me().then((u) => setIsHost(u.role === 'host' || u.role === 'admin')).catch(() => {});
  }, [authenticated]);

  async function becomeHost() {
    setBusy(true);
    try {
      await api.becomeHost();
      setIsHost(true);
      navigation.navigate('HostListings');
    } catch (e) {
      Alert.alert('Could not become a host', e.message);
    } finally {
      setBusy(false);
    }
  }

  function confirmDelete() {
    Alert.alert(
      'Delete account',
      'This permanently anonymises your account and signs you out. This cannot be undone.',
      [
        { text: 'Cancel', style: 'cancel' },
        {
          text: 'Delete',
          style: 'destructive',
          onPress: async () => {
            try {
              await api.deleteAccount();
              await logout();
            } catch (e) {
              Alert.alert('Could not delete account', e.message);
            }
          },
        },
      ],
    );
  }

  if (!authenticated) {
    return (
      <View style={styles.center}>
        <Text style={styles.meta}>Sign in to access your account.</Text>
        <Pressable style={styles.btn} onPress={login}><Text style={styles.btnText}>Sign in</Text></Pressable>
      </View>
    );
  }

  return (
    <View style={styles.container}>
      {LINKS.map((l) => (
        <Pressable key={l.screen} style={styles.row} onPress={() => navigation.navigate(l.screen)}>
          <Text style={styles.icon}>{l.icon}</Text>
          <Text style={styles.label}>{l.label}</Text>
          <Text style={styles.chevron}>›</Text>
        </Pressable>
      ))}
      {isHost ? (
        <>
          <Pressable style={styles.row} onPress={() => navigation.navigate('HostListings')}>
            <Text style={styles.icon}>🏠</Text>
            <Text style={styles.label}>Host dashboard</Text>
            <Text style={styles.chevron}>›</Text>
          </Pressable>
          <Pressable style={styles.row} onPress={() => navigation.navigate('HostEarnings')}>
            <Text style={styles.icon}>💸</Text>
            <Text style={styles.label}>Earnings & payouts</Text>
            <Text style={styles.chevron}>›</Text>
          </Pressable>
        </>
      ) : (
        <Pressable style={styles.row} onPress={becomeHost} disabled={busy}>
          <Text style={styles.icon}>🏠</Text>
          <Text style={styles.label}>Become a host</Text>
          <Text style={styles.chevron}>›</Text>
        </Pressable>
      )}
      <Pressable
        style={styles.row}
        onPress={() => setupTwoFactor()}
        disabled={!canSetupTwoFactor}
      >
        <Text style={styles.icon}>🔐</Text>
        <Text style={styles.label}>Two-factor authentication</Text>
        <Text style={styles.chevron}>›</Text>
      </Pressable>
      <Pressable style={styles.row} onPress={confirmDelete}>
        <Text style={styles.icon}>🗑️</Text>
        <Text style={[styles.label, { color: '#c0392b' }]}>Delete account</Text>
        <Text style={styles.chevron}>›</Text>
      </Pressable>
      <Pressable style={styles.signOut} onPress={logout}>
        <Text style={styles.signOutText}>Sign out</Text>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff', padding: 12 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 12, backgroundColor: '#fff' },
  row: { flexDirection: 'row', alignItems: 'center', paddingVertical: 16, borderBottomWidth: 1, borderColor: '#eee' },
  icon: { fontSize: 18, width: 32 },
  label: { flex: 1, fontSize: 16, fontWeight: '600' },
  chevron: { fontSize: 22, color: '#bbb' },
  meta: { color: '#717171' },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, paddingHorizontal: 20, paddingVertical: 12 },
  btnText: { color: '#fff', fontWeight: '700' },
  signOut: { marginTop: 24, alignItems: 'center', paddingVertical: 12 },
  signOutText: { color: '#c0392b', fontWeight: '700' },
});
