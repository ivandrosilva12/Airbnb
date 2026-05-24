import { useEffect, useState } from 'react';
import { View, Text, TextInput, Pressable, StyleSheet, ActivityIndicator, ScrollView } from 'react-native';
import { useApi } from '../api/useApi';

const DOC_TYPES = [
  { value: 'passport', label: 'Passport' },
  { value: 'id_card', label: 'ID card' },
  { value: 'driver_license', label: 'Driver’s license' },
];

const STATUS_LABEL = {
  pending: 'Under review',
  approved: 'Verified',
  rejected: 'Rejected',
  none: 'Not verified',
};

export default function VerificationScreen() {
  const api = useApi();
  const [verification, setVerification] = useState(null);
  const [loading, setLoading] = useState(true);
  const [docType, setDocType] = useState('passport');
  const [legalName, setLegalName] = useState('');
  const [docRef, setDocRef] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState(null);

  async function load() {
    setLoading(true);
    try {
      const res = await api.getVerification();
      setVerification(res.verification);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, []);

  const status = verification?.status || 'none';
  const canSubmit = status === 'none' || status === 'rejected';

  async function submit() {
    setSubmitting(true);
    setError(null);
    try {
      const v = await api.submitVerification({ documentType: docType, documentRef: docRef, legalName });
      setVerification(v);
      setLegalName('');
      setDocRef('');
    } catch (e) {
      setError(e.message);
    } finally {
      setSubmitting(false);
    }
  }

  if (loading) return <ActivityIndicator style={{ flex: 1 }} color="#ff385c" />;

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ padding: 16 }}>
      <Text style={styles.intro}>
        Verify your identity to earn a verified badge and build trust with hosts and guests.
      </Text>
      {error && <Text style={styles.error}>{error}</Text>}

      <View style={styles.statusRow}>
        <Text style={styles.statusLabel}>Status:</Text>
        <Text style={[styles.badge, styles[`badge_${status}`]]}>{STATUS_LABEL[status]}</Text>
      </View>
      {status === 'pending' && <Text style={styles.meta}>Your documents are under review.</Text>}
      {status === 'approved' && <Text style={styles.meta}>Your identity has been verified. Thank you!</Text>}
      {status === 'rejected' && (
        <Text style={styles.meta}>Rejected: {verification.rejectionReason || '—'}. You can submit again below.</Text>
      )}

      {canSubmit && (
        <View style={styles.form}>
          <Text style={styles.field}>Document type</Text>
          <View style={styles.docRow}>
            {DOC_TYPES.map((d) => (
              <Pressable
                key={d.value}
                style={[styles.docChip, docType === d.value && styles.docChipActive]}
                onPress={() => setDocType(d.value)}
              >
                <Text style={docType === d.value ? styles.docChipTextActive : styles.docChipText}>{d.label}</Text>
              </Pressable>
            ))}
          </View>

          <Text style={styles.field}>Full legal name</Text>
          <TextInput style={styles.input} value={legalName} onChangeText={setLegalName} placeholder="As printed on the document" />

          <Text style={styles.field}>Document number / reference</Text>
          <TextInput style={styles.input} value={docRef} onChangeText={setDocRef} autoCapitalize="characters" />

          <Pressable
            style={[styles.btn, (submitting || !legalName || !docRef) && styles.btnDisabled]}
            disabled={submitting || !legalName || !docRef}
            onPress={submit}
          >
            <Text style={styles.btnText}>{submitting ? 'Submitting…' : 'Submit for verification'}</Text>
          </Pressable>
        </View>
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff' },
  intro: { color: '#717171', marginBottom: 16 },
  statusRow: { flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 6 },
  statusLabel: { fontWeight: '700' },
  badge: { paddingHorizontal: 10, paddingVertical: 2, borderRadius: 999, overflow: 'hidden', fontWeight: '700', fontSize: 13 },
  badge_none: { backgroundColor: '#f0f0f0', color: '#717171' },
  badge_pending: { backgroundColor: '#fff4e5', color: '#b26a00' },
  badge_approved: { backgroundColor: '#e6f7ee', color: '#1a7f47' },
  badge_rejected: { backgroundColor: '#fdecea', color: '#c0392b' },
  meta: { color: '#717171', marginBottom: 8 },
  form: { marginTop: 20, gap: 6 },
  field: { fontWeight: '600', marginTop: 10 },
  input: { borderWidth: 1, borderColor: '#ddd', borderRadius: 8, padding: 10 },
  docRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  docChip: { borderWidth: 1, borderColor: '#ddd', borderRadius: 999, paddingHorizontal: 14, paddingVertical: 8 },
  docChipActive: { backgroundColor: '#ff385c', borderColor: '#ff385c' },
  docChipText: { color: '#222' },
  docChipTextActive: { color: '#fff', fontWeight: '700' },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, paddingVertical: 14, alignItems: 'center', marginTop: 16 },
  btnDisabled: { opacity: 0.5 },
  btnText: { color: '#fff', fontWeight: '700' },
  error: { color: '#c0392b', marginBottom: 8 },
});
