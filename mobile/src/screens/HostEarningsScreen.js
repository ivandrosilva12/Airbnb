import { useEffect, useState, useCallback } from 'react';
import { View, Text, ScrollView, Pressable, StyleSheet, ActivityIndicator, Linking, RefreshControl } from 'react-native';
import { useApi } from '../api/useApi';

// TODO(i18n): translate — wrap hardcoded labels via useT() from ../i18n/I18nContext.
// Key namespace mirrors the web (earnings.*).

// HostEarningsScreen mirrors the web payouts panel: balances, payout-account
// onboarding (opens the hosted link in the browser), requesting a payout, and
// the disbursement history. Pull to refresh re-checks the account state.
export default function HostEarningsScreen() {
  const api = useApi();
  const [balances, setBalances] = useState([]);
  const [available, setAvailable] = useState([]);
  const [account, setAccount] = useState({ hasAccount: false, enabled: false });
  const [disbursements, setDisbursements] = useState([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);

  const load = useCallback(async () => {
    setError(null);
    try {
      const [summary, avail, payouts] = await Promise.all([
        api.hostEarnings(),
        api.payoutAvailable(),
        api.listDisbursements(),
      ]);
      setBalances(summary.balances || []);
      setAvailable(avail.available || []);
      setAccount(avail.account || { hasAccount: false, enabled: false });
      setDisbursements(payouts.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [api]);

  useEffect(() => {
    load();
  }, []);

  async function refreshStatus() {
    setBusy(true);
    try {
      await api.refreshPayoutAccount();
      await load();
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }

  async function startOnboarding() {
    setBusy(true);
    setError(null);
    try {
      const { url } = await api.onboardPayouts({ returnUrl: 'airhost://payouts', refreshUrl: 'airhost://payouts' });
      if (url) await Linking.openURL(url);
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }

  async function requestPayout(currency) {
    setBusy(true);
    setError(null);
    try {
      await api.requestPayout(currency);
      await load();
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }

  if (loading) return <ActivityIndicator style={{ flex: 1 }} color="#ff385c" />;

  return (
    <ScrollView
      style={styles.container}
      refreshControl={<RefreshControl refreshing={busy} onRefresh={refreshStatus} />}
    >
      {error && <Text style={styles.error}>{error}</Text>}

      <Text style={styles.h2}>Balance</Text>
      {balances.length === 0 ? (
        <Text style={styles.meta}>No earnings yet.</Text>
      ) : (
        balances.map((b) => (
          <View key={b.currency} style={styles.card}>
            <Text style={styles.currency}>{b.currency}</Text>
            <Text style={styles.net}>{b.net.display}</Text>
            <Text style={styles.meta}>Earned {b.earned.display} · Refunded {b.refunded.display}</Text>
          </View>
        ))
      )}

      <Text style={styles.h2}>Payouts</Text>
      {!account.enabled ? (
        <View style={styles.card}>
          <Text style={styles.meta}>Set up your payout account to receive your earnings.</Text>
          <Pressable style={[styles.btn, busy && styles.btnDisabled]} onPress={startOnboarding} disabled={busy}>
            <Text style={styles.btnText}>Set up payouts</Text>
          </Pressable>
          {account.hasAccount && (
            <Pressable style={styles.linkBtn} onPress={refreshStatus} disabled={busy}>
              <Text style={styles.link}>I’ve completed onboarding — refresh</Text>
            </Pressable>
          )}
        </View>
      ) : available.length === 0 ? (
        <Text style={styles.meta}>No funds available to pay out yet.</Text>
      ) : (
        available.map((a) => (
          <View key={a.currency} style={styles.card}>
            <Text style={styles.currency}>{a.currency}</Text>
            <Text style={styles.net}>{a.available.display}</Text>
            <Pressable
              style={[styles.btn, (busy || a.available.amountCents <= 0) && styles.btnDisabled]}
              onPress={() => requestPayout(a.currency)}
              disabled={busy || a.available.amountCents <= 0}
            >
              <Text style={styles.btnText}>Request payout</Text>
            </Pressable>
          </View>
        ))
      )}

      {disbursements.length > 0 && (
        <>
          <Text style={styles.h2}>Payout history</Text>
          {disbursements.map((d) => (
            <View key={d.id} style={styles.row}>
              <Text style={styles.meta}>{new Date(d.createdAt).toLocaleDateString()}</Text>
              <Text style={styles.amount}>{d.amount.display}</Text>
              <Text style={[styles.badge, d.status === 'paid' ? styles.paid : d.status === 'failed' ? styles.failed : styles.pending]}>
                {d.status}
              </Text>
            </View>
          ))}
        </>
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff', padding: 16 },
  h2: { fontSize: 18, fontWeight: '800', marginTop: 16, marginBottom: 8 },
  card: { borderWidth: 1, borderColor: '#eee', borderRadius: 12, padding: 16, marginBottom: 12 },
  currency: { color: '#717171', fontSize: 12, textTransform: 'uppercase' },
  net: { fontSize: 24, fontWeight: '800', marginVertical: 4 },
  meta: { color: '#717171' },
  row: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', paddingVertical: 10, borderBottomWidth: 1, borderColor: '#eee' },
  amount: { fontWeight: '700' },
  badge: { fontSize: 12, fontWeight: '700', overflow: 'hidden', borderRadius: 6, paddingHorizontal: 8, paddingVertical: 2 },
  paid: { backgroundColor: '#e6f7ee', color: '#1a7f47' },
  pending: { backgroundColor: '#fff4e5', color: '#b26a00' },
  failed: { backgroundColor: '#fdecea', color: '#c0392b' },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, padding: 12, alignItems: 'center', marginTop: 12 },
  btnDisabled: { opacity: 0.5 },
  btnText: { color: '#fff', fontWeight: '700' },
  linkBtn: { marginTop: 10, alignItems: 'center' },
  link: { color: '#ff385c', fontWeight: '600' },
  error: { color: '#c0392b', marginBottom: 8 },
});
